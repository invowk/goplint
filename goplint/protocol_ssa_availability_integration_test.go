// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/ssa"
)

func TestProtocolAdaptersFailClosedForTypedSSAUnavailability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fixture    string
		ssaBuilder func(*analysis.Pass) *ssaResult
		wantStatus ssaAvailabilityStatus
		wantCount  int
	}{
		{
			name:       "build failure",
			fixture:    "protocol_ssa_build_failure",
			wantStatus: ssaAvailabilityBuildFailure,
			wantCount:  4,
			ssaBuilder: func(*analysis.Pass) *ssaResult {
				return &ssaResult{Availability: ssaAvailability{
					Status: ssaAvailabilityBuildFailure,
					Detail: "injected-build-failure",
				}}
			},
		},
		{
			name:       "missing function",
			fixture:    "protocol_ssa_missing_function",
			wantStatus: ssaAvailabilityMissingFunction,
			wantCount:  4,
			ssaBuilder: func(pass *analysis.Pass) *ssaResult {
				result := buildSSAForPass(pass)
				result.functionResolver = func(*types.Func) *ssa.Function { return nil }
				return result
			},
		},
		{
			name:       "missing closure",
			fixture:    "protocol_ssa_missing_closure",
			wantStatus: ssaAvailabilityMissingClosure,
			wantCount:  2,
			ssaBuilder: func(pass *analysis.Pass) *ssaResult {
				result := buildSSAForPass(pass)
				result.closureResolver = func(token.Pos) *ssa.Function { return nil }
				return result
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newAnalyzerHarness()
			resetFlags(t, h)
			h.state.ssaBuilder = tt.ssaBuilder
			setFlag(t, h.Analyzer, "check-cast-validation", "true")
			setFlag(t, h.Analyzer, "check-use-before-validate", "true")
			setFlag(t, h.Analyzer, "check-constructor-validates", "true")
			setFlag(t, h.Analyzer, "check-boundary-request-validation", "true")
			results := runAnalysisTest(t, analysistest.TestData(), h.Analyzer, tt.fixture)
			statusCount := 0
			for _, result := range results {
				for _, diagnostic := range result.Diagnostics {
					status := FindingMetaFromDiagnosticURL(diagnostic.URL, "ssa_availability_status")
					if status == "" {
						continue
					}
					statusCount++
					if status != string(tt.wantStatus) {
						t.Errorf("diagnostic %q SSA status = %q, want %q", diagnostic.Message, status, tt.wantStatus)
					}
				}
			}
			if statusCount != tt.wantCount {
				t.Errorf("SSA-qualified diagnostic count = %d, want %d", statusCount, tt.wantCount)
			}
		})
	}
}
