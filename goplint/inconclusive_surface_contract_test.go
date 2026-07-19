// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"go/token"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestMixedCategoryInconclusiveBlocksAllSuppressionSurfaces(t *testing.T) {
	t.Parallel()

	const (
		findingID = "gpl3_mixed_inconclusive_contract"
		message   = "boundary request proof remains inconclusive"
	)
	baseline := &BaselineConfig{lookupByID: map[string]map[string]bool{
		CategoryUnvalidatedBoundaryRequest: {findingID: true},
	}}
	if !baseline.ContainsFinding(CategoryUnvalidatedBoundaryRequest, findingID, message) {
		t.Fatal("test precondition: matching suppressible-category baseline entry was not active")
	}

	diagnostics := make([]analysis.Diagnostic, 0, 1)
	pass := &analysis.Pass{
		Report: func(diagnostic analysis.Diagnostic) {
			diagnostics = append(diagnostics, diagnostic)
		},
	}
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass,
		baseline,
		token.Pos(1),
		CategoryUnvalidatedBoundaryRequest,
		findingID,
		message,
		map[string]string{"cfg_inconclusive_reason": "contract-injection"},
	)
	if len(diagnostics) != 1 {
		t.Fatalf("production reporter emitted %d diagnostics, want 1 despite matching baseline", len(diagnostics))
	}
	meta := map[string]string{
		"cfg_inconclusive_reason": "contract-injection",
		"cfg_outcome_status":      cfgRefinementStatusInconclusive,
	}
	stream, err := json.Marshal(FindingStreamRecord{
		Category: CategoryUnvalidatedBoundaryRequest,
		ID:       findingID,
		Message:  message,
		Posn:     "contract.go:1:1",
		Meta:     meta,
	})
	if err != nil {
		t.Fatalf("Marshal(stream) error = %v", err)
	}
	stream = append(stream, '\n')
	if _, err := CollectBaselineFindingsFromStream(stream); err == nil || !strings.Contains(err.Error(), "always visible") {
		t.Fatalf("CollectBaselineFindingsFromStream() error = %v, want always-visible rejection", err)
	}
	if _, err := baselineStreamDiagnosticCounts(stream); err == nil || !strings.Contains(err.Error(), "always visible") {
		t.Fatalf("baselineStreamDiagnosticCounts() error = %v, want always-visible rejection", err)
	}

	analysisData, err := json.Marshal(AnalysisResult{
		"example.com/contract": {
			"goplint": {
				{
					Posn:     "contract.go:1:1",
					Message:  message,
					Category: CategoryUnvalidatedBoundaryRequest,
					URL:      diagnostics[0].URL,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(analysis) error = %v", err)
	}
	if _, err := CollectBaselineFindingsFromAnalysisJSON(analysisData); err == nil || !strings.Contains(err.Error(), "always visible") {
		t.Fatalf("CollectBaselineFindingsFromAnalysisJSON() error = %v, want always-visible rejection", err)
	}
	if _, err := CountBaselineDiagnosticsFromAnalysisJSON(analysisData); err == nil || !strings.Contains(err.Error(), "always visible") {
		t.Fatalf("CountBaselineDiagnosticsFromAnalysisJSON() error = %v, want always-visible rejection", err)
	}
}
