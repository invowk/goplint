// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/mutationguard"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

var errUnexpectedRootGuard = errors.New("unexpected root guard execution")

func executeMutation(moduleRoot string, mutation targetedMutation, count int) mutationResult {
	tempRoot, err := os.MkdirTemp("", "goplint-targeted-mutation-*")
	if err != nil {
		return invalidMutationResult(mutation.ID, mutationReasonWorkspace, err.Error())
	}
	defer func() {
		if cleanupErr := os.RemoveAll(tempRoot); cleanupErr != nil {
			fmt.Fprintln(os.Stderr, "targeted mutation cleanup:", cleanupErr)
		}
	}()
	if err := copyModule(moduleRoot, tempRoot); err != nil {
		return invalidMutationResult(mutation.ID, mutationReasonWorkspace, err.Error())
	}
	runtime := mutationRuntime{
		ReadSource:  readSourceSnapshot,
		WriteSource: writeSourceSnapshot,
		Compile:     runGoTestCompile,
		RunGuard:    runGoTest,
	}
	return executeMutationInWorkspace(tempRoot, mutation, count, runtime)
}

func executeMutationInWorkspace(
	workdir string,
	mutation targetedMutation,
	count int,
	runtime mutationRuntime,
) (result mutationResult) {
	result = invalidMutationResult(mutation.ID, mutationReasonWorkspace, "mutation lifecycle did not complete")
	if runtime.ReadSource == nil || runtime.WriteSource == nil || runtime.Compile == nil || runtime.RunGuard == nil {
		result.Reason = "mutation runtime is incomplete"
		return result
	}
	if count < 2 {
		result.Reason = "mutation repeat count must be at least two"
		return result
	}
	path := filepath.Join(workdir, filepath.FromSlash(mutation.File))
	original, err := runtime.ReadSource(path)
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	if err := runControlGuards(workdir, []targetedMutation{mutation}, count, runtime.RunGuard, "preflight"); err != nil {
		return invalidMutationResult(mutation.ID, mutationReasonControl, err.Error())
	}
	mutated, err := transformMutationSource(original, mutation)
	if err != nil {
		return invalidMutationResult(mutation.ID, mutationReasonAnchor, err.Error())
	}
	defer func() {
		if restoreErr := runtime.WriteSource(path, original); restoreErr != nil {
			result = invalidMutationResult(mutation.ID, mutationReasonRestoration, restoreErr.Error())
			return
		}
		restored, restoreErr := runtime.ReadSource(path)
		if restoreErr != nil {
			result = invalidMutationResult(mutation.ID, mutationReasonRestoration, restoreErr.Error())
			return
		}
		if !sourceSnapshotsEqual(restored, original) {
			result = invalidMutationResult(mutation.ID, mutationReasonRestoration, "restored source digest or mode mismatch")
			return
		}
		if controlErr := runControlGuards(
			workdir,
			[]targetedMutation{mutation},
			count,
			runtime.RunGuard,
			"post-mutation control",
		); controlErr != nil {
			if result.Status == mutationStatusKilled || result.Status == mutationStatusSurvived {
				result = invalidMutationResult(mutation.ID, mutationReasonControl, controlErr.Error())
			} else {
				result.Reason = strings.TrimSpace(result.Reason + "; post-control: " + controlErr.Error())
			}
		}
	}()
	if err := runtime.WriteSource(path, mutated); err != nil {
		return invalidMutationResult(mutation.ID, mutationReasonTransformation, err.Error())
	}

	compileResult := runtime.Compile(workdir)
	if compileResult.TimedOut {
		return invalidMutationResult(mutation.ID, mutationReasonTimeout, "mutant compilation timed out")
	}
	if compileResult.Err != nil {
		return invalidMutationResult(
			mutation.ID,
			mutationReasonCompile,
			"mutant did not compile: "+guardDiagnostics(compileResult),
		)
	}

	var attribution []guardMismatchRecord
	for repeat := range count {
		guardResult := runtime.RunGuard(workdir, mutation.Guard.TestRegex, 1)
		trace, traceErr := parseGoTestTrace(guardResult.Output)
		if traceErr != nil {
			return invalidMutationResult(
				mutation.ID,
				mutationReasonMalformedAttribution,
				traceErr.Error()+": "+guardDiagnostics(guardResult),
			)
		}
		classification := attributedGoTestFailures(mutation, trace, guardResult)
		if classification.Status != mutationStatusKilled {
			return mutationResult{
				ID:         mutation.ID,
				Status:     classification.Status,
				ReasonCode: classification.ReasonCode,
				Reason:     strings.TrimSpace(classification.Reason + ": " + guardDiagnostics(guardResult)),
			}
		}
		if repeat == 0 {
			attribution = slices.Clone(classification.Records)
			continue
		}
		if repeatErr := validateRepeatedAttribution(attribution, classification.Records); repeatErr != nil {
			return invalidMutationResult(
				mutation.ID,
				mutationReasonNonRepeatableAttribution,
				repeatErr.Error(),
			)
		}
	}
	return mutationResult{
		ID:              mutation.ID,
		Status:          mutationStatusKilled,
		GuardMismatches: attribution,
	}
}

func validateRepeatedAttribution(previous, current []guardMismatchRecord) error {
	if !slices.Equal(previous, current) {
		return errors.New("guard mismatch attribution changed across repeats")
	}
	return nil
}

func transformMutationSource(original sourceSnapshot, mutation targetedMutation) (sourceSnapshot, error) {
	content := string(original.Content)
	if strings.Count(content, mutation.Before) != 1 {
		return sourceSnapshot{}, errors.New("source anchor does not occur exactly once")
	}
	digest := sha256.Sum256([]byte(mutation.Before))
	if got := hex.EncodeToString(digest[:]); got != mutation.BeforeSHA256 {
		return sourceSnapshot{}, fmt.Errorf("source anchor hash mismatch: got %s", got)
	}
	mutated := strings.Replace(content, mutation.Before, mutation.After, 1)
	afterDigest := sha256.Sum256([]byte(mutation.After))
	if got := hex.EncodeToString(afterDigest[:]); got != mutation.AfterSHA256 {
		return sourceSnapshot{}, fmt.Errorf("mutant transformation hash mismatch: got %s", got)
	}
	if strings.Count(mutated, mutation.After) != 1 {
		return sourceSnapshot{}, errors.New("mutant transformation cardinality mismatch")
	}
	return sourceSnapshot{Content: []byte(mutated), Mode: original.Mode}, nil
}

func parseGoTestTrace(output string) (goTestTrace, error) {
	trace := goTestTrace{
		RunCounts:  make(map[string]int),
		PassCounts: make(map[string]int),
		FailCounts: make(map[string]int),
		SkipCounts: make(map[string]int),
	}
	decoder := json.NewDecoder(strings.NewReader(output))
	for {
		var event goTestEvent
		if err := decoder.Decode(&event); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return goTestTrace{}, fmt.Errorf("decode structured guard output: %w", err)
		}
		switch event.Action {
		case "run":
			if event.Test != "" {
				trace.RunCounts[event.Test]++
			}
		case "pass":
			if event.Test != "" {
				trace.PassCounts[event.Test]++
			}
		case "fail":
			if event.Test == "" {
				trace.PackageFailure = true
			} else {
				trace.FailCounts[event.Test]++
			}
		case "skip":
			if event.Test != "" {
				trace.SkipCounts[event.Test]++
			}
		}
		lowerOutput := strings.ToLower(event.Output)
		if strings.Contains(lowerOutput, "panic: test timed out") {
			trace.TimedOut = true
		} else if strings.Contains(lowerOutput, "panic:") {
			trace.Panicked = true
		}
		mismatch, found, mismatchErr := mutationguard.DecodeOutputLine(event.Output)
		if mismatchErr != nil {
			return goTestTrace{}, fmt.Errorf("decode mutation guard output: %w", mismatchErr)
		}
		if found {
			trace.Mismatches = append(trace.Mismatches, observedGuardMismatch{Test: event.Test, Event: mismatch})
		}
	}
	return trace, nil
}

func validateCleanGuardRun(trace goTestTrace, result commandResult, guard mutationGuard, count int) error {
	if result.TimedOut || trace.TimedOut {
		return errors.New("clean guard timed out")
	}
	if trace.Panicked {
		return errors.New("clean guard panicked")
	}
	if result.Err != nil || trace.PackageFailure || len(trace.FailCounts) != 0 {
		return errors.New("clean guard failed")
	}
	if len(trace.Mismatches) != 0 {
		return errors.New("clean guard emitted a mismatch record")
	}
	if len(trace.SkipCounts) != 0 {
		return errors.New("clean guard skipped a selected test")
	}
	if err := validateSelectedRootCensus(trace, guard.SelectedTests, count); err != nil {
		return err
	}
	for _, testName := range guard.SelectedTests {
		if trace.PassCounts[testName] != count {
			return fmt.Errorf("clean selected test %q passed %d times, want %d", testName, trace.PassCounts[testName], count)
		}
	}
	for _, mismatch := range guard.ExpectedMismatches {
		if trace.PassCounts[mismatch.Assertion.Test] != count {
			return fmt.Errorf(
				"clean mismatch test %q passed %d times, want %d",
				mismatch.Assertion.Test,
				trace.PassCounts[mismatch.Assertion.Test],
				count,
			)
		}
	}
	return nil
}

func attributedGoTestFailures(
	mutation targetedMutation,
	trace goTestTrace,
	result commandResult,
) guardClassification {
	invalid := func(code mutationReasonCode, reason string) guardClassification {
		return guardClassification{Status: mutationStatusInvalid, ReasonCode: code, Reason: reason}
	}
	if result.TimedOut || trace.TimedOut {
		return invalid(mutationReasonTimeout, "guard timed out")
	}
	if trace.Panicked {
		return invalid(mutationReasonPanic, "guard panicked")
	}
	if err := validateSelectedRootCensus(trace, mutation.Guard.SelectedTests, 1); err != nil {
		reasonCode := mutationReasonMissingOrSkippedGuard
		if errors.Is(err, errUnexpectedRootGuard) {
			reasonCode = mutationReasonUnexpectedTestFailure
		}
		return invalid(reasonCode, err.Error())
	}
	if len(trace.SkipCounts) != 0 {
		return invalid(mutationReasonMissingOrSkippedGuard, "guard skipped a selected test")
	}
	if result.Err == nil {
		if len(trace.FailCounts) != 0 || trace.PackageFailure || len(trace.Mismatches) != 0 {
			return invalid(mutationReasonMalformedAttribution, "successful guard run contains failure evidence")
		}
		for _, testName := range mutation.Guard.SelectedTests {
			if trace.PassCounts[testName] != 1 {
				return invalid(mutationReasonMissingOrSkippedGuard, "selected guard did not pass exactly once")
			}
		}
		for _, expected := range mutation.Guard.ExpectedMismatches {
			if trace.PassCounts[expected.Assertion.Test] != 1 {
				return invalid(mutationReasonMissingOrSkippedGuard, "expected mismatch test did not pass exactly once")
			}
		}
		return guardClassification{Status: mutationStatusSurvived}
	}

	leaves := leafFailedTests(trace.FailCounts)
	if len(leaves) == 0 || len(trace.Mismatches) == 0 {
		return invalid(mutationReasonUnattributedGuardFailure, "guard failed without a structured semantic mismatch")
	}
	expectedByKey := make(map[string]expectedGuardMismatch, len(mutation.Guard.ExpectedMismatches))
	expectedTests := make(map[string]bool, len(mutation.Guard.ExpectedMismatches))
	for _, expected := range mutation.Guard.ExpectedMismatches {
		expectedByKey[assertionKey(expected.Assertion)] = expected
		expectedTests[expected.Assertion.Test] = true
	}
	if !slices.Equal(leaves, sortedMapKeys(expectedTests)) {
		return invalid(
			mutationReasonUnexpectedTestFailure,
			fmt.Sprintf("leaf guard failures = %v, want %v", leaves, sortedMapKeys(expectedTests)),
		)
	}
	seen := make(map[string]bool, len(trace.Mismatches))
	records := make([]guardMismatchRecord, 0, len(trace.Mismatches))
	for _, observed := range trace.Mismatches {
		assertion := guardAssertion{Test: observed.Test, ID: observed.Event.AssertionID}
		key := assertionKey(assertion)
		if seen[key] {
			return invalid(mutationReasonMalformedAttribution, "duplicate structured guard mismatch "+key)
		}
		seen[key] = true
		expected, exists := expectedByKey[key]
		if !exists {
			if expectedTests[observed.Test] {
				return invalid(mutationReasonWrongAssertion, "guard failed at undeclared assertion "+key)
			}
			return invalid(mutationReasonUnexpectedTestFailure, "guard mismatch came from undeclared test "+observed.Test)
		}
		if observed.Event.Expected != expected.Expected {
			return invalid(mutationReasonWrongExpectedObservation, "guard assertion reported the wrong expected observation")
		}
		if observed.Event.Actual != expected.Actual {
			return invalid(mutationReasonWrongActualObservation, "guard assertion reported the wrong actual observation")
		}
		if trace.FailCounts[observed.Test] == 0 {
			return invalid(mutationReasonMalformedAttribution, "guard mismatch was emitted by a non-failing test")
		}
		records = append(records, guardMismatchRecord{
			MutantID:                    mutation.ID,
			DeclaredConcern:             mutation.Concern,
			FailingAssertion:            assertion,
			ExpectedSemanticObservation: observed.Event.Expected,
			ActualSemanticObservation:   observed.Event.Actual,
		})
	}
	if len(seen) != len(expectedByKey) {
		return invalid(mutationReasonMalformedAttribution, "one or more declared guard mismatches were not observed")
	}
	slices.SortFunc(records, func(left, right guardMismatchRecord) int {
		return strings.Compare(assertionKey(left.FailingAssertion), assertionKey(right.FailingAssertion))
	})
	return guardClassification{Status: mutationStatusKilled, Records: records}
}

func validateSelectedRootCensus(trace goTestTrace, selectedTests []string, count int) error {
	selected := make(map[string]bool, len(selectedTests))
	for _, testName := range selectedTests {
		selected[testName] = true
		if trace.RunCounts[testName] != count {
			return fmt.Errorf("selected guard %q ran %d times, want %d", testName, trace.RunCounts[testName], count)
		}
	}
	for testName, runCount := range trace.RunCounts {
		root, _, _ := strings.Cut(testName, "/")
		if root != testName {
			continue
		}
		if !selected[testName] || runCount != count {
			return fmt.Errorf("%w %q count %d", errUnexpectedRootGuard, testName, runCount)
		}
	}
	return nil
}

func leafFailedTests(failures map[string]int) []string {
	actual := make([]string, 0, len(failures))
	for testName := range failures {
		isParent := false
		for candidate := range failures {
			if candidate != testName && strings.HasPrefix(candidate, testName+"/") {
				isParent = true
				break
			}
		}
		if !isParent {
			actual = append(actual, testName)
		}
	}
	sort.Strings(actual)
	return actual
}

func sortedMapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertionKey(assertion guardAssertion) string {
	return assertion.Test + "\x00" + assertion.ID
}

func invalidMutationResult(id string, code mutationReasonCode, reason string) mutationResult {
	return mutationResult{ID: id, Status: mutationStatusInvalid, ReasonCode: code, Reason: reason}
}

func sourceSnapshotsEqual(left, right sourceSnapshot) bool {
	return left.Mode.Perm() == right.Mode.Perm() && sha256.Sum256(left.Content) == sha256.Sum256(right.Content)
}

func readSourceSnapshot(path string) (sourceSnapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return sourceSnapshot{}, fmt.Errorf("stat mutation source %s: %w", path, err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return sourceSnapshot{}, fmt.Errorf("read mutation source %s: %w", path, err)
	}
	return sourceSnapshot{Content: content, Mode: info.Mode()}, nil
}

func writeSourceSnapshot(path string, snapshot sourceSnapshot) error {
	writeErr := os.WriteFile(path, snapshot.Content, snapshot.Mode.Perm())
	chmodErr := os.Chmod(path, snapshot.Mode.Perm())
	if writeErr != nil {
		writeErr = fmt.Errorf("write mutation source %s: %w", path, writeErr)
	}
	if chmodErr != nil {
		chmodErr = fmt.Errorf("restore mutation source mode %s: %w", path, chmodErr)
	}
	return errors.Join(writeErr, chmodErr)
}

func runGoTest(workdir, testRegex string, count int) commandResult {
	ctx, cancel := context.WithTimeout(context.Background(), mutationGuardTimeout)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		"go",
		"test",
		"-json",
		"./goplint",
		"-run",
		testRegex,
		"-count",
		strconv.Itoa(count),
	)
	command.Dir = workdir
	command.Env = mutationGuardEnvironment()
	command.WaitDelay = mutationProcessWaitDelay
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return commandResult{
		Output:      stdout.String(),
		ErrorOutput: stderr.String(),
		Err:         err,
		TimedOut:    ctx.Err() != nil,
	}
}

// guardDiagnostics joins both guard output streams for failure reasons.
func guardDiagnostics(result commandResult) string {
	return compactOutput(strings.TrimSpace(result.Output + "\n" + result.ErrorOutput))
}

func mutationGuardEnvironment() []string {
	removed := map[string]bool{
		"GOWORK":                           true,
		soundnessevidence.EnvEvidenceDir:   true,
		soundnessgate.EnvReportPath:        true,
		soundnessgate.EnvSubgateReportPath: true,
	}
	environment := slices.DeleteFunc(slices.Clone(os.Environ()), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return removed[name]
	})
	return append(environment, "GOWORK=off")
}

func runGoTestCompile(workdir string) commandResult {
	outputDir, err := os.MkdirTemp("", "goplint-targeted-mutation-compile-*")
	if err != nil {
		return commandResult{Err: err}
	}
	defer func() {
		if cleanupErr := os.RemoveAll(outputDir); cleanupErr != nil {
			fmt.Fprintln(os.Stderr, "targeted mutation compile cleanup:", cleanupErr)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), mutationGuardTimeout)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		"go",
		"test",
		"-c",
		"-o",
		filepath.Join(outputDir, "goplint.test"),
		"./goplint",
	)
	command.Dir = workdir
	command.Env = append(os.Environ(), "GOWORK=off")
	command.WaitDelay = mutationProcessWaitDelay
	output, runErr := command.CombinedOutput()
	return commandResult{Output: string(output), Err: runErr, TimedOut: ctx.Err() != nil}
}

func copySnapshot(snapshot sourceSnapshot) sourceSnapshot {
	return sourceSnapshot{Content: bytes.Clone(snapshot.Content), Mode: snapshot.Mode}
}
