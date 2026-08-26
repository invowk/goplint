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

func TestCaptureReusesVerifiedAggregateReportWithoutExecutingIt(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	report := newSyntheticTreeRunReport(t, fixture)
	options := fixture.captureOptions()
	options.ReuseAggregateReportPath = writeReusedReport(t, report)
	record, err := Capture(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "passed" || len(record.Commands) != 1 {
		t.Fatalf("Capture() with reuse = %+v", record)
	}
	outcome := record.Commands[0]
	if !outcome.Passed || outcome.ExitCode != 0 || outcome.DurationMS < 0 {
		t.Fatalf("reused aggregate outcome = %+v", outcome)
	}
	if !strings.HasPrefix(outcome.Log, reusedAggregateReportPrefix+record.AggregateReport.SHA256) {
		t.Fatalf("reused aggregate log = %q, want the reuse marker and report digest", outcome.Log)
	}
	if outcome.LogSHA256 != digestBytes([]byte(outcome.Log)) {
		t.Fatalf("reused aggregate log digest = %q", outcome.LogSHA256)
	}
	if record.AggregateReport.Report.WorkspaceDigest != report.WorkspaceDigest ||
		record.Provenance.CarriedReportSHA256 != record.AggregateReport.SHA256 {
		t.Fatalf("reused aggregate identity = %+v", record.AggregateReport)
	}
	if err := Verify(t.Context(), fixture.options); err != nil {
		t.Fatalf("Verify() of a reuse-generated record: %v", err)
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
	if err := Verify(t.Context(), fixture.options); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureRejectsReusedAggregateReportThatIsNotAProof(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// selectPath returns the reuse selection for the fixture; an empty
		// returned path means the report produced by report() is written to a
		// private file outside the repository.
		selectPath func(*testing.T, verifyFixture) string
		report     func(*testing.T, verifyFixture) soundnessgate.RunReport
		want       []string
	}{
		{
			name: "workspace digest mismatch",
			report: func(t *testing.T, fixture verifyFixture) soundnessgate.RunReport {
				t.Helper()
				return withWorkspaceDigest(
					newSyntheticTreeRunReport(t, fixture),
					soundnessevidence.DigestBytes([]byte(forgedDigestSeed)),
				)
			},
			want: []string{
				"aggregate report workspace digest",
				soundnessevidence.DigestBytes([]byte(forgedDigestSeed)),
			},
		},
		{
			name: "manifest digest mismatch",
			report: func(t *testing.T, fixture verifyFixture) soundnessgate.RunReport {
				t.Helper()
				return withManifestDigest(
					newSyntheticTreeRunReport(t, fixture),
					soundnessevidence.DigestBytes([]byte(forgedDigestSeed)),
				)
			},
			want: []string{
				"aggregate report manifest digest",
				soundnessevidence.DigestBytes([]byte(forgedDigestSeed)),
			},
		},
		{
			name: "profile mismatch",
			report: func(t *testing.T, fixture verifyFixture) soundnessgate.RunReport {
				t.Helper()
				report := newSyntheticTreeRunReport(t, fixture)
				report.Profile = soundnessgate.ProfileConsumer
				return report
			},
			want: []string{`aggregate report profile "consumer"`, `expected "semantic"`},
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
				options.ReuseAggregateReportPath = writeReusedReport(t, tt.report(t, fixture))
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

func TestRebindRejectsReusedAggregateReportSelection(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	options := fixture.captureOptions()
	options.ReuseAggregateReportPath = writeReusedReport(t, newSyntheticTreeRunReport(t, fixture))
	_, err := Rebind(t.Context(), options)
	if err == nil || !strings.Contains(err.Error(), "not applicable") {
		t.Fatalf("Rebind() error = %v, want a refused reuse selection", err)
	}
}

// newSyntheticTreeRunReport produces the aggregate report an earlier gate run
// over byte-identical content would have retained: it is bound to the workspace
// digest of an independently materialized synthetic worktree.
func newSyntheticTreeRunReport(t *testing.T, fixture verifyFixture) soundnessgate.RunReport {
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
	if err := materialization.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	return report
}

func writeReusedReport(t *testing.T, report soundnessgate.RunReport) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aggregate-report.json")
	writeTestJSONPath(t, path, report)
	return path
}

// withWorkspaceDigest rebinds every aggregate identity to another workspace so
// the report stays internally consistent: the rejection under test must be the
// exact-tree digest comparison, not a self-inconsistent report.
func withWorkspaceDigest(report soundnessgate.RunReport, digest string) soundnessgate.RunReport {
	report.WorkspaceDigest = digest
	report.Observations = slices.Clone(report.Observations)
	for index := range report.Observations {
		report.Observations[index].Binding.WorkspaceDigest = digest
	}
	return report
}

func withManifestDigest(report soundnessgate.RunReport, digest string) soundnessgate.RunReport {
	report.ManifestDigest = digest
	report.Observations = slices.Clone(report.Observations)
	for index := range report.Observations {
		report.Observations[index].Binding.ManifestDigest = digest
	}
	return report
}
