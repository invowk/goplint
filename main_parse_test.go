// SPDX-License-Identifier: MPL-2.0

package main

import (
	"testing"

	"github.com/invowk/invowk/tools/goplint/goplint"
)

func TestParseAnalysisJSON(t *testing.T) {
	t.Parallel()

	t.Run("single package with findings", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					suppressibleDiagnostic("primitive", "struct field pkg.Foo.Bar uses primitive type string"),
					suppressibleDiagnostic("missing-validate", "named type pkg.MyType has no Validate() method"),
				},
			},
		})

		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings["primitive"]) != 1 {
			t.Errorf("expected 1 primitive finding, got %d", len(findings["primitive"]))
		}
		if len(findings["missing-validate"]) != 1 {
			t.Errorf("expected 1 missing-validate finding, got %d", len(findings["missing-validate"]))
		}
	})

	t.Run("deduplicates across packages", func(t *testing.T) {
		t.Parallel()
		// Simulate the same diagnostic appearing in both the package and its test variant.
		diag := suppressibleDiagnostic("primitive", "struct field pkg.Foo.Bar uses primitive type string")
		pkg1 := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {"goplint": {diag}},
		})
		pkg2 := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg [example.com/pkg.test]": {"goplint": {diag}},
		})
		combined := append([]byte{}, pkg1...)
		combined = append(combined, pkg2...)

		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON(combined)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings["primitive"]) != 1 {
			t.Errorf("expected 1 deduplicated finding, got %d", len(findings["primitive"]))
		}
	})

	t.Run("uses finding ID from diagnostic URL", func(t *testing.T) {
		t.Parallel()
		const findingID = "gpl3_deadbeef"
		input := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{
						Category: "primitive",
						Message:  "struct field pkg.Foo.Bar uses primitive type string",
						URL:      goplint.DiagnosticURLForFinding(findingID),
					},
				},
			},
		})

		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings["primitive"]) != 1 {
			t.Fatalf("expected 1 primitive finding, got %d", len(findings["primitive"]))
		}
		if findings["primitive"][0].ID != findingID {
			t.Errorf("expected finding ID %q, got %q", findingID, findings["primitive"][0].ID)
		}
	})

	t.Run("handles CFA categories in baseline parsing", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{
						Category: goplint.CategoryUnvalidatedCast,
						Message:  "type conversion to pkg.CommandName from non-constant without Validate() check",
						URL:      goplint.DiagnosticURLForFinding("gpl3_cfa_unvalidated"),
					},
					{
						Category: goplint.CategoryUseBeforeValidateSameBlock,
						Message:  "variable x of type pkg.CommandName used before Validate() in same block",
						URL:      goplint.DiagnosticURLForFinding("gpl3_cfa_ubv"),
					},
				},
			},
		})

		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings[goplint.CategoryUnvalidatedCast]) != 1 {
			t.Fatalf("expected 1 %s finding, got %d", goplint.CategoryUnvalidatedCast, len(findings[goplint.CategoryUnvalidatedCast]))
		}
		if len(findings[goplint.CategoryUseBeforeValidateSameBlock]) != 1 {
			t.Fatalf("expected 1 %s finding, got %d", goplint.CategoryUseBeforeValidateSameBlock, len(findings[goplint.CategoryUseBeforeValidateSameBlock]))
		}
	})

	t.Run("missing suppressible finding URL falls back to deterministic ID", func(t *testing.T) {
		t.Parallel()
		const (
			category = "primitive"
			message  = "struct field pkg.Foo.Bar uses primitive type string"
		)
		input := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: category, Message: message},
				},
			},
		})

		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error for missing finding URL: %v", err)
		}
		got := findings[category]
		if len(got) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(got))
		}
		wantID := goplint.StableFindingID(category, "", message)
		if got[0].ID != wantID {
			t.Fatalf("expected fallback id %q, got %q", wantID, got[0].ID)
		}
	})

	t.Run("repeated messages with distinct finding IDs are preserved", func(t *testing.T) {
		t.Parallel()
		const (
			category = goplint.CategoryUnusedValidateResult
			message  = "Validate() result discarded — error return is unused"
		)
		input := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{
						Category: category,
						Posn:     "pkg/file.go:10:2",
						Message:  message,
						URL:      suppressibleDiagnosticWithSeed(category, message, "line-10").URL,
					},
					{
						Category: category,
						Posn:     "pkg/file.go:20:2",
						Message:  message,
						URL:      suppressibleDiagnosticWithSeed(category, message, "line-20").URL,
					},
				},
			},
		})

		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings[category]) != 2 {
			t.Fatalf("expected 2 %s findings, got %d", category, len(findings[category]))
		}
	})

	t.Run("filters out stale-exception diagnostics", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					suppressibleDiagnostic("primitive", "real finding"),
					{Category: goplint.CategoryStaleException, Message: "stale exception: pattern ..."},
				},
			},
		})

		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings["primitive"]) != 1 {
			t.Errorf("expected 1 primitive finding, got %d", len(findings["primitive"]))
		}
		if len(findings[goplint.CategoryStaleException]) != 0 {
			t.Errorf("expected 0 stale-exception findings, got %d", len(findings[goplint.CategoryStaleException]))
		}
	})

	t.Run("skips entries with empty category or message", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: "", Message: "orphaned message"},
					{Category: "primitive", Message: "", URL: suppressibleDiagnostic("primitive", "unused").URL},
					suppressibleDiagnostic("primitive", "valid finding"),
				},
			},
		})

		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings["primitive"]) != 1 {
			t.Errorf("expected 1 finding (empty category/message filtered), got %d", len(findings["primitive"]))
		}
	})

	t.Run("filters non-suppressible categories", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: goplint.CategoryUnknownDirective, Message: "unknown directive key"},
					suppressibleDiagnostic("primitive", "valid finding"),
				},
			},
		})

		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings[goplint.CategoryUnknownDirective]) != 0 {
			t.Errorf("expected unknown-directive to be excluded from baseline findings")
		}
		if len(findings["primitive"]) != 1 {
			t.Errorf("expected primitive finding to remain, got %d", len(findings["primitive"]))
		}
	})

	t.Run("unknown category returns error", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: "totally-unknown-category", Message: "unexpected"},
				},
			},
		})
		_, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err == nil {
			t.Fatal("expected error for unknown category")
		}
	})

	t.Run("empty input returns empty findings", func(t *testing.T) {
		t.Parallel()
		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON([]byte{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("expected 0 categories, got %d", len(findings))
		}
	})

	t.Run("whitespace-only input returns empty findings", func(t *testing.T) {
		t.Parallel()
		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON([]byte(" \n\t "))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("expected 0 categories, got %d", len(findings))
		}
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		t.Parallel()
		_, err := goplint.CollectBaselineFindingsFromAnalysisJSON([]byte("{invalid json"))
		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})

	t.Run("truncated object after valid stream returns error", func(t *testing.T) {
		t.Parallel()
		good := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					suppressibleDiagnostic("primitive", "valid finding"),
				},
			},
		})
		input := append(append([]byte{}, good...), []byte(`{"example.com/bad":`)...)
		_, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err == nil {
			t.Fatal("expected error for truncated JSON stream")
		}
	})

	t.Run("ignores non-goplint analyzer results", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]goplint.AnalysisDiagnostic{
			"example.com/pkg": {
				"otherana": {
					{Category: "other", Message: "not our concern"},
				},
				"goplint": {
					suppressibleDiagnostic("primitive", "our finding"),
				},
			},
		})

		findings, err := goplint.CollectBaselineFindingsFromAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings["other"]) != 0 {
			t.Errorf("expected 0 'other' findings, got %d", len(findings["other"]))
		}
		if len(findings["primitive"]) != 1 {
			t.Errorf("expected 1 primitive finding, got %d", len(findings["primitive"]))
		}
	})
}

func suppressibleDiagnostic(category, message string) goplint.AnalysisDiagnostic {
	return suppressibleDiagnosticWithSeed(category, message, message)
}

func suppressibleDiagnosticWithSeed(category, message, seed string) goplint.AnalysisDiagnostic {
	id := goplint.StableFindingID(category, "main_parse_test", seed)
	return goplint.AnalysisDiagnostic{
		Category: category,
		Message:  message,
		URL:      goplint.DiagnosticURLForFinding(id),
	}
}
