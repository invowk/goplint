// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBaselineRejectsProtocolInconclusiveData(t *testing.T) {
	t.Parallel()

	categories := []string{
		CategoryUnvalidatedCastInconclusive,
		CategoryMissingConstructorValidateInc,
		CategoryUseBeforeValidateInconclusive,
	}
	for _, category := range categories {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			content := fmt.Sprintf("[%s]\nentries = [{ id = %q, message = %q }]\n", category, "gpl3_exact", "proof incomplete")
			path := writeTempFile(t, "baseline.toml", content)
			if _, err := loadBaseline(path, false); err == nil || !strings.Contains(err.Error(), category) {
				t.Fatalf("loadBaseline() error = %v, want rejection naming %q", err, category)
			}
		})
	}

	t.Run("outcome metadata", func(t *testing.T) {
		t.Parallel()

		path := writeTempFile(t, "baseline.toml", "[primitive]\nentries = [{ id = \"gpl3_exact\", message = \"x\", outcome = \"inconclusive\" }]\n")
		if _, err := loadBaseline(path, false); err == nil || !strings.Contains(err.Error(), "outcome") {
			t.Fatalf("loadBaseline() error = %v, want outcome metadata rejection", err)
		}
	})
}

func TestBaselineNeverMatchesProtocolInconclusiveCategories(t *testing.T) {
	t.Parallel()

	const findingID = "gpl3_exact"
	for _, category := range []string{
		CategoryUnvalidatedCastInconclusive,
		CategoryMissingConstructorValidateInc,
		CategoryUseBeforeValidateInconclusive,
	} {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			baseline := &BaselineConfig{lookupByID: map[string]map[string]bool{
				category: {findingID: true},
			}}
			if baseline.ContainsFinding(category, findingID, "proof incomplete") {
				t.Fatalf("ContainsFinding() matched always-visible category %q", category)
			}
		})
	}
}

func TestBaselineCollectionRejectsProtocolInconclusiveFindings(t *testing.T) {
	t.Parallel()

	for _, category := range []string{
		CategoryUnvalidatedCastInconclusive,
		CategoryMissingConstructorValidateInc,
		CategoryUseBeforeValidateInconclusive,
	} {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			stream := []byte(fmt.Sprintf("{\"category\":%q,\"id\":\"gpl3_exact\",\"message\":\"proof incomplete\",\"posn\":\"x.go:1:1\"}\n", category))
			if _, err := CollectBaselineFindingsFromStream(stream); err == nil || !strings.Contains(err.Error(), "always visible") {
				t.Fatalf("CollectBaselineFindingsFromStream() error = %v, want always-visible rejection", err)
			}

			analysis := []byte(fmt.Sprintf("{\"example.com/p\":{\"goplint\":[{\"posn\":\"x.go:1:1\",\"message\":\"proof incomplete\",\"category\":%q,\"url\":\"goplint://finding/gpl3_exact\"}]}}", category))
			if _, err := CollectBaselineFindingsFromAnalysisJSON(analysis); err == nil || !strings.Contains(err.Error(), "always visible") {
				t.Fatalf("CollectBaselineFindingsFromAnalysisJSON() error = %v, want always-visible rejection", err)
			}
		})
	}

	t.Run("inconclusive outcome on suppressible category", func(t *testing.T) {
		t.Parallel()

		record := FindingStreamRecord{
			Category: CategoryUnvalidatedBoundaryRequest,
			ID:       "gpl3_exact",
			Message:  "proof incomplete",
			Posn:     "x.go:1:1",
			Meta: map[string]string{
				"cfg_outcome_status": cfgRefinementStatusInconclusive,
			},
		}
		stream, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("json.Marshal(stream record): %v", err)
		}
		stream = append(stream, '\n')
		if _, err := CollectBaselineFindingsFromStream(stream); err == nil || !strings.Contains(err.Error(), "always visible") {
			t.Fatalf("CollectBaselineFindingsFromStream() error = %v, want outcome rejection", err)
		}
		if _, err := baselineStreamDiagnosticCounts(stream); err == nil || !strings.Contains(err.Error(), "always visible") {
			t.Fatalf("baselineStreamDiagnosticCounts() error = %v, want outcome rejection", err)
		}

		diagnostic := AnalysisDiagnostic{
			Posn:     "x.go:1:1",
			Message:  "proof incomplete",
			Category: CategoryUnvalidatedBoundaryRequest,
			URL: DiagnosticURLForFindingWithMeta("gpl3_exact", map[string]string{
				"cfg_outcome_status": cfgRefinementStatusInconclusive,
			}),
		}
		analysis, err := json.Marshal(AnalysisResult{
			"example.com/p": {"goplint": {diagnostic}},
		})
		if err != nil {
			t.Fatalf("json.Marshal(analysis result): %v", err)
		}
		if _, err := CollectBaselineFindingsFromAnalysisJSON(analysis); err == nil || !strings.Contains(err.Error(), "always visible") {
			t.Fatalf("CollectBaselineFindingsFromAnalysisJSON() error = %v, want outcome rejection", err)
		}
		if _, err := CountBaselineDiagnosticsFromAnalysisJSON(analysis); err == nil || !strings.Contains(err.Error(), "always visible") {
			t.Fatalf("CountBaselineDiagnosticsFromAnalysisJSON() error = %v, want outcome rejection", err)
		}
	})
}

func TestWriteBaselineRejectsProtocolInconclusiveFindings(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline.toml")
	err := WriteBaseline(path, map[string][]BaselineFinding{
		CategoryUnvalidatedCastInconclusive: {{ID: "gpl3_exact", Message: "proof incomplete"}},
	})
	if err == nil || !strings.Contains(err.Error(), "always visible") {
		t.Fatalf("WriteBaseline() error = %v, want always-visible rejection", err)
	}
}
