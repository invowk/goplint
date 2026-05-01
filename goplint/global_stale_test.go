// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestCollectGlobalStaleExceptionPatterns(t *testing.T) {
	t.Parallel()

	t.Run("deduplicates package coverage per pattern", func(t *testing.T) {
		t.Parallel()
		stream := append(
			makeGlobalStaleAnalysisJSON(t, AnalysisResult{
				"example.com/a": {
					"goplint": {
						{Category: CategoryStaleException, Message: `stale exception: pattern "dup.pattern" matched no diagnostics (reason: x)`},
						{Category: CategoryStaleException, Message: `stale exception: pattern "dup.pattern" matched no diagnostics (reason: x)`},
					},
				},
			}),
			makeGlobalStaleAnalysisJSON(t, AnalysisResult{
				"example.com/b": {
					"goplint": {},
				},
			})...,
		)

		patterns, totalPatterns, totalPackages, err := CollectGlobalStaleExceptionPatterns(stream)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if totalPackages != 2 {
			t.Fatalf("expected 2 packages, got %d", totalPackages)
		}
		if totalPatterns != 1 {
			t.Fatalf("expected 1 stale pattern, got %d", totalPatterns)
		}
		if len(patterns) != 0 {
			t.Fatalf("expected no globally stale patterns, got %v", patterns)
		}
	})

	t.Run("reports patterns stale in all packages", func(t *testing.T) {
		t.Parallel()
		stream := append(
			makeGlobalStaleAnalysisJSON(t, AnalysisResult{
				"example.com/a": {
					"goplint": {
						{Category: CategoryStaleException, Message: `stale exception: pattern "shared.pattern" matched no diagnostics (reason: x)`},
					},
				},
			}),
			makeGlobalStaleAnalysisJSON(t, AnalysisResult{
				"example.com/b": {
					"goplint": {
						{Category: CategoryStaleException, Message: `stale exception: pattern "shared.pattern" matched no diagnostics (reason: y)`},
					},
				},
			})...,
		)

		patterns, totalPatterns, totalPackages, err := CollectGlobalStaleExceptionPatterns(stream)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if totalPackages != 2 {
			t.Fatalf("expected 2 packages, got %d", totalPackages)
		}
		if totalPatterns != 1 {
			t.Fatalf("expected 1 stale pattern, got %d", totalPatterns)
		}
		if !slices.Equal(patterns, []string{"shared.pattern"}) {
			t.Fatalf("unexpected globally stale patterns: got %v", patterns)
		}
	})

	t.Run("no packages analyzed returns zero counts", func(t *testing.T) {
		t.Parallel()
		patterns, totalPatterns, totalPackages, err := CollectGlobalStaleExceptionPatterns(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(patterns) != 0 {
			t.Fatalf("expected no patterns, got %v", patterns)
		}
		if totalPatterns != 0 || totalPackages != 0 {
			t.Fatalf("expected zero counts, got patterns=%d packages=%d", totalPatterns, totalPackages)
		}
	})

	t.Run("whitespace-only stream returns zero counts", func(t *testing.T) {
		t.Parallel()
		patterns, totalPatterns, totalPackages, err := CollectGlobalStaleExceptionPatterns([]byte(" \n\t "))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(patterns) != 0 {
			t.Fatalf("expected no patterns, got %v", patterns)
		}
		if totalPatterns != 0 || totalPackages != 0 {
			t.Fatalf("expected zero counts, got patterns=%d packages=%d", totalPatterns, totalPackages)
		}
	})

	t.Run("malformed stream returns decode error", func(t *testing.T) {
		t.Parallel()
		_, _, _, err := CollectGlobalStaleExceptionPatterns([]byte("{invalid"))
		if err == nil {
			t.Fatal("expected decode error")
		}
	})
}

func TestStaleExceptionPatternFromDiagnostic(t *testing.T) {
	t.Parallel()

	t.Run("prefers URL metadata", func(t *testing.T) {
		t.Parallel()
		diag := AnalysisDiagnostic{
			Category: CategoryStaleException,
			Message:  `stale exception: pattern "wrong" matched no diagnostics (reason: test)`,
			URL: DiagnosticURLForFindingWithMeta(
				StableFindingID(CategoryStaleException, "pkg.Type.Field"),
				map[string]string{"pattern": "pkg.Type.Field"},
			),
		}
		got := StaleExceptionPatternFromDiagnostic(diag)
		if got != "pkg.Type.Field" {
			t.Fatalf("StaleExceptionPatternFromDiagnostic() = %q, want %q", got, "pkg.Type.Field")
		}
	})

	t.Run("falls back to message parsing", func(t *testing.T) {
		t.Parallel()
		diag := AnalysisDiagnostic{
			Category: CategoryStaleException,
			Message:  `stale exception: pattern "pkg.Type.Legacy" matched no diagnostics (reason: legacy)`,
		}
		got := StaleExceptionPatternFromDiagnostic(diag)
		if got != "pkg.Type.Legacy" {
			t.Fatalf("StaleExceptionPatternFromDiagnostic() = %q, want %q", got, "pkg.Type.Legacy")
		}
	})

	t.Run("invalid message returns empty pattern", func(t *testing.T) {
		t.Parallel()
		got := StaleExceptionPatternFromDiagnostic(AnalysisDiagnostic{Message: "something else"})
		if got != "" {
			t.Fatalf("StaleExceptionPatternFromDiagnostic() = %q, want empty", got)
		}
	})
}

func makeGlobalStaleAnalysisJSON(t *testing.T, result AnalysisResult) []byte {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshaling test JSON: %v", err)
	}
	return data
}
