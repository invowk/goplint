// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCheckCastValidationCFA exercises the --check-cast-validation mode with
// CFA (enabled by default) against the cfa_castvalidation fixture. This
// verifies path-reachability analysis catches cases the AST heuristic misses:
// dead-branch validation, use-before-validate, and conditional validation.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFA(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	// CFA is default — no explicit flag needed.

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_castvalidation")
}

// TestCheckCastValidationCFAClosure exercises --check-cast-validation with
// CFA (enabled by default) against the cfa_closure fixture. This verifies
// that CFA mode analyzes closure bodies (which AST mode skips entirely)
// with independent CFGs and validation scopes, including nested closures.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFAClosure(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	// CFA is default — no explicit flag needed.

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_closure")
}

// TestCheckCastValidationCFASelectorCanonicalization verifies selector target
// canonicalization in CFA mode. Equivalent forms like (*h).Name and h.Name
// should match and avoid false positives.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFASelectorCanonicalization(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "castvalidation_selector_canonicalization")
}

// TestCheckCastValidationCFASelectorShadowing verifies shadowed selector
// targets are tracked by object identity and do not get incorrectly
// suppressed by Validate() on outer variables.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFASelectorShadowing(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "castvalidation_selector_shadowing")
}

// TestCheckCastValidationCFAMethodValue verifies CFA recognizes Validate()
// invoked through bound method values (including aliased function variables).
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFAMethodValue(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "castvalidation_method_value")
}

// TestCheckCastValidationCFAClosureVarAlias verifies closure-variable alias
// calls (g := f; g()) resolve back to the bound literal for CFA analysis.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFAClosureVarAlias(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_closure_var_alias")
}

// TestCheckCastValidationCFAShortCircuit verifies that short-circuit boolean
// expressions do not count as guaranteed validation in CFA mode.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFAShortCircuit(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_short_circuit_validate")
}

// TestCheckUseBeforeValidateCFA exercises --check-use-before-validate mode
// against the use_before_validate fixture. Verifies that DDD Value Type
// variables used as function arguments or method receivers before Validate()
// in the same basic block are flagged, even when all paths to return
// eventually call Validate().
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckUseBeforeValidateCFA(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	setFlag(t, h.Analyzer, "check-use-before-validate", "true")
	setFlag(t, h.Analyzer, "ubv-mode", ubvModeOrder)

	runAnalysisTest(t, testdata, h.Analyzer, "use_before_validate")
}

// TestCheckUseBeforeValidateCrossCFA exercises --check-use-before-validate
// against the use_before_validate_cross fixture. Verifies that cross-block UBV detection
// correctly flags variables used in successor blocks before Validate() is
// called on any path from the cast, while same-block UBV takes priority.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckUseBeforeValidateCrossCFA(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	setFlag(t, h.Analyzer, "check-use-before-validate", "true")
	setFlag(t, h.Analyzer, "ubv-mode", ubvModeOrder)

	runAnalysisTest(t, testdata, h.Analyzer, "use_before_validate_cross")
}

// TestCheckUseBeforeValidateClosureCFA exercises UBV in synchronous closures
// (IIFEs and deferred closures). Verifies synchronous closure bodies are
// considered for use detection while goroutine closures remain excluded.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckUseBeforeValidateClosureCFA(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	setFlag(t, h.Analyzer, "check-use-before-validate", "true")
	setFlag(t, h.Analyzer, "ubv-mode", ubvModeOrder)

	runAnalysisTest(t, testdata, h.Analyzer, "use_before_validate_closure")
}

// TestCheckCastValidationCFAConditionalContexts exercises conditional-context
// handling in CFA helper logic (range/type-switch and init statements).
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFAConditionalContexts(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_conditional_contexts")
}

// TestCheckCastValidationCFANonExecutableClosure verifies CFA skips detached
// closure literals while still analyzing executable closures (IIFE/go/defer).
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFANonExecutableClosure(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_non_executable_closure")
}

// TestCheckCastValidationCFAExecutableClosureVar verifies that closures bound
// to local variables are analyzed when invoked (f(), defer f(), go f()) and
// remain ignored when only stored.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFAExecutableClosureVar(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_executable_closure_var")
}

// TestCheckCastValidationCFAClosureVarRebind verifies closure-variable calls
// resolve function literal bindings at each call site, even after rebinding.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFAClosureVarRebind(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_closure_var_rebind")
}

// TestCheckUseBeforeValidateClosureVarCall verifies UBV ordering for direct and
// deferred closure-variable calls in the enclosing function block.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckUseBeforeValidateClosureVarCall(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	setFlag(t, h.Analyzer, "check-use-before-validate", "true")
	setFlag(t, h.Analyzer, "ubv-mode", ubvModeOrder)

	runAnalysisTest(t, testdata, h.Analyzer, "use_before_validate_closure_var_call")
}

// TestCheckUseBeforeValidateMethodValue verifies same-block UBV ordering
// recognizes Validate() calls invoked via method values.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckUseBeforeValidateMethodValue(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	setFlag(t, h.Analyzer, "check-use-before-validate", "true")
	setFlag(t, h.Analyzer, "ubv-mode", ubvModeOrder)

	runAnalysisTest(t, testdata, h.Analyzer, "use_before_validate_method_value")
}

// TestCheckCastValidationCFANoReturnTerminator verifies non-return sinks like
// panic/os.Exit/log.Fatal do not create synthetic "unvalidated return" paths.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFANoReturnTerminator(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_no_return_terminator")
}

// TestCheckAllEnablesCFABackedCastValidation verifies --check-all activates
// cast validation, which is always CFA-backed.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckAllEnablesCFABackedCastValidation(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-all", "true")

	rc := newRunConfigForState(h.state)
	if !rc.checkCastValidation {
		t.Error("expected checkCastValidation = true when --check-all is set")
	}
}
