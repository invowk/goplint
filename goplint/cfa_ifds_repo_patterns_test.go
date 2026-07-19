// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestCanonicalRepoPatternsStayClean(t *testing.T) {
	t.Parallel()

	castDiags := runRepoPatternDiagnostics(t, func(analyzer *analysis.Analyzer) {
		setFlag(t, analyzer, "check-cast-validation", "true")
	})
	assertNoCategory(t, castDiags, CategoryUnvalidatedCast)
	assertNoCategory(t, castDiags, CategoryUnvalidatedCastInconclusive)

	constructorDiags := runRepoPatternDiagnostics(t, func(analyzer *analysis.Analyzer) {
		setFlag(t, analyzer, "check-constructor-validates", "true")
	})
	assertNoCategory(t, constructorDiags, CategoryMissingConstructorValidate)
	assertNoCategory(t, constructorDiags, CategoryMissingConstructorValidateInc)
}

func runRepoPatternDiagnostics(
	t *testing.T,
	configure func(analyzer *analysis.Analyzer),
) []analysis.Diagnostic {
	t.Helper()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	configure(h.Analyzer)

	diagnostics, _, results := collectDiagnosticsForPackages(t, h.Analyzer, "cfa_ifds_repo_patterns")
	for _, result := range results {
		if result != nil && result.Err != nil {
			t.Fatalf("analysis result error: %v", result.Err)
		}
	}
	return diagnostics
}

func assertNoCategory(t *testing.T, diagnostics []analysis.Diagnostic, category string) {
	t.Helper()

	got := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Category == category {
			got++
		}
	}
	if got != 0 {
		t.Fatalf("expected no %s diagnostics, got %d", category, got)
	}
}
