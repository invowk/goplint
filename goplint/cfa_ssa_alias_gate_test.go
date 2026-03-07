// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestPhaseDAliasGate(t *testing.T) {
	t.Parallel()

	t.Run("alias mode discharges nested and closure casts", func(t *testing.T) {
		t.Parallel()

		off := runPhaseDAliasFixture(t, cfgAliasModeOff, false)
		on := runPhaseDAliasFixture(t, cfgAliasModeSSA, false)

		assertSymbolCategoryCount(t, off.bySymbol, "NestedAliasValidated", CategoryUnvalidatedCast, 1)
		assertSymbolCategoryCount(t, on.bySymbol, "NestedAliasValidated", CategoryUnvalidatedCast, 0)
		assertSymbolCategoryCount(t, off.bySymbol, "ClosureAliasValidated", CategoryUnvalidatedCast, 1)
		assertSymbolCategoryCount(t, on.bySymbol, "ClosureAliasValidated", CategoryUnvalidatedCast, 0)
	})

	t.Run("alias mode preserves ubv behavior while fixing cast precision", func(t *testing.T) {
		t.Parallel()

		off := runPhaseDAliasFixture(t, cfgAliasModeOff, true)
		on := runPhaseDAliasFixture(t, cfgAliasModeSSA, true)

		assertSymbolCategoryCount(t, off.bySymbol, "AliasValidateSuppressesCast", CategoryUnvalidatedCast, 1)
		assertSymbolCategoryCount(t, on.bySymbol, "AliasValidateSuppressesCast", CategoryUnvalidatedCast, 0)
		assertSymbolCategoryCount(t, off.bySymbol, "AliasValidateSuppressesCast", CategoryUseBeforeValidateSameBlock, 0)
		assertSymbolCategoryCount(t, on.bySymbol, "AliasValidateSuppressesCast", CategoryUseBeforeValidateSameBlock, 0)

		assertSymbolCategoryCount(t, off.bySymbol, "AliasUseBeforeValidate", CategoryUseBeforeValidateSameBlock, 1)
		assertSymbolCategoryCount(t, on.bySymbol, "AliasUseBeforeValidate", CategoryUseBeforeValidateSameBlock, 1)
		assertSymbolCategoryCount(t, off.bySymbol, "AliasUseBeforeValidate", CategoryUnvalidatedCast, 0)
		assertSymbolCategoryCount(t, on.bySymbol, "AliasUseBeforeValidate", CategoryUnvalidatedCast, 0)
	})
}

type phaseDAliasFixtureResult struct {
	bySymbol map[string]map[string]int
}

func runPhaseDAliasFixture(
	t *testing.T,
	aliasMode string,
	checkUBV bool,
) phaseDAliasFixtureResult {
	t.Helper()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	if checkUBV {
		setFlag(t, h.Analyzer, "check-use-before-validate", "true")
	}
	setFlag(t, h.Analyzer, "cfg-interproc-engine", cfgInterprocEngineLegacy)
	setFlag(t, h.Analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineOff)
	setFlag(t, h.Analyzer, "cfg-refinement-mode", cfgRefinementModeOff)
	setFlag(t, h.Analyzer, "cfg-alias-mode", aliasMode)

	_, _, results := collectDiagnosticsForPackagesRespectCurrentEngine(t, h.Analyzer, "cfa_ssa_alias_gate")
	for _, result := range results {
		if result != nil && result.Err != nil {
			t.Fatalf("analysis result error: %v", result.Err)
		}
	}

	return phaseDAliasFixtureResult{
		bySymbol: collectDiagnosticCountsBySymbol(results),
	}
}

func collectDiagnosticCountsBySymbol(results []*analysistest.Result) map[string]map[string]int {
	out := make(map[string]map[string]int)
	for _, result := range results {
		if result == nil || result.Pass == nil {
			continue
		}
		spansByFile := collectFunctionSpans(result.Pass)
		for _, diag := range result.Diagnostics {
			symbol, ok := symbolNameForDiagnostic(result.Pass.Fset, spansByFile, diag.Pos)
			if !ok {
				continue
			}
			if out[symbol] == nil {
				out[symbol] = make(map[string]int)
			}
			out[symbol][diag.Category]++
		}
	}
	return out
}

func assertSymbolCategoryCount(
	t *testing.T,
	bySymbol map[string]map[string]int,
	symbol string,
	category string,
	want int,
) {
	t.Helper()

	got := 0
	if byCategory := bySymbol[symbol]; byCategory != nil {
		got = byCategory[category]
	}
	if got != want {
		t.Fatalf("symbol=%s category=%s count=%d, want %d", symbol, category, got, want)
	}
}
