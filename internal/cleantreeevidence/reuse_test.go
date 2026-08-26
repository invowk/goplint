// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/invowk/goplint/internal/soundnessevidence"
	"github.com/invowk/goplint/internal/soundnessgate"
)

const forgedDigestSeed = "forged-reuse-identity"

// TestCaptureAdmitsTreeAndToolchainBoundReportAndRecordsReuse covers exactly
// what reuse establishes: the report is bound to the synthetic tree, the
// manifest, the registry, and the producing toolchain, and the record states
// reuse through a distinct provenance kind. It does not establish that the
// aggregate populations were executed — the reused report is a self-attested
// census, which is why the default freshness verifier refuses the record.
func TestCaptureAdmitsTreeAndToolchainBoundReportAndRecordsReuse(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	pair := newReusedReportPair(t, fixture)
	options := fixture.captureOptions()
	options.ReuseAggregateReportPath = publishReusedReport(t, pair)
	record, err := Capture(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "passed" || len(record.Commands) != 1 {
		t.Fatalf("Capture() with reuse = %+v", record)
	}
	if record.Provenance.Kind != ProvenanceReused {
		t.Fatalf("Capture() provenance kind = %q, want %q", record.Provenance.Kind, ProvenanceReused)
	}
	outcome := record.Commands[0]
	if !outcome.Passed || outcome.ExitCode != 0 || outcome.DurationMS < 0 {
		t.Fatalf("reused aggregate outcome = %+v", outcome)
	}
	if !strings.HasPrefix(outcome.Log, reusedAggregateReportPrefix+record.AggregateReport.SHA256) {
		t.Fatalf("reused aggregate log = %q, want the reuse marker and report digest", outcome.Log)
	}
	for _, want := range []string{pair.binding.PlanID, pair.binding.Toolchain.Digest} {
		if !strings.Contains(outcome.Log, want) {
			t.Fatalf("reused aggregate log = %q, want producing binding %q", outcome.Log, want)
		}
	}
	if outcome.LogSHA256 != digestBytes([]byte(outcome.Log)) {
		t.Fatalf("reused aggregate log digest = %q", outcome.LogSHA256)
	}
	if record.AggregateReport.Report.WorkspaceDigest != pair.report.WorkspaceDigest ||
		record.Provenance.CarriedReportSHA256 != record.AggregateReport.SHA256 {
		t.Fatalf("reused aggregate identity = %+v", record.AggregateReport)
	}
	options.ReuseAggregateReportPath = ""
	if err := Verify(t.Context(), fixture.options); err == nil {
		t.Fatal("Verify() accepted a reused-aggregate record without the explicit opt-in")
	}
	allowing := fixture.options
	allowing.AllowReusedAggregate = true
	if err := Verify(t.Context(), allowing); err != nil {
		t.Fatalf("Verify() with -allow-reused-aggregate: %v", err)
	}
}

func TestVerifyRefusesReusedRecordWithoutOptInNamingRegeneration(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	record, err := LoadRecord(resolveFromRoot(fixture.root, fixture.options.EvidencePath))
	if err != nil {
		t.Fatal(err)
	}
	record.Provenance.Kind = ProvenanceReused
	if err := WriteRecord(resolveFromRoot(fixture.root, fixture.options.EvidencePath), record); err != nil {
		t.Fatal(err)
	}
	err = Verify(t.Context(), fixture.options)
	if err == nil {
		t.Fatal("Verify() accepted a reused-aggregate record by default")
	}
	for _, want := range []string{
		"reused from a caller-provided report",
		"make generate-goplint-clean-tree-evidence",
		"-allow-reused-aggregate",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Verify() error = %v, want %q", err, want)
		}
	}
}

func TestRebindRefusesToCarryReusedProvenanceForward(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	pair := newReusedReportPair(t, fixture)
	options := fixture.captureOptions()
	options.ReuseAggregateReportPath = publishReusedReport(t, pair)
	if _, err := Capture(t.Context(), options); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, fixture.root, "docs/notes.md", "revised prose notes\n")
	_, err := Rebind(t.Context(), fixture.captureOptions())
	if err == nil {
		t.Fatal("Rebind() carried a reused-aggregate record forward")
	}
	for _, want := range []string{ProvenanceReused, ProvenanceRebound, "fresh gate execution"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Rebind() error = %v, want %q", err, want)
		}
	}
}

func TestCaptureExecutesAggregateCommandWithoutReuseSelection(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	record, err := Capture(t.Context(), fixture.captureOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Commands) != 1 || strings.Contains(record.Commands[0].Log, reusedAggregateReportPrefix) {
		t.Fatalf("default Capture() recorded reuse instead of execution: %+v", record.Commands)
	}
	if record.Provenance.Kind != ProvenanceGenerated {
		t.Fatalf("default Capture() provenance kind = %q, want %q", record.Provenance.Kind, ProvenanceGenerated)
	}
	if err := Verify(t.Context(), fixture.options); err != nil {
		t.Fatal(err)
	}
}

func TestRebindRejectsReusedAggregateReportSelection(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	options := fixture.captureOptions()
	options.ReuseAggregateReportPath = publishReusedReport(t, newReusedReportPair(t, fixture))
	_, err := Rebind(t.Context(), options)
	if err == nil || !strings.Contains(err.Error(), "not applicable") {
		t.Fatalf("Rebind() error = %v, want a refused reuse selection", err)
	}
}

func TestCaptureRejectsReusedAggregateReportThatIsNotAProof(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// selectPath returns the reuse selection directly; when nil, mutate
		// rewrites the valid report/binding pair before it is published.
		selectPath func(*testing.T, verifyFixture) string
		mutate     func(*testing.T, *reusedReportPair)
		want       []string
	}{
		{
			name: "workspace digest mismatch",
			mutate: func(t *testing.T, pair *reusedReportPair) {
				t.Helper()
				pair.rebindWorkspaceDigest(t, soundnessevidence.DigestBytes([]byte(forgedDigestSeed)))
			},
			want: []string{
				"aggregate report workspace digest",
				soundnessevidence.DigestBytes([]byte(forgedDigestSeed)),
			},
		},
		{
			name: "manifest digest mismatch",
			mutate: func(t *testing.T, pair *reusedReportPair) {
				t.Helper()
				pair.rebindManifestDigest(t, soundnessevidence.DigestBytes([]byte(forgedDigestSeed)))
			},
			want: []string{
				"aggregate report manifest digest",
				soundnessevidence.DigestBytes([]byte(forgedDigestSeed)),
			},
		},
		{
			// The binding profile is decidable without the synthetic tree, so it
			// is rejected before the plan starts executing.
			name: "binding profile mismatch",
			mutate: func(t *testing.T, pair *reusedReportPair) {
				t.Helper()
				pair.report.Profile = soundnessgate.ProfileConsumer
				pair.binding.Profile = soundnessgate.ProfileConsumer
				pair.rebindReportDigest(t)
			},
			want: []string{`binding profile "consumer"`, `expected "semantic"`},
		},
		{
			// A report whose own profile drifts from a still-consistent binding
			// is rejected by the tree-bound admission check.
			name: "report profile mismatch",
			mutate: func(t *testing.T, pair *reusedReportPair) {
				t.Helper()
				pair.report.Profile = soundnessgate.ProfileConsumer
				pair.rebindReportDigest(t)
			},
			want: []string{`aggregate report profile "consumer"`, `expected "semantic"`},
		},
		{
			name: "tampered subgate report digest",
			mutate: func(t *testing.T, pair *reusedReportPair) {
				t.Helper()
				// The per-subgate report_digest is not recomputable from any
				// retained artifact, but the companion binding pins the whole
				// report's canonical bytes, so post-production tampering is
				// still rejected.
				pair.report.Subgates[0].ReportDigest = soundnessevidence.DigestBytes([]byte(forgedDigestSeed))
			},
			want: []string{"companion binding", "binding report digest"},
		},
		{
			name: "toolchain drift replay",
			mutate: func(t *testing.T, pair *reusedReportPair) {
				t.Helper()
				drifted := soundnessgate.ToolchainBinding{GoVersion: "go0.0.1", GOOS: "plan9", GOARCH: "mips"}
				digest, err := soundnessgate.ToolchainBindingDigest(drifted)
				if err != nil {
					t.Fatal(err)
				}
				drifted.Digest = digest
				pair.binding.Toolchain = drifted
			},
			want: []string{"producing toolchain", "go0.0.1", "current"},
		},
		{
			name: "registry digest mismatch",
			mutate: func(t *testing.T, pair *reusedReportPair) {
				t.Helper()
				pair.binding.RegistryDigest = soundnessevidence.DigestBytes([]byte(forgedDigestSeed))
			},
			want: []string{"binding registry digest"},
		},
		{
			name: "missing companion binding",
			mutate: func(t *testing.T, pair *reusedReportPair) {
				t.Helper()
				pair.omitBinding = true
			},
			want: []string{"load companion binding", "GOPLINT_SOUNDNESS_REPORT_PATH"},
		},
		{
			name: "registry drops a registration",
			mutate: func(t *testing.T, pair *reusedReportPair) {
				t.Helper()
				registry := loadFixtureRegistry(t, pair.fixtureRoot)
				registry.Registrations = registry.Registrations[:1]
				writeTestJSON(t, pair.fixtureRoot, "registry.json", registry)
			},
			want: []string{"validate retained aggregate report", `requires extra registration "test-mutation"`},
		},
		{
			name: "registry gains a registration",
			mutate: func(t *testing.T, pair *reusedReportPair) {
				t.Helper()
				registry := loadFixtureRegistry(t, pair.fixtureRoot)
				extra := registry.Registrations[0]
				extra.ID = "test-must-report-extra"
				extra.Category = "test-category-extra"
				extra.FeatureID = "test-feature-extra"
				extra.TestID = "TestCleanTreeAggregateHelperProcessExtra"
				// The registry requires canonical ID order.
				registry.Registrations = slices.Insert(registry.Registrations, 1, extra)
				writeTestJSON(t, pair.fixtureRoot, "registry.json", registry)
			},
			want: []string{
				"validate retained aggregate report",
				`registration "test-must-report-extra" is missing from its producer subgate`,
			},
		},
		{
			name: "unparseable report",
			selectPath: func(t *testing.T, _ verifyFixture) string {
				t.Helper()
				path := filepath.Join(t.TempDir(), "aggregate-report.json")
				if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			},
			want: []string{"load reused aggregate run report", "decode"},
		},
		{
			name: "missing report",
			selectPath: func(t *testing.T, _ verifyFixture) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "absent-report.json")
			},
			want: []string{"inspect reused aggregate report"},
		},
		{
			name: "relative report path",
			selectPath: func(t *testing.T, _ verifyFixture) string {
				t.Helper()
				return filepath.Join("testdata", "aggregate-report.json")
			},
			want: []string{"must be absolute"},
		},
		{
			name: "report inside the repository",
			selectPath: func(t *testing.T, fixture verifyFixture) string {
				t.Helper()
				return writeTestFile(t, fixture.root, "aggregate-report.json", "{}\n")
			},
			want: []string{"must be outside the repository"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newVerifyFixture(t)
			options := fixture.captureOptions()
			switch {
			case tt.selectPath != nil:
				options.ReuseAggregateReportPath = tt.selectPath(t, fixture)
			default:
				pair := newReusedReportPair(t, fixture)
				tt.mutate(t, &pair)
				options.ReuseAggregateReportPath = publishReusedReport(t, pair)
			}
			evidencePath := resolveFromRoot(fixture.root, fixture.options.EvidencePath)
			before, err := os.ReadFile(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			_, captureErr := Capture(t.Context(), options)
			if captureErr == nil {
				t.Fatal("Capture() admitted a reused report that is not a proof for the synthetic tree")
			}
			for _, want := range tt.want {
				if !strings.Contains(captureErr.Error(), want) {
					t.Fatalf("Capture() error = %v, want %q", captureErr, want)
				}
			}
			after, err := os.ReadFile(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(before, after) {
				t.Fatal("rejected reuse rewrote the retained record")
			}
		})
	}
}

// reusedReportPair is one aggregate report plus the companion binding a
// producing run would have retained beside it.
type reusedReportPair struct {
	report      soundnessgate.RunReport
	binding     soundnessgate.RunReportBinding
	fixtureRoot string
	omitBinding bool
}

// newReusedReportPair produces the report and companion binding an earlier gate
// run over byte-identical content would have retained: both are bound to the
// workspace digest of an independently materialized synthetic worktree and to
// this process's toolchain.
func newReusedReportPair(t *testing.T, fixture verifyFixture) reusedReportPair {
	t.Helper()
	plan, err := LoadPlan(resolveFromRoot(fixture.root, fixture.options.PlanPath))
	if err != nil {
		t.Fatal(err)
	}
	materialization, err := Materialize(
		t.Context(),
		fixture.root,
		fixture.options.PathsPath,
		plan.OwnershipManifestPath,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	report := createFixtureRunReport(t, materialization.Worktree)
	registryDigest, err := digestFile(resolveFromRoot(materialization.Worktree, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := materialization.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	pair := reusedReportPair{report: report, fixtureRoot: fixture.root}
	pair.binding = newFixtureReportBinding(t, report, registryDigest)
	return pair
}

// newFixtureReportBinding derives the companion binding through the production
// constructor from the minimal plan bindings it reads.
func newFixtureReportBinding(
	t *testing.T,
	report soundnessgate.RunReport,
	registryDigest string,
) soundnessgate.RunReportBinding {
	t.Helper()
	toolchain, err := soundnessgate.CurrentToolchainBinding()
	if err != nil {
		t.Fatal(err)
	}
	binding, err := soundnessgate.DeriveRunReportBinding(soundnessgate.ExecutionPlan{
		PlanID:    soundnessevidence.DigestBytes([]byte("fixture-plan")),
		Profile:   report.Profile,
		Workspace: soundnessgate.WorkspaceBinding{Root: ".", Digest: report.WorkspaceDigest},
		Manifest:  soundnessgate.ArtifactBinding{Path: "manifest.json", Digest: report.ManifestDigest},
		Registry:  soundnessgate.ArtifactBinding{Path: "registry.json", Digest: registryDigest},
		Toolchain: toolchain,
		Resources: soundnessgate.ResourceBudget{
			CPUUnits: 1, MemoryBytes: 1024 * 1024, MaximumWorkers: 1, RunnerClass: "fixture",
		},
	}, report)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

// rebindWorkspaceDigest rewrites every aggregate identity to another workspace
// so the pair stays internally consistent: the rejection under test must be the
// exact-tree digest comparison, not a self-inconsistent report or binding.
func (pair *reusedReportPair) rebindWorkspaceDigest(t *testing.T, digest string) {
	t.Helper()
	pair.report.WorkspaceDigest = digest
	pair.report.Observations = slices.Clone(pair.report.Observations)
	for index := range pair.report.Observations {
		pair.report.Observations[index].Binding.WorkspaceDigest = digest
	}
	pair.binding.WorkspaceDigest = digest
	pair.rebindReportDigest(t)
}

func (pair *reusedReportPair) rebindManifestDigest(t *testing.T, digest string) {
	t.Helper()
	pair.report.ManifestDigest = digest
	pair.report.Observations = slices.Clone(pair.report.Observations)
	for index := range pair.report.Observations {
		pair.report.Observations[index].Binding.ManifestDigest = digest
	}
	pair.binding.ManifestDigest = digest
	pair.rebindReportDigest(t)
}

// rebindReportDigest re-pins the companion binding to the current report bytes.
func (pair *reusedReportPair) rebindReportDigest(t *testing.T) {
	t.Helper()
	digest, err := soundnessgate.CanonicalRunReportDigest(pair.report)
	if err != nil {
		t.Fatal(err)
	}
	pair.binding.ReportSHA256 = digest
}

// publishReusedReport writes the pair exactly as a producing run would, without
// revalidating it, so rejection tests can publish deliberately broken pairs.
func publishReusedReport(t *testing.T, pair reusedReportPair) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aggregate-report.json")
	writeTestJSONPath(t, path, pair.report)
	if !pair.omitBinding {
		writeTestJSONPath(t, soundnessgate.RunReportBindingPath(path), pair.binding)
	}
	return path
}

func loadFixtureRegistry(t *testing.T, root string) soundnessevidence.Registry {
	t.Helper()
	registry, err := soundnessevidence.LoadRegistry(t.Context(), resolveFromRoot(root, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
