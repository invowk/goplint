// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"slices"
	"strings"
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
						staleExceptionDiagnostic("dup.pattern", "x"),
						staleExceptionDiagnostic("dup.pattern", "x"),
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
						staleExceptionDiagnostic("shared.pattern", "x"),
					},
				},
			}),
			makeGlobalStaleAnalysisJSON(t, AnalysisResult{
				"example.com/b": {
					"goplint": {
						staleExceptionDiagnostic("shared.pattern", "y"),
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

	t.Run("missing pattern metadata fails closed", func(t *testing.T) {
		t.Parallel()
		stream := makeGlobalStaleAnalysisJSON(t, AnalysisResult{
			"example.com/a": {
				"goplint": {{Category: CategoryStaleException, Message: `stale exception: pattern "legacy" matched no diagnostics`}},
			},
		})
		_, _, _, err := CollectGlobalStaleExceptionPatterns(stream)
		if err == nil {
			t.Fatal("expected missing metadata error")
		}
	})
}

func TestCollectGlobalStaleExceptionPatternsFromStreams(t *testing.T) {
	t.Parallel()

	analysis := append(
		makeGlobalStaleAnalysisJSON(t, AnalysisResult{
			"example.com/a": {"goplint": {{
				Category: CategoryStaleException,
				Posn:     "a.go:1:1",
				Message:  `stale exception: pattern "shared" matched no diagnostics`,
			}}},
		}),
		makeGlobalStaleAnalysisJSON(t, AnalysisResult{
			"example.com/b": {"goplint": {{
				Category: CategoryStaleException,
				Posn:     "b.go:1:1",
				Message:  `stale exception: pattern "shared" matched no diagnostics`,
			}}},
		})...,
	)
	findings := []byte(strings.Join([]string{
		`{"package":"example.com/a","category":"stale-exception","id":"id-a","message":"stale exception: pattern \"shared\" matched no diagnostics","posn":"a.go:1:1","meta":{"pattern":"shared"}}`,
		`{"package":"example.com/b","category":"stale-exception","id":"id-b","message":"stale exception: pattern \"shared\" matched no diagnostics","posn":"b.go:1:1","meta":{"pattern":"shared"}}`,
		"",
	}, "\n"))

	patterns, totalPatterns, totalPackages, err := CollectGlobalStaleExceptionPatternsFromStreams(analysis, findings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(patterns, []string{"shared"}) || totalPatterns != 1 || totalPackages != 2 {
		t.Fatalf("unexpected aggregation: patterns=%v totalPatterns=%d totalPackages=%d", patterns, totalPatterns, totalPackages)
	}

	_, _, _, err = CollectGlobalStaleExceptionPatternsFromStreams(analysis, nil)
	if err == nil {
		t.Fatal("expected missing findings stream coverage error")
	}
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

	t.Run("does not parse compatibility message", func(t *testing.T) {
		t.Parallel()
		diag := AnalysisDiagnostic{
			Category: CategoryStaleException,
			Message:  `stale exception: pattern "pkg.Type.Legacy" matched no diagnostics (reason: legacy)`,
		}
		got := StaleExceptionPatternFromDiagnostic(diag)
		if got != "" {
			t.Fatalf("StaleExceptionPatternFromDiagnostic() = %q, want empty", got)
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

func staleExceptionDiagnostic(pattern, reason string) AnalysisDiagnostic {
	return AnalysisDiagnostic{
		Category: CategoryStaleException,
		Message:  `stale exception: pattern "` + pattern + `" matched no diagnostics (reason: ` + reason + `)`,
		URL: DiagnosticURLForFindingWithMeta(
			StableFindingID(CategoryStaleException, pattern),
			map[string]string{"pattern": pattern},
		),
	}
}

func makeGlobalStaleAnalysisJSON(t *testing.T, result AnalysisResult) []byte {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshaling test JSON: %v", err)
	}
	return data
}
