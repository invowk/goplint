// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRunReportBindingRoundTripsAndPinsReportBytes(t *testing.T) {
	t.Parallel()

	plan, report := bindingFixture(t)
	binding, err := DeriveRunReportBinding(plan, report)
	if err != nil {
		t.Fatalf("DeriveRunReportBinding() error = %v", err)
	}
	reportPath := filepath.Join(t.TempDir(), "aggregate-report.json")
	if err := PublishRunReportBinding(t.Context(), reportPath, binding); err != nil {
		t.Fatalf("PublishRunReportBinding() error = %v", err)
	}
	loaded, err := LoadRunReportBinding(t.Context(), RunReportBindingPath(reportPath))
	if err != nil {
		t.Fatalf("LoadRunReportBinding() error = %v", err)
	}
	if loaded != binding {
		t.Fatalf("LoadRunReportBinding() = %+v, want %+v", loaded, binding)
	}
	digest, err := CanonicalRunReportDigest(report)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ReportSHA256 != digest {
		t.Fatalf("binding report digest = %q, want %q", loaded.ReportSHA256, digest)
	}
	report.Subgates[0].ReportDigest = "sha256:" + strings.Repeat("c", 64)
	tamperedDigest, err := CanonicalRunReportDigest(report)
	if err != nil {
		t.Fatal(err)
	}
	if tamperedDigest == digest {
		t.Fatal("canonical report digest ignored a mutated subgate report digest")
	}
}

func TestDeriveRunReportBindingRejectsMismatchedAndInvalidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*ExecutionPlan, *RunReport)
		want   string
	}{
		{
			name:   "plan profile mismatch",
			mutate: func(plan *ExecutionPlan, _ *RunReport) { plan.Profile = ProfileConsumer },
			want:   "do not match the plan",
		},
		{
			name: "plan workspace mismatch",
			mutate: func(plan *ExecutionPlan, _ *RunReport) {
				plan.Workspace.Digest = "sha256:" + strings.Repeat("d", 64)
			},
			want: "do not match the plan",
		},
		{
			name: "plan manifest mismatch",
			mutate: func(plan *ExecutionPlan, _ *RunReport) {
				plan.Manifest.Digest = "sha256:" + strings.Repeat("e", 64)
			},
			want: "do not match the plan",
		},
		{
			name:   "missing registry digest",
			mutate: func(plan *ExecutionPlan, _ *RunReport) { plan.Registry.Digest = "" },
			want:   "registry_digest",
		},
		{
			name:   "invalid resources",
			mutate: func(plan *ExecutionPlan, _ *RunReport) { plan.Resources.CPUUnits = 0 },
			want:   "resources",
		},
		{
			name: "self-inconsistent toolchain digest",
			mutate: func(plan *ExecutionPlan, _ *RunReport) {
				plan.Toolchain.GoVersion = "go0.0.1"
			},
			want: "toolchain digest does not match",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan, report := bindingFixture(t)
			tt.mutate(&plan, &report)
			_, err := DeriveRunReportBinding(plan, report)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("DeriveRunReportBinding() error = %v, want %q", err, tt.want)
			}
		})
	}
}

// bindingFixture returns one consistent plan/report pair whose only purpose is
// to exercise the companion binding contract.
func bindingFixture(t *testing.T) (ExecutionPlan, RunReport) {
	t.Helper()
	manifest := validGateManifest()
	subgates, err := manifest.SubgatesForProfile(ProfileSemantic)
	if err != nil {
		t.Fatal(err)
	}
	binding := validGateBinding(subgates[0])
	report := RunReport{
		FormatVersion:   RunReportFormatVersion,
		Profile:         ProfileSemantic,
		RunID:           binding.RunID,
		WorkspaceDigest: binding.WorkspaceDigest,
		ManifestDigest:  binding.ManifestDigest,
		Subgates: []SubgateResult{
			{
				ID:            subgates[0].ID,
				CommandDigest: binding.CommandDigest,
				ReportDigest:  "sha256:" + strings.Repeat("f", 64),
				Populations:   []Population{{ID: "cases", Count: 1}},
			},
		},
	}
	toolchain, err := CurrentToolchainBinding()
	if err != nil {
		t.Fatal(err)
	}
	plan := ExecutionPlan{
		PlanID:    "sha256:" + strings.Repeat("a", 64),
		Profile:   report.Profile,
		Workspace: WorkspaceBinding{Root: ".", Digest: report.WorkspaceDigest},
		Manifest:  ArtifactBinding{Path: "manifest.json", Digest: report.ManifestDigest},
		Registry:  ArtifactBinding{Path: "registry.json", Digest: "sha256:" + strings.Repeat("b", 64)},
		Toolchain: toolchain,
		Resources: ResourceBudget{CPUUnits: 1, MemoryBytes: 1024, MaximumWorkers: 1, RunnerClass: "fixture"},
	}
	return plan, report
}
