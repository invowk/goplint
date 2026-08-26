// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"strings"
	"testing"

	"github.com/invowk/goplint/internal/soundnessevidence"
)

func parityReferenceReport() RunReport {
	return RunReport{
		FormatVersion:   RunReportFormatVersion,
		Profile:         ProfileHarness,
		RunID:           "run-reference",
		WorkspaceDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestDigest:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Subgates: []SubgateResult{
			{
				ID:            "fixture-alpha",
				CommandDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				ReportDigest:  "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				Populations:   []Population{{ID: "fixture-cases", Count: 2}},
			},
			{
				ID:            "fixture-beta",
				CommandDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				ReportDigest:  "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				Populations:   []Population{{ID: "fixture-echoes", Count: 1}},
			},
		},
		Observations: []soundnessevidence.SemanticObservation{
			{
				FormatVersion:  soundnessevidence.ObservationFormatVersion,
				RegistrationID: "parity-fixture.artifact-parity",
				Category:       "parity-fixture",
				Binding:        soundnessevidence.ObservationBinding{RunID: "run-reference"},
			},
		},
	}
}

func TestCompareNormalizedRunReportsIgnoresRunScopedIdentity(t *testing.T) {
	t.Parallel()

	reference := parityReferenceReport()
	candidate := parityReferenceReport()
	candidate.RunID = "run-candidate"
	candidate.Observations[0].Binding.RunID = "run-candidate"
	candidate.Subgates[0].ReportDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if err := CompareNormalizedRunReports(reference, candidate); err != nil {
		t.Fatalf("CompareNormalizedRunReports() error = %v, want nil for run-scoped drift", err)
	}
}

func TestCompareNormalizedRunReportsFailsClosedOnEvidenceDivergence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(report *RunReport)
		keyword string
	}{
		{
			name: "lost observation",
			mutate: func(report *RunReport) {
				report.Observations = nil
			},
			keyword: "diverge",
		},
		{
			name: "population count mismatch",
			mutate: func(report *RunReport) {
				report.Subgates[0].Populations[0].Count = 1
			},
			keyword: "fixture-cases",
		},
		{
			name: "lost subgate result",
			mutate: func(report *RunReport) {
				report.Subgates = report.Subgates[:1]
			},
			keyword: "diverge",
		},
		{
			name: "different verdict population",
			mutate: func(report *RunReport) {
				report.Subgates[1].Populations[0].ID = "fixture-echoes-renamed"
			},
			keyword: "fixture-echoes",
		},
		{
			name: "workspace identity drift",
			mutate: func(report *RunReport) {
				report.WorkspaceDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
			},
			keyword: "diverge",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			reference := parityReferenceReport()
			candidate := parityReferenceReport()
			testCase.mutate(&candidate)
			err := CompareNormalizedRunReports(reference, candidate)
			if err == nil {
				t.Fatal("CompareNormalizedRunReports() accepted divergent evidence")
			}
			if !strings.Contains(err.Error(), testCase.keyword) {
				t.Fatalf("CompareNormalizedRunReports() error = %q, want mention of %q", err, testCase.keyword)
			}
		})
	}
}
