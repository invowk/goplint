// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestIFDSRepoPatternsStayCleanWithoutPhaseC(t *testing.T) {
	t.Parallel()

	for _, engine := range []string{cfgInterprocEngineLegacy, cfgInterprocEngineIFDS} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()

			castDiags := runRepoPatternDiagnostics(t, engine, func(analyzer *analysis.Analyzer) {
				setFlag(t, analyzer, "check-cast-validation", "true")
			})
			assertNoCategory(t, castDiags, CategoryUnvalidatedCast)
			assertNoCategory(t, castDiags, CategoryUnvalidatedCastInconclusive)

			constructorDiags := runRepoPatternDiagnostics(t, engine, func(analyzer *analysis.Analyzer) {
				setFlag(t, analyzer, "check-constructor-validates", "true")
			})
			assertNoCategory(t, constructorDiags, CategoryMissingConstructorValidate)
			assertNoCategory(t, constructorDiags, CategoryMissingConstructorValidateInc)
		})
	}
}

func runRepoPatternDiagnostics(
	t *testing.T,
	engine string,
	configure func(analyzer *analysis.Analyzer),
) []analysis.Diagnostic {
	t.Helper()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "cfg-interproc-engine", engine)
	setFlag(t, h.Analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineOff)
	setFlag(t, h.Analyzer, "cfg-refinement-mode", cfgRefinementModeOff)
	setFlag(t, h.Analyzer, "cfg-alias-mode", cfgAliasModeOff)
	configure(h.Analyzer)

	diagnostics, _, results := collectDiagnosticsForPackagesRespectCurrentEngine(t, h.Analyzer, "cfa_ifds_repo_patterns")
	for _, result := range results {
		if result != nil && result.Err != nil {
			t.Fatalf("analysis result error: %v", result.Err)
		}
	}
	return diagnostics
}

func assertNoCategory(t *testing.T, diagnostics []analysis.Diagnostic, category string) {
	t.Helper()

	if got := countDiagnosticCategory(diagnostics, category); got != 0 {
		t.Fatalf("expected no %s diagnostics, got %d", category, got)
	}
}
