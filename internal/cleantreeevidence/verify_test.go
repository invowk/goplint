// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func TestVerifyAcceptsCurrentTreeAndPreservesCaller(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	before, err := SnapshotCallerState(t.Context(), fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(t.Context(), fixture.options); err != nil {
		t.Fatal(err)
	}
	after, err := SnapshotCallerState(t.Context(), fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("Verify mutated caller state: before=%+v after=%+v", before, after)
	}
}

func TestCaptureRoundTripProducesVerifiableRecord(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	record, err := Capture(t.Context(), CaptureOptions{
		Root:         fixture.root,
		PathsPath:    fixture.options.PathsPath,
		PlanPath:     fixture.options.PlanPath,
		EvidencePath: fixture.options.EvidencePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "passed" || len(record.Commands) != 1 || !record.Commands[0].Passed {
		t.Fatalf("Capture() = %+v", record)
	}
	if len(record.DiffCensus.Changes) == 0 ||
		!slicesEqual(record.DiffCensus.AuthorizedOutputs, []string{"evidence.json", "evidence.json.tmp"}) {
		t.Fatalf("Capture() complete-diff census = %+v", record.DiffCensus)
	}
	if err := Verify(t.Context(), fixture.options); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsCompleteDiffAndDependencyStateFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*testing.T, verifyFixture)
		want   string
	}{
		{
			name: "omitted tracked path",
			mutate: func(t *testing.T, fixture verifyFixture) {
				t.Helper()
				writeTestFile(t, fixture.root, "omitted-tracked.txt", "not reviewed\n")
				runTestGit(t, fixture.root, "add", "omitted-tracked.txt")
			},
			want: "silently omitted",
		},
		{
			name: "omitted untracked path",
			mutate: func(t *testing.T, fixture verifyFixture) {
				t.Helper()
				writeTestFile(t, fixture.root, "omitted-untracked.txt", "not reviewed\n")
			},
			want: "silently omitted",
		},
		{
			name: "stale artifact",
			mutate: func(t *testing.T, fixture verifyFixture) {
				t.Helper()
				writeTestFile(t, fixture.root, "input.txt", "stale artifact\n")
				refreshFixtureDiffCensus(t, fixture)
			},
			want: "retained input identities",
		},
		{
			name: "partial predecessor task state",
			mutate: func(t *testing.T, fixture verifyFixture) {
				t.Helper()
				writeTestFile(
					t,
					fixture.root,
					"openspec/changes/complete-goplint-soundness-hardening/tasks.md",
					"- [ ] 12.9 Review final tree\n- [ ] 12.10 Archive\n",
				)
				refreshFixtureDiffCensus(t, fixture)
			},
			want: "pending IDs",
		},
		{
			name: "unjustified exclusion",
			mutate: func(t *testing.T, fixture verifyFixture) {
				t.Helper()
				plan, err := LoadPlan(resolveFromRoot(fixture.root, fixture.options.PlanPath))
				if err != nil {
					t.Fatal(err)
				}
				plan.DiffReview.ReviewedExclusions = []ReviewedExclusion{{Path: "unrelated.txt"}}
				writeTestJSON(t, fixture.root, fixture.options.PlanPath, plan)
			},
			want: "trimmed nonempty reason",
		},
		{
			name: "wrong archive order",
			mutate: func(t *testing.T, fixture verifyFixture) {
				t.Helper()
				plan, err := LoadPlan(resolveFromRoot(fixture.root, fixture.options.PlanPath))
				if err != nil {
					t.Fatal(err)
				}
				plan.TaskLedgers[0], plan.TaskLedgers[1] = plan.TaskLedgers[1], plan.TaskLedgers[0]
				writeTestJSON(t, fixture.root, fixture.options.PlanPath, plan)
			},
			want: "dependency order",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newVerifyFixture(t)
			tt.mutate(t, fixture)
			before, err := SnapshotCallerState(t.Context(), fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			err = Verify(t.Context(), fixture.options)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Verify() error = %v, want %q", err, tt.want)
			}
			after, snapshotErr := SnapshotCallerState(t.Context(), fixture.root)
			if snapshotErr != nil {
				t.Fatal(snapshotErr)
			}
			if before != after {
				t.Fatalf("failed Verify mutated caller state: before=%+v after=%+v", before, after)
			}
		})
	}
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func refreshFixtureDiffCensus(t *testing.T, fixture verifyFixture) {
	t.Helper()
	plan, err := LoadPlan(resolveFromRoot(fixture.root, fixture.options.PlanPath))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := LoadPathSelection(resolveFromRoot(fixture.root, fixture.options.PathsPath))
	if err != nil {
		t.Fatal(err)
	}
	record, err := LoadRecord(resolveFromRoot(fixture.root, fixture.options.EvidencePath))
	if err != nil {
		t.Fatal(err)
	}
	materialization, err := Materialize(t.Context(), fixture.root, fixture.options.PathsPath, false)
	if err != nil {
		t.Fatal(err)
	}
	record.Repository = materialization.Identity
	if err := materialization.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	record.DiffCensus, err = collectDiffCensus(
		t.Context(),
		fixture.root,
		record.Repository.BaseCommit,
		paths,
		plan.DiffReview,
		[]string{fixture.options.EvidencePath, fixture.options.EvidencePath + ".tmp"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRecord(resolveFromRoot(fixture.root, fixture.options.EvidencePath), record); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyReplaysRetainedBaseAfterProofCommit(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	record, err := LoadRecord(resolveFromRoot(fixture.root, fixture.options.EvidencePath))
	if err != nil {
		t.Fatal(err)
	}
	runTestGit(t, fixture.root, "add", "-A")
	runTestGit(
		t,
		fixture.root,
		"-c",
		"user.name=test",
		"-c",
		"user.email=test@invalid",
		"commit",
		"--quiet",
		"-m",
		"retain proof",
	)
	currentHead, err := gitOutput(t.Context(), fixture.root, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if currentHead == record.Repository.BaseCommit {
		t.Fatal("test precondition: retained base did not precede the proof commit")
	}
	before, err := SnapshotCallerState(t.Context(), fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(t.Context(), fixture.options); err != nil {
		t.Fatal(err)
	}
	after, err := SnapshotCallerState(t.Context(), fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("post-commit Verify mutated caller state: before=%+v after=%+v", before, after)
	}
}

func TestCaptureRejectsSuccessfulCommandWithoutAggregateReport(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	plan, err := LoadPlan(resolveFromRoot(fixture.root, fixture.options.PlanPath))
	if err != nil {
		t.Fatal(err)
	}
	plan.Commands[0].Args = []string{"git", "--version"}
	writeTestJSON(t, fixture.root, fixture.options.PlanPath, plan)
	record, err := Capture(t.Context(), CaptureOptions{
		Root:         fixture.root,
		PathsPath:    fixture.options.PathsPath,
		PlanPath:     fixture.options.PlanPath,
		EvidencePath: fixture.options.EvidencePath,
	})
	if err == nil {
		t.Fatal("Capture() accepted a successful no-op without aggregate evidence")
	}
	if record.Status != "failed" || len(record.Commands) != 1 || !record.Commands[0].Passed {
		t.Fatalf("Capture() failed record = %+v", record)
	}
}

func TestVerifyRejectsSelectedTrackedOrUntrackedDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "tracked", path: "tracked.txt"},
		{name: "selected untracked", path: "input.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newVerifyFixture(t)
			writeTestFile(t, fixture.root, tt.path, "drift\n")
			before, err := SnapshotCallerState(t.Context(), fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			if err := Verify(t.Context(), fixture.options); err == nil {
				t.Fatal("Verify() succeeded for stale selected content")
			}
			after, err := SnapshotCallerState(t.Context(), fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatalf("failed Verify mutated caller state: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestVerifyRejectsTamperedRetainedEvidenceWithoutMutatingCaller(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{name: "tool version", mutate: func(record *Record) { record.Toolchain[0].Version = "forged" }},
		{name: "command vector", mutate: func(record *Record) { record.Commands[0].Args = []string{"true"} }},
		{name: "command log", mutate: func(record *Record) { record.Commands[0].Log = "forged" }},
		{name: "failed command", mutate: func(record *Record) { record.Commands[0].Passed = false }},
		{name: "recorder changed index", mutate: func(record *Record) { record.Preservation.IndexSHA256After = "changed" }},
		{name: "stale registry", mutate: func(record *Record) { record.AggregateReport.RegistrySHA256 = digestBytes([]byte("stale")) }},
		{name: "stale manifest binding", mutate: func(record *Record) {
			record.AggregateReport.Report.ManifestDigest = soundnessevidence.DigestBytes([]byte("stale"))
		}},
		{name: "stale workspace binding", mutate: func(record *Record) {
			record.AggregateReport.Report.WorkspaceDigest = soundnessevidence.DigestBytes([]byte("stale"))
		}},
		{name: "missing observation", mutate: func(record *Record) {
			record.AggregateReport.Report.Observations = record.AggregateReport.Report.Observations[1:]
		}},
		{name: "duplicate observation", mutate: func(record *Record) {
			record.AggregateReport.Report.Observations = append(
				record.AggregateReport.Report.Observations,
				record.AggregateReport.Report.Observations[0],
			)
		}},
		{name: "zero population", mutate: func(record *Record) { record.AggregateReport.Report.Subgates[0].Populations[1].Count = 0 }},
		{name: "noncausal mutation proof", mutate: func(record *Record) { record.MutationProofs[0].Restored = false }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newVerifyFixture(t)
			record, err := LoadRecord(resolveFromRoot(fixture.root, fixture.options.EvidencePath))
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(&record)
			if err := WriteRecord(resolveFromRoot(fixture.root, fixture.options.EvidencePath), record); err != nil {
				t.Fatal(err)
			}
			before, err := SnapshotCallerState(t.Context(), fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			if err := Verify(t.Context(), fixture.options); err == nil {
				t.Fatal("Verify() succeeded for tampered retained evidence")
			}
			after, err := SnapshotCallerState(t.Context(), fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatalf("failed Verify mutated caller state: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestCleanTreeAggregateHelperProcess(t *testing.T) {
	t.Parallel()

	reportPath := os.Getenv(soundnessgate.EnvReportPath)
	if reportPath == "" {
		return
	}
	report := createFixtureRunReport(t, ".")
	writeTestJSONPath(t, reportPath, report)
}

type verifyFixture struct {
	root    string
	options VerifyOptions
}

func newVerifyFixture(t *testing.T) verifyFixture {
	t.Helper()
	root := initializeTestRepository(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	aggregateCommand := []string{executable, "-test.run=^TestCleanTreeAggregateHelperProcess$"}
	gitVersionOutput, err := runCommand(t.Context(), root, nil, nil, "git", "--version")
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		FormatVersion: FormatVersion,
		Inputs: []InputPlan{
			{Name: "reviewed-input", Path: "input.txt"},
		},
		Toolchain: []ToolPlan{
			{
				Name:              "git",
				Command:           []string{"git", "--version"},
				RequiredVersionRE: "^" + regexp.QuoteMeta(string(gitVersionOutput[:len(gitVersionOutput)-1])) + "$",
			},
		},
		TaskLedgers: []TaskLedgerPlan{
			{
				Name:            "complete-goplint-soundness-hardening",
				Path:            "openspec/changes/complete-goplint-soundness-hardening/tasks.md",
				ExpectedPending: []string{"12.10"},
			},
			{
				Name:            "close-goplint-soundness-review-gaps",
				Path:            "openspec/changes/close-goplint-soundness-review-gaps/tasks.md",
				ExpectedPending: []string{"10.8"},
			},
			{
				Name:            "close-residual-goplint-soundness-gaps",
				Path:            "openspec/changes/close-residual-goplint-soundness-gaps/tasks.md",
				ExpectedPending: []string{},
			},
		},
		DiffReview: DiffReviewPlan{ReviewedExclusions: []ReviewedExclusion{}},
		Counterexamples: CounterexamplePlan{
			Path: "counterexamples.json",
			Required: []CounterexampleObservationPlan{
				{ID: "CE-1", Observation: "must report"},
			},
		},
		Commands: []CommandPlan{
			{Name: "proof", Args: aggregateCommand, TimeoutMinutes: 1},
		},
		AggregateReport: AggregateReportPlan{
			CommandName:  "proof",
			OutputFile:   "aggregate-report.json",
			ManifestPath: "manifest.json",
			RegistryPath: "registry.json",
			Profile:      soundnessgate.ProfileCore,
		},
		MutationProofs: []MutationProofPlan{
			{Name: "test-mutant", Observation: "test-mutation"},
		},
	}
	writeFixtureRegistryAndManifest(t, root, aggregateCommand)
	writeTestFile(t, root, "input.txt", "input\n")
	writeTestFile(
		t,
		root,
		"openspec/changes/complete-goplint-soundness-hardening/tasks.md",
		"- [x] 12.9 Review final tree\n- [ ] 12.10 Ready for archive authorization\n",
	)
	writeTestFile(
		t,
		root,
		"openspec/changes/close-goplint-soundness-review-gaps/tasks.md",
		"- [x] 10.7 Record proof\n- [ ] 10.8 Ready for archive authorization\n",
	)
	writeTestFile(
		t,
		root,
		"openspec/changes/close-residual-goplint-soundness-gaps/tasks.md",
		"- [x] 10.10 Ready for archive authorization\n",
	)
	writeTestJSON(t, root, "counterexamples.json", counterexampleInventory{
		FormatVersion: FormatVersion,
		Counterexamples: []counterexampleInventoryEntry{
			{ID: "CE-1", Observation: "must report"},
		},
	})
	writeTestJSON(t, root, "plan.json", plan)
	writeTestFile(t, root, "tracked.txt", "selected tracked drift\n")
	runTestGit(t, root, "add", "input.txt")
	writeTestFile(t, root, "paths.txt", "counterexamples.json\ninput.txt\nmanifest.json\nopenspec/changes/close-goplint-soundness-review-gaps/tasks.md\nopenspec/changes/close-residual-goplint-soundness-gaps/tasks.md\nopenspec/changes/complete-goplint-soundness-hardening/tasks.md\npaths.txt\nplan.json\nregistry.json\ntracked.txt\n")
	materialization, err := Materialize(t.Context(), root, "paths.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	identity := materialization.Identity
	paths, err := LoadPathSelection(resolveFromRoot(root, "paths.txt"))
	if err != nil {
		t.Fatal(err)
	}
	diffCensus, err := collectDiffCensus(
		t.Context(),
		root,
		identity.BaseCommit,
		paths,
		plan.DiffReview,
		[]string{"evidence.json", "evidence.json.tmp"},
	)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := collectInputs(root, "plan.json", "paths.txt", plan)
	if err != nil {
		t.Fatal(err)
	}
	toolchain, err := collectToolchain(t.Context(), root, plan)
	if err != nil {
		t.Fatal(err)
	}
	taskLedgers, err := collectTaskLedgers(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	counterexamples, err := collectCounterexamples(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	aggregateReport, err := validateAggregateReport(
		t.Context(),
		materialization.Worktree,
		plan.AggregateReport,
		createFixtureRunReport(t, materialization.Worktree),
	)
	if err != nil {
		t.Fatal(err)
	}
	mutationProofs, err := collectMutationProofs(plan, aggregateReport.Report)
	if err != nil {
		t.Fatal(err)
	}
	if err := materialization.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	record := Record{
		FormatVersion:   FormatVersion,
		Status:          "passed",
		StartedAt:       now,
		FinishedAt:      now,
		Repository:      identity,
		DiffCensus:      diffCensus,
		Inputs:          inputs,
		Toolchain:       toolchain,
		TaskLedgers:     taskLedgers,
		Counterexamples: counterexamples,
		Commands: []CommandOutcome{
			{
				Name:         "proof",
				Args:         aggregateCommand,
				VectorSHA256: commandVectorDigest("", aggregateCommand),
				LogSHA256:    digestBytes(nil),
				Passed:       true,
			},
		},
		AggregateReport: aggregateReport,
		MutationProofs:  mutationProofs,
		Preservation: PreservationIdentity{
			IndexSHA256Before:    digestBytes([]byte("unchanged-index")),
			IndexSHA256After:     digestBytes([]byte("unchanged-index")),
			WorktreeSHA256Before: digestBytes([]byte("unchanged-worktree")),
			WorktreeSHA256After:  digestBytes([]byte("unchanged-worktree")),
		},
	}
	writeTestJSON(t, root, "evidence.json", record)
	return verifyFixture{
		root: root,
		options: VerifyOptions{
			Root:         root,
			PathsPath:    "paths.txt",
			PlanPath:     "plan.json",
			EvidencePath: "evidence.json",
		},
	}
}

func writeFixtureRegistryAndManifest(t *testing.T, root string, aggregateCommand []string) {
	t.Helper()
	maximum := 1
	registry := soundnessevidence.Registry{
		FormatVersion: soundnessevidence.RegistryFormatVersion,
		Registrations: []soundnessevidence.Registration{
			{
				ID:         "test-must-report",
				Category:   "test-category",
				Layer:      soundnessevidence.LayerMustReport,
				FeatureID:  "test-feature",
				ProducerID: "proof",
				TestID:     "TestCleanTreeAggregateHelperProcess",
				Boundary:   soundnessevidence.BoundaryProductionAnalyzer,
				Expected: soundnessevidence.Expectation{
					Outcome:      soundnessevidence.OutcomeMustReport,
					MinimumCases: 1,
					Diagnostics: soundnessevidence.CountRange{
						Minimum: 1,
						Maximum: &maximum,
					},
					RequiredStages: []soundnessevidence.ExecutionStage{
						soundnessevidence.StageReporting,
					},
				},
			},
			{
				ID:         "test-mutation",
				Category:   "test-category-mutation",
				Layer:      soundnessevidence.LayerMutation,
				FeatureID:  "test-mutation-feature",
				ProducerID: "proof",
				TestID:     "TestCleanTreeAggregateHelperProcessMutation",
				Boundary:   soundnessevidence.BoundaryMutationRunner,
				Expected: soundnessevidence.Expectation{
					Outcome:      soundnessevidence.OutcomeMutantKilled,
					MinimumCases: 1,
					Diagnostics:  soundnessevidence.CountRange{},
					RequiredStages: []soundnessevidence.ExecutionStage{
						soundnessevidence.StageSourceExtraction,
						soundnessevidence.StageIdentity,
						soundnessevidence.StageGraphConstruction,
						soundnessevidence.StagePropagation,
						soundnessevidence.StageAggregation,
						soundnessevidence.StageReporting,
					},
					RequiredProperties: causalMutationProperties,
					RequiredDimensions: []string{"test-mutant"},
				},
			},
		},
	}
	manifest := soundnessgate.Manifest{
		FormatVersion: soundnessgate.ManifestFormatVersion,
		RegistryPath:  "registry.json",
		Profiles: []soundnessgate.Profile{
			{ID: soundnessgate.ProfileCore, SubgateIDs: []string{"proof"}},
			{ID: soundnessgate.ProfileComplete, SubgateIDs: []string{"clean-tree-freshness", "proof"}},
		},
		Subgates: []soundnessgate.Subgate{
			{
				ID:                      "clean-tree-freshness",
				WorkingDirectory:        ".",
				Command:                 []string{"true"},
				TimeoutSeconds:          60,
				ReportFile:              "clean-tree-report.json",
				RequiredRegistrationIDs: []string{},
				RequiredPopulations: []soundnessgate.PopulationRequirement{
					{ID: "verified-clean-tree-records", Minimum: 1},
				},
			},
			{
				ID:                      "proof",
				WorkingDirectory:        ".",
				Command:                 aggregateCommand,
				TimeoutSeconds:          60,
				ReportFile:              "proof-report.json",
				RequiredRegistrationIDs: []string{"test-must-report", "test-mutation"},
				RequiredPopulations: []soundnessgate.PopulationRequirement{
					{ID: "cases", Minimum: 1},
					{ID: "causal-mutants", Minimum: 1},
					{ID: "clean-controls", Minimum: 2},
					{ID: "intended-mismatches", Minimum: 1},
					{ID: "restorations", Minimum: 1},
					{ID: "selected-guards", Minimum: 1},
				},
			},
		},
	}
	writeTestJSON(t, root, "registry.json", registry)
	writeTestJSON(t, root, "manifest.json", manifest)
}

func createFixtureRunReport(t *testing.T, root string) soundnessgate.RunReport {
	t.Helper()
	manifest, manifestDigest, err := soundnessgate.LoadManifest(t.Context(), resolveFromRoot(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceDigest, err := soundnessgate.WorkspaceDigest(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	proofSubgates, err := manifest.SubgatesForProfile(soundnessgate.ProfileCore)
	if err != nil {
		t.Fatal(err)
	}
	commandDigest, err := soundnessgate.CommandDigest(proofSubgates[0])
	if err != nil {
		t.Fatal(err)
	}
	binding := soundnessevidence.ObservationBinding{
		RunID:           "test-run",
		WorkspaceDigest: workspaceDigest,
		ManifestDigest:  manifestDigest,
		CommandDigest:   commandDigest,
		SubgateID:       "proof",
	}
	report := soundnessgate.RunReport{
		FormatVersion:   soundnessgate.RunReportFormatVersion,
		Profile:         soundnessgate.ProfileCore,
		RunID:           binding.RunID,
		WorkspaceDigest: workspaceDigest,
		ManifestDigest:  manifestDigest,
		Subgates: []soundnessgate.SubgateResult{
			{
				ID:            "proof",
				CommandDigest: commandDigest,
				ReportDigest:  soundnessevidence.DigestBytes([]byte("proof-report")),
				Populations: []soundnessgate.Population{
					{ID: "cases", Count: 1},
					{ID: "causal-mutants", Count: 1},
					{ID: "clean-controls", Count: 2},
					{ID: "intended-mismatches", Count: 1},
					{ID: "restorations", Count: 1},
					{ID: "selected-guards", Count: 1},
				},
			},
		},
		Observations: []soundnessevidence.SemanticObservation{
			{
				FormatVersion:  soundnessevidence.ObservationFormatVersion,
				Binding:        binding,
				RegistrationID: "test-must-report",
				Category:       "test-category",
				Layer:          soundnessevidence.LayerMustReport,
				FeatureID:      "test-feature",
				ProducerID:     "proof",
				TestID:         "TestCleanTreeAggregateHelperProcess",
				Boundary:       soundnessevidence.BoundaryProductionAnalyzer,
				ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
					soundnessevidence.BoundaryProductionAnalyzer,
				),
				Result: soundnessevidence.ObservationResult{
					Outcome:         soundnessevidence.OutcomeMustReport,
					CaseCount:       1,
					DiagnosticCount: 1,
				},
				Stages: []soundnessevidence.ExecutionStage{
					soundnessevidence.StageReporting,
				},
				Cases: []soundnessevidence.SemanticCase{
					{
						ID:        "test-must-report/case-001",
						Category:  "test-category",
						Layer:     soundnessevidence.LayerMustReport,
						FeatureID: "test-feature",
						Boundary:  soundnessevidence.BoundaryProductionAnalyzer,
						ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
							soundnessevidence.BoundaryProductionAnalyzer,
						),
						Outcome:         soundnessevidence.OutcomeMustReport,
						DiagnosticCount: 1,
						Stages: []soundnessevidence.ExecutionStage{
							soundnessevidence.StageReporting,
						},
					},
				},
			},
			{
				FormatVersion:  soundnessevidence.ObservationFormatVersion,
				Binding:        binding,
				RegistrationID: "test-mutation",
				Category:       "test-category-mutation",
				Layer:          soundnessevidence.LayerMutation,
				FeatureID:      "test-mutation-feature",
				ProducerID:     "proof",
				TestID:         "TestCleanTreeAggregateHelperProcessMutation",
				Boundary:       soundnessevidence.BoundaryMutationRunner,
				ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
					soundnessevidence.BoundaryMutationRunner,
				),
				Result: soundnessevidence.ObservationResult{
					Outcome:   soundnessevidence.OutcomeMutantKilled,
					CaseCount: 1,
				},
				Stages: []soundnessevidence.ExecutionStage{
					soundnessevidence.StageSourceExtraction,
					soundnessevidence.StageIdentity,
					soundnessevidence.StageGraphConstruction,
					soundnessevidence.StagePropagation,
					soundnessevidence.StageAggregation,
					soundnessevidence.StageReporting,
				},
				Properties: causalMutationProperties,
				Dimensions: []string{"test-mutant"},
				Cases: []soundnessevidence.SemanticCase{
					{
						ID:        "test-mutation/case-001",
						Category:  "test-category-mutation",
						Layer:     soundnessevidence.LayerMutation,
						FeatureID: "test-mutation-feature",
						Boundary:  soundnessevidence.BoundaryMutationRunner,
						ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
							soundnessevidence.BoundaryMutationRunner,
						),
						Outcome: soundnessevidence.OutcomeMutantKilled,
						Stages: []soundnessevidence.ExecutionStage{
							soundnessevidence.StageSourceExtraction,
							soundnessevidence.StageIdentity,
							soundnessevidence.StageGraphConstruction,
							soundnessevidence.StagePropagation,
							soundnessevidence.StageAggregation,
							soundnessevidence.StageReporting,
						},
						Properties: causalMutationProperties,
						Dimensions: []string{"test-mutant"},
					},
				},
			},
		},
	}
	registry, err := soundnessevidence.LoadRegistry(t.Context(), resolveFromRoot(root, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := soundnessgate.ValidateRunReport(report, manifest, registry); err != nil {
		t.Fatal(err)
	}
	return report
}

func writeTestJSON(t *testing.T, root, relative string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := writeTestFile(t, root, relative, string(data))
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func writeTestJSONPath(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
