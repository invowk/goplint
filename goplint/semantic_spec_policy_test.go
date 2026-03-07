// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestSemanticSpecInconclusivePolicyE2E(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		policy        string
		wantPresent   bool
		wantWarnMeta  bool
		allowWantDiff bool
	}{
		{name: "error emits inconclusive finding", policy: cfgInconclusivePolicyError, wantPresent: true},
		{name: "warn emits inconclusive with warning metadata", policy: cfgInconclusivePolicyWarn, wantPresent: true, wantWarnMeta: true},
		{name: "off suppresses inconclusive finding", policy: cfgInconclusivePolicyOff, wantPresent: false, allowWantDiff: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newAnalyzerHarness()
			resetFlags(t, h)
			setFlag(t, h.Analyzer, "check-cast-validation", "true")
			setFlag(t, h.Analyzer, "cfg-max-states", "1")
			setFlag(t, h.Analyzer, "cfg-inconclusive-policy", tt.policy)

			diagnostics, analysisErrors, results := collectDiagnosticsForPackages(t, h.Analyzer, "cfa_cast_inconclusive")
			for _, result := range results {
				if result != nil && result.Err != nil {
					t.Fatalf("analysis result error: %v", result.Err)
				}
			}

			if !tt.allowWantDiff && len(analysisErrors) > 0 {
				t.Fatalf("unexpected analysistest errors: %v", analysisErrors)
			}

			foundInconclusive := false
			for _, diag := range diagnostics {
				if diag.Category != CategoryUnvalidatedCastInconclusive {
					continue
				}
				foundInconclusive = true
				if got := FindingMetaFromDiagnosticURL(diag.URL, "cfg_inconclusive_policy"); got != tt.policy {
					t.Fatalf("cfg_inconclusive_policy = %q, want %q", got, tt.policy)
				}
				if tt.wantWarnMeta {
					if got := FindingMetaFromDiagnosticURL(diag.URL, "cfg_inconclusive_severity"); got != "warning" {
						t.Fatalf("cfg_inconclusive_severity = %q, want %q", got, "warning")
					}
				}
			}

			if foundInconclusive != tt.wantPresent {
				t.Fatalf("inconclusive presence = %t, want %t", foundInconclusive, tt.wantPresent)
			}
		})
	}
}
