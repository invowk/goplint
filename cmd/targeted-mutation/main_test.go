// SPDX-License-Identifier: MPL-2.0

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/mutationguard"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func TestMutationGuardEnvironmentIsolatesAggregateOutputs(t *testing.T) {
	t.Setenv("GOWORK", "caller-workspace")
	t.Setenv(soundnessevidence.EnvEvidenceDir, "/tmp/aggregate-observations")
	t.Setenv(soundnessgate.EnvReportPath, "/tmp/aggregate-report.json")
	t.Setenv(soundnessgate.EnvSubgateReportPath, "/tmp/subgate-report.json")

	values := make(map[string][]string)
	for _, entry := range mutationGuardEnvironment() {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = append(values[name], value)
		}
	}
	for _, name := range []string{
		soundnessevidence.EnvEvidenceDir,
		soundnessgate.EnvReportPath,
		soundnessgate.EnvSubgateReportPath,
	} {
		if _, exists := values[name]; exists {
			t.Errorf("mutation guard environment retained %s", name)
		}
	}
	if got := values["GOWORK"]; !slices.Equal(got, []string{"off"}) {
		t.Errorf("mutation guard GOWORK values = %v, want [off]", got)
	}
}

func TestValidateProfileAcceptsV2GuardContract(t *testing.T) {
	t.Parallel()

	mutations := make([]targetedMutation, 0, len(mutationCategories))
	mutationIDs := make([]string, 0, len(mutationCategories))
	for _, category := range mutationCategories {
		mutation := testMutation("mutant/" + category)
		mutation.Categories = []string{category}
		mutation.ChangedStages = slices.Clone(mutationExecutionStages)
		mutations = append(mutations, mutation)
		mutationIDs = append(mutationIDs, mutation.ID)
	}
	profile := mutationProfile{
		FormatVersion: mutationProfileFormatVersion,
		Count:         2,
		MutationIDs:   mutationIDs,
	}
	manifest := mutationManifest{
		FormatVersion: mutationManifestFormatVersion,
		Policy:        mutationPolicy{},
		Mutations:     mutations,
	}
	selected, err := validateProfile(profile, manifest)
	if err != nil {
		t.Fatalf("validateProfile() error = %v", err)
	}
	if !slices.EqualFunc(selected, mutations, func(left, right targetedMutation) bool {
		return left.ID == right.ID
	}) {
		t.Fatalf("validateProfile() selected IDs differ from profile order")
	}
	incomplete := manifest
	incomplete.Mutations = slices.Clone(manifest.Mutations)
	incomplete.Mutations[0].ChangedStages = []soundnessevidence.ExecutionStage{soundnessevidence.StageReporting}
	if _, err := validateProfile(profile, incomplete); err == nil {
		t.Fatal("validateProfile() accepted incomplete category stage coverage")
	}
	uncategorized := manifest
	uncategorized.Mutations = slices.Clone(manifest.Mutations)
	uncategorized.Mutations[0].Categories = nil
	if _, err := validateProfile(profile, uncategorized); err == nil {
		t.Fatal("validateProfile() accepted an uncategorized mutant")
	}
	profile.Count = 1
	if _, err := validateProfile(profile, manifest); err == nil {
		t.Fatal("validateProfile() accepted a non-repeatable profile")
	}
}

func TestRepositoryV2ProfileAndAnchorsValidate(t *testing.T) {
	t.Parallel()

	moduleRoot, err := findModuleRoot()
	if err != nil {
		t.Fatalf("findModuleRoot() error = %v", err)
	}
	profilePath := "testdata/mutation/profiles/blocking-v2.json"
	profile, err := readJSON[mutationProfile](filepath.Join(moduleRoot, profilePath))
	if err != nil {
		t.Fatalf("read profile %s: %v", profilePath, err)
	}
	manifest, err := readJSON[mutationManifest](filepath.Join(moduleRoot, filepath.FromSlash(profile.Manifest)))
	if err != nil {
		t.Fatalf("read manifest for %s: %v", profilePath, err)
	}
	mutations, err := validateProfile(profile, manifest)
	if err != nil {
		t.Fatalf("validate profile %s: %v", profilePath, err)
	}
	for _, mutation := range mutations {
		content, err := os.ReadFile(filepath.Join(moduleRoot, filepath.FromSlash(mutation.File)))
		if err != nil {
			t.Fatalf("read mutation source %s: %v", mutation.File, err)
		}
		if _, err := transformMutationSource(sourceSnapshot{Content: content}, mutation); err != nil {
			t.Fatalf("validate mutation %s source contract: %v", mutation.ID, err)
		}
	}
}

func TestDecodeJSONRejectsLegacyMutationFieldsAndTrailingValues(t *testing.T) {
	t.Parallel()

	for _, encoded := range []string{
		`{"id":"mutant","test_regex":"^TestGuard$","expected_failures":["TestGuard"]}`,
		`{} {}`,
	} {
		if _, err := decodeJSON[targetedMutation](encoded, "test mutation"); err == nil {
			t.Fatalf("decodeJSON(%q) accepted an invalid v2 contract", encoded)
		}
	}
}

func TestValidateMutationGuardRejectsInvalidV2Contracts(t *testing.T) {
	t.Parallel()

	base := testMutation("mutant/base")
	tests := []struct {
		name   string
		mutate func(*targetedMutation)
	}{
		{name: "empty stages", mutate: func(m *targetedMutation) { m.ChangedStages = nil }},
		{name: "duplicate stages", mutate: func(m *targetedMutation) {
			m.ChangedStages = []soundnessevidence.ExecutionStage{
				soundnessevidence.StageReporting,
				soundnessevidence.StageReporting,
			}
		}},
		{name: "unknown stage", mutate: func(m *targetedMutation) {
			m.ChangedStages = []soundnessevidence.ExecutionStage{"unknown"}
		}},
		{name: "missing selected test", mutate: func(m *targetedMutation) { m.Guard.SelectedTests = nil }},
		{name: "subtest selected as root", mutate: func(m *targetedMutation) {
			m.Guard.SelectedTests = []string{"TestGuard/subtest"}
		}},
		{name: "regex misses selected test", mutate: func(m *targetedMutation) { m.Guard.TestRegex = "^TestOther$" }},
		{name: "mismatch outside selected root", mutate: func(m *targetedMutation) {
			m.Guard.ExpectedMismatches[0].Assertion.Test = "TestOther"
		}},
		{name: "duplicate assertion", mutate: func(m *targetedMutation) {
			m.Guard.ExpectedMismatches = append(m.Guard.ExpectedMismatches, m.Guard.ExpectedMismatches[0])
		}},
		{name: "identical observations", mutate: func(m *targetedMutation) {
			m.Guard.ExpectedMismatches[0].Actual = m.Guard.ExpectedMismatches[0].Expected
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mutation := base
			mutation.ChangedStages = slices.Clone(base.ChangedStages)
			mutation.Guard.SelectedTests = slices.Clone(base.Guard.SelectedTests)
			mutation.Guard.ExpectedMismatches = slices.Clone(base.Guard.ExpectedMismatches)
			tt.mutate(&mutation)
			if err := validateChangedStages(mutation.ID, mutation.ChangedStages); err == nil {
				if err := validateMutationGuard(mutation.ID, mutation.Guard); err == nil {
					t.Fatal("invalid v2 mutation contract was accepted")
				}
			}
		})
	}
}

func TestValidateCleanGuardRunRequiresExactSelectedTestCensus(t *testing.T) {
	t.Parallel()

	mutation := testMutation("mutant/control")
	clean := mustTrace(t, repeatedCleanOutput(mutation.Guard, 2))
	if err := validateCleanGuardRun(clean, commandResult{}, mutation.Guard, 2); err != nil {
		t.Fatalf("validateCleanGuardRun(clean) error = %v", err)
	}

	tests := []struct {
		name   string
		result commandResult
		trace  goTestTrace
	}{
		{name: "missing", trace: mustTrace(t, "")},
		{name: "skipped", trace: mustTrace(t, testEvent("run", "TestGuard", "")+testEvent("skip", "TestGuard", ""))},
		{name: "extra root", trace: mustTrace(t, repeatedCleanOutput(mutation.Guard, 2)+testEvent("run", "TestOther", "")+testEvent("pass", "TestOther", ""))},
		{name: "failed", result: commandResult{Err: errors.New("exit status 1")}, trace: mustTrace(t, causalFailureOutput(t, mutation, mutation.Guard.ExpectedMismatches[0]))},
		{name: "timeout", result: commandResult{TimedOut: true}, trace: clean},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := validateCleanGuardRun(tt.trace, tt.result, mutation.Guard, 2); err == nil {
				t.Fatal("validateCleanGuardRun() accepted invalid clean control")
			}
		})
	}
}

func TestAttributedGoTestFailuresRequiresExactStructuredMismatch(t *testing.T) {
	t.Parallel()

	mutation := testMutation("mutant/causal")
	expected := mutation.Guard.ExpectedMismatches[0]
	trace := mustTrace(t, causalFailureOutput(t, mutation, expected))
	classification := attributedGoTestFailures(
		mutation,
		trace,
		commandResult{Err: errors.New("exit status 1")},
	)
	if classification.Status != mutationStatusKilled || len(classification.Records) != 1 {
		t.Fatalf("attributedGoTestFailures() = %+v, want one causal kill", classification)
	}
	record := classification.Records[0]
	if record.MutantID != mutation.ID || record.DeclaredConcern != mutation.Concern ||
		record.FailingAssertion != expected.Assertion ||
		record.ExpectedSemanticObservation != expected.Expected ||
		record.ActualSemanticObservation != expected.Actual {
		t.Fatalf("guard mismatch record = %+v, want manifest-bound attribution", record)
	}
}

func TestAttributedGoTestFailuresRejectsNonCausalOutcomes(t *testing.T) {
	t.Parallel()

	mutation := testMutation("mutant/noncausal")
	expected := mutation.Guard.ExpectedMismatches[0]
	wrongAssertion := expected
	wrongAssertion.Assertion.ID = "unrelated-assertion"
	wrongExpected := expected
	wrongExpected.Expected.State = "different-clean-state"
	wrongActual := expected
	wrongActual.Actual.State = "different-mutant-state"
	tests := []struct {
		name       string
		output     func(*testing.T) string
		result     commandResult
		wantReason mutationReasonCode
	}{
		{
			name: "survived",
			output: func(*testing.T) string {
				return repeatedCleanOutput(mutation.Guard, 1)
			},
			wantReason: mutationReasonNone,
		},
		{
			name: "setup or unrelated assertion",
			output: func(*testing.T) string {
				return testEvent("run", "TestGuard", "") + testEvent("fail", "TestGuard", "") + testEvent("fail", "", "FAIL\n")
			},
			result:     commandResult{Err: errors.New("exit status 1")},
			wantReason: mutationReasonUnattributedGuardFailure,
		},
		{
			name:       "wrong assertion",
			output:     func(t *testing.T) string { return causalFailureOutput(t, mutation, wrongAssertion) },
			result:     commandResult{Err: errors.New("exit status 1")},
			wantReason: mutationReasonWrongAssertion,
		},
		{
			name:       "wrong expected observation",
			output:     func(t *testing.T) string { return causalFailureOutput(t, mutation, wrongExpected) },
			result:     commandResult{Err: errors.New("exit status 1")},
			wantReason: mutationReasonWrongExpectedObservation,
		},
		{
			name:       "wrong actual observation",
			output:     func(t *testing.T) string { return causalFailureOutput(t, mutation, wrongActual) },
			result:     commandResult{Err: errors.New("exit status 1")},
			wantReason: mutationReasonWrongActualObservation,
		},
		{
			name: "panic",
			output: func(t *testing.T) string {
				return causalFailureOutput(t, mutation, expected) + testEvent("output", "TestGuard", "panic: boom\n")
			},
			result:     commandResult{Err: errors.New("exit status 1")},
			wantReason: mutationReasonPanic,
		},
		{
			name:       "timeout",
			output:     func(t *testing.T) string { return causalFailureOutput(t, mutation, expected) },
			result:     commandResult{Err: errors.New("deadline"), TimedOut: true},
			wantReason: mutationReasonTimeout,
		},
		{
			name: "missing selected test",
			output: func(*testing.T) string {
				return testEvent("fail", "", "FAIL\n")
			},
			result:     commandResult{Err: errors.New("exit status 1")},
			wantReason: mutationReasonMissingOrSkippedGuard,
		},
		{
			name: "unexpected failing test",
			output: func(t *testing.T) string {
				return causalFailureOutput(t, mutation, expected) +
					testEvent("run", "TestOther", "") + testEvent("fail", "TestOther", "")
			},
			result:     commandResult{Err: errors.New("exit status 1")},
			wantReason: mutationReasonUnexpectedTestFailure,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			trace := mustTrace(t, tt.output(t))
			classification := attributedGoTestFailures(mutation, trace, tt.result)
			if tt.name == "survived" {
				if classification.Status != mutationStatusSurvived {
					t.Fatalf("classification = %+v, want survived", classification)
				}
				return
			}
			if classification.Status != mutationStatusInvalid || classification.ReasonCode != tt.wantReason {
				t.Fatalf("classification = %+v, want invalid/%s", classification, tt.wantReason)
			}
		})
	}
}

func TestParseGoTestTraceRejectsMalformedOrDuplicateMismatchEvidence(t *testing.T) {
	t.Parallel()

	malformed := testEvent("output", "TestGuard", mutationguard.EventPrefix+"{not-json}\n")
	if _, err := parseGoTestTrace(malformed); err == nil {
		t.Fatal("parseGoTestTrace() accepted malformed mismatch JSON")
	}

	mutation := testMutation("mutant/duplicate")
	expected := mutation.Guard.ExpectedMismatches[0]
	output := causalFailureOutput(t, mutation, expected)
	marker := mismatchOutput(t, expected)
	trace := mustTrace(t, output+testEvent("output", expected.Assertion.Test, marker+"\n"))
	classification := attributedGoTestFailures(mutation, trace, commandResult{Err: errors.New("exit status 1")})
	if classification.ReasonCode != mutationReasonMalformedAttribution {
		t.Fatalf("duplicate mismatch classification = %+v, want malformed attribution", classification)
	}
}

func TestExecuteMutationInWorkspaceUsesCausalLifecycleAndRestoresSource(t *testing.T) {
	t.Parallel()

	mutation := testMutation("mutant/lifecycle")
	mutation.Guard.ExpectedMismatches[0].Assertion.Test = "TestGuard/subtest"
	count := 2
	current := sourceSnapshot{Content: []byte("prefix clean suffix"), Mode: 0o640}
	mutation.Before = "clean"
	mutation.After = "mutated"
	mutation.BeforeSHA256 = digestText(mutation.Before)
	mutation.AfterSHA256 = digestText(mutation.After)
	var phases []string
	guardCalls := 0
	runtime := mutationRuntime{
		ReadSource: func(string) (sourceSnapshot, error) {
			phases = append(phases, "read:"+string(current.Content))
			return copySnapshot(current), nil
		},
		WriteSource: func(_ string, snapshot sourceSnapshot) error {
			current = copySnapshot(snapshot)
			phases = append(phases, "write:"+string(snapshot.Content))
			return nil
		},
		Compile: func(string) commandResult {
			phases = append(phases, "compile")
			return commandResult{}
		},
		RunGuard: func(_ string, _ string, gotCount int) commandResult {
			guardCalls++
			phases = append(phases, fmt.Sprintf("guard:%d:%d", guardCalls, gotCount))
			if guardCalls == 1 || guardCalls == count+2 {
				return commandResult{Output: repeatedCleanOutput(mutation.Guard, count)}
			}
			return commandResult{
				Output: causalFailureOutput(t, mutation, mutation.Guard.ExpectedMismatches[0]),
				Err:    errors.New("exit status 1"),
			}
		},
	}
	result := executeMutationInWorkspace("/memory", mutation, count, runtime)
	if result.Status != mutationStatusKilled || len(result.GuardMismatches) != 1 {
		t.Fatalf("executeMutationInWorkspace() = %+v, want causal kill", result)
	}
	if string(current.Content) != "prefix clean suffix" || current.Mode.Perm() != fs.FileMode(0o640) {
		t.Fatalf("source was not restored: content=%q mode=%o", current.Content, current.Mode.Perm())
	}
	mutatedWrite := slices.Index(phases, "write:prefix mutated suffix")
	compile := slices.Index(phases, "compile")
	restoredWrite := slices.Index(phases, "write:prefix clean suffix")
	postControl := slices.Index(phases, fmt.Sprintf("guard:%d:%d", count+2, count))
	if mutatedWrite < 0 || compile <= mutatedWrite || restoredWrite <= compile || postControl <= restoredWrite {
		t.Fatalf("lifecycle phases = %v, want mutate -> compile/guards -> restore -> post-control", phases)
	}
}

func TestExecuteMutationInWorkspaceRejectsCompileAndRestorationFailures(t *testing.T) {
	t.Parallel()

	mutation := testMutation("mutant/lifecycle-failures")
	mutation.Before = "clean"
	mutation.After = "mutated"
	mutation.BeforeSHA256 = digestText(mutation.Before)
	mutation.AfterSHA256 = digestText(mutation.After)
	tests := []struct {
		name            string
		compile         commandResult
		failMutantWrite bool
		failRestore     bool
		wantReason      mutationReasonCode
	}{
		{name: "compile", compile: commandResult{Err: errors.New("compile failed")}, wantReason: mutationReasonCompile},
		{name: "compile timeout", compile: commandResult{Err: errors.New("deadline"), TimedOut: true}, wantReason: mutationReasonTimeout},
		{name: "partial mutant write", failMutantWrite: true, wantReason: mutationReasonTransformation},
		{name: "restoration", failRestore: true, wantReason: mutationReasonRestoration},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			current := sourceSnapshot{Content: []byte("clean"), Mode: 0o600}
			writes := 0
			runtime := mutationRuntime{
				ReadSource: func(string) (sourceSnapshot, error) { return copySnapshot(current), nil },
				WriteSource: func(_ string, snapshot sourceSnapshot) error {
					writes++
					if tt.failRestore && writes == 2 {
						return errors.New("restore failed")
					}
					current = copySnapshot(snapshot)
					if tt.failMutantWrite && writes == 1 {
						return errors.New("partial mutant write")
					}
					return nil
				},
				Compile: func(string) commandResult { return tt.compile },
				RunGuard: func(string, string, int) commandResult {
					if string(current.Content) == "clean" {
						return commandResult{Output: repeatedCleanOutput(mutation.Guard, 2)}
					}
					return commandResult{
						Output: causalFailureOutput(t, mutation, mutation.Guard.ExpectedMismatches[0]),
						Err:    errors.New("exit status 1"),
					}
				},
			}
			result := executeMutationInWorkspace("/memory", mutation, 2, runtime)
			if result.Status != mutationStatusInvalid || result.ReasonCode != tt.wantReason {
				t.Fatalf("executeMutationInWorkspace() = %+v, want invalid/%s", result, tt.wantReason)
			}
			if !tt.failRestore && string(current.Content) != "clean" {
				t.Fatalf("source content = %q, want restored clean content", current.Content)
			}
		})
	}
}

func TestValidateRepeatedAttributionRejectsDrift(t *testing.T) {
	t.Parallel()

	mutation := testMutation("mutant/repeat")
	record := guardMismatchRecord{
		MutantID:                    mutation.ID,
		DeclaredConcern:             mutation.Concern,
		FailingAssertion:            mutation.Guard.ExpectedMismatches[0].Assertion,
		ExpectedSemanticObservation: mutation.Guard.ExpectedMismatches[0].Expected,
		ActualSemanticObservation:   mutation.Guard.ExpectedMismatches[0].Actual,
	}
	if err := validateRepeatedAttribution([]guardMismatchRecord{record}, []guardMismatchRecord{record}); err != nil {
		t.Fatalf("validateRepeatedAttribution(equal) error = %v", err)
	}
	drifted := record
	drifted.FailingAssertion.ID = "other"
	if err := validateRepeatedAttribution([]guardMismatchRecord{record}, []guardMismatchRecord{drifted}); err == nil {
		t.Fatal("validateRepeatedAttribution() accepted attribution drift")
	}
}

func TestMutationEvidenceCasesUseOnlyDeclaredChangedStages(t *testing.T) {
	t.Parallel()

	first := testMutation("mutant/propagation")
	first.Categories = []string{"unvalidated-cast"}
	first.ChangedStages = []soundnessevidence.ExecutionStage{soundnessevidence.StagePropagation}
	second := testMutation("mutant/reporting")
	second.Categories = []string{"unvalidated-cast"}
	second.ChangedStages = []soundnessevidence.ExecutionStage{soundnessevidence.StageReporting}
	results := []mutationResult{causalResult(first), causalResult(second)}
	cases, err := mutationEvidenceCases([]targetedMutation{first, second}, results)
	if err != nil {
		t.Fatalf("mutationEvidenceCases() error = %v", err)
	}
	observation, err := soundnessevidence.ObservationFromCases(
		"unvalidated-cast.mutation",
		"targeted-mutation",
		"cmd/targeted-mutation/unvalidated-cast",
		cases["unvalidated-cast"],
	)
	if err != nil {
		t.Fatalf("ObservationFromCases() error = %v", err)
	}
	want := []soundnessevidence.ExecutionStage{
		soundnessevidence.StagePropagation,
		soundnessevidence.StageReporting,
	}
	if !slices.Equal(observation.Stages, want) {
		t.Fatalf("mutation evidence stages = %v, want %v", observation.Stages, want)
	}
}

func testMutation(id string) targetedMutation {
	expected := mutationguard.Observation{Subject: "semantic-state", State: "clean"}
	actual := mutationguard.Observation{Subject: "semantic-state", State: "mutated"}
	return targetedMutation{
		ID:            id,
		Concern:       "the declared semantic concern",
		ChangedStages: []soundnessevidence.ExecutionStage{soundnessevidence.StageReporting},
		File:          "goplint/source.go",
		Before:        "before",
		BeforeSHA256:  digestText("before"),
		After:         "after",
		AfterSHA256:   digestText("after"),
		Guard: mutationGuard{
			TestRegex:     "^TestGuard$",
			SelectedTests: []string{"TestGuard"},
			ExpectedMismatches: []expectedGuardMismatch{
				{
					Assertion: guardAssertion{Test: "TestGuard", ID: "semantic-assertion"},
					Expected:  expected,
					Actual:    actual,
				},
			},
		},
	}
}

func causalResult(mutation targetedMutation) mutationResult {
	expected := mutation.Guard.ExpectedMismatches[0]
	return mutationResult{
		ID:     mutation.ID,
		Status: mutationStatusKilled,
		GuardMismatches: []guardMismatchRecord{
			{
				MutantID:                    mutation.ID,
				DeclaredConcern:             mutation.Concern,
				FailingAssertion:            expected.Assertion,
				ExpectedSemanticObservation: expected.Expected,
				ActualSemanticObservation:   expected.Actual,
			},
		},
	}
}

func repeatedCleanOutput(guard mutationGuard, count int) string {
	var output strings.Builder
	for range count {
		for _, root := range guard.SelectedTests {
			output.WriteString(testEvent("run", root, ""))
		}
		for _, mismatch := range guard.ExpectedMismatches {
			if mismatch.Assertion.Test != rootTest(mismatch.Assertion.Test) {
				output.WriteString(testEvent("run", mismatch.Assertion.Test, ""))
				output.WriteString(testEvent("pass", mismatch.Assertion.Test, ""))
			}
		}
		for _, root := range guard.SelectedTests {
			output.WriteString(testEvent("pass", root, ""))
		}
	}
	return output.String()
}

func causalFailureOutput(t *testing.T, mutation targetedMutation, mismatch expectedGuardMismatch) string {
	t.Helper()

	root := rootTest(mismatch.Assertion.Test)
	var output strings.Builder
	for _, selected := range mutation.Guard.SelectedTests {
		output.WriteString(testEvent("run", selected, ""))
	}
	if mismatch.Assertion.Test != root {
		output.WriteString(testEvent("run", mismatch.Assertion.Test, ""))
	}
	output.WriteString(testEvent("output", mismatch.Assertion.Test, mismatchOutput(t, mismatch)+"\n"))
	output.WriteString(testEvent("fail", mismatch.Assertion.Test, ""))
	if mismatch.Assertion.Test != root {
		output.WriteString(testEvent("fail", root, ""))
	}
	for _, selected := range mutation.Guard.SelectedTests {
		if selected != root {
			output.WriteString(testEvent("pass", selected, ""))
		}
	}
	output.WriteString(testEvent("fail", "", "FAIL\n"))
	return output.String()
}

func mismatchOutput(t *testing.T, mismatch expectedGuardMismatch) string {
	t.Helper()

	encoded, err := mutationguard.EncodeEvent(mutationguard.AssertionEvent{
		FormatVersion: mutationguard.EventFormatVersion,
		AssertionID:   mismatch.Assertion.ID,
		Expected:      mismatch.Expected,
		Actual:        mismatch.Actual,
	})
	if err != nil {
		t.Fatalf("EncodeEvent() error = %v", err)
	}
	return encoded
}

func mustTrace(t *testing.T, output string) goTestTrace {
	t.Helper()

	trace, err := parseGoTestTrace(output)
	if err != nil {
		t.Fatalf("parseGoTestTrace() error = %v", err)
	}
	return trace
}

func testEvent(action, testName, output string) string {
	return fmt.Sprintf("{\"Action\":%q,\"Test\":%q,\"Output\":%q}\n", action, testName, output)
}

func rootTest(testName string) string {
	root, _, _ := strings.Cut(testName, "/")
	return root
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
