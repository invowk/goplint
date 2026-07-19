// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCheckCastValidationCFA exercises the --check-cast-validation mode with
// canonical path analysis against the cfa_castvalidation fixture. This
// verifies path-reachability analysis catches cases the AST heuristic misses:
// dead-branch validation, use-before-validate, and conditional validation.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFA(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	// Canonical protocol analysis needs no backend flag.

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_castvalidation")
}

func TestCheckCastValidationCFAHonorsExcludedProcedurePaths(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", "excludedprotocol", "goplint.toml"))
	runAnalysisTest(t, testdata, h.Analyzer, "excludedprotocol")
}

func TestGenericConstraintCastCFA(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_generic_constraints")
}

func TestRecursiveSummaryCFA(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	results := runAnalysisTest(t, testdata, h.Analyzer, "cfa_recursive_summary")
	wantUnsafe := map[string]bool{
		"RecursivePreserveUnsafe":       false,
		"MutualRecursivePreserveUnsafe": false,
	}
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Category != CategoryUnvalidatedCast {
				continue
			}
			callChain := FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_witness_call_chain")
			for function := range wantUnsafe {
				if !strings.Contains(callChain, "."+function) {
					continue
				}
				summaries, summariesErr := strconv.Atoi(FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_tabulation_summaries"))
				reuses, reusesErr := strconv.Atoi(FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_tabulation_summary_reuses"))
				if summariesErr != nil || reusesErr != nil || summaries == 0 || reuses == 0 {
					t.Errorf(
						"%s tabulation evidence summaries=%d (%v) reuses=%d (%v), want finite non-zero summary reuse",
						function,
						summaries,
						summariesErr,
						reuses,
						reusesErr,
					)
				}
				wantUnsafe[function] = true
			}
		}
	}
	for function, found := range wantUnsafe {
		if !found {
			t.Errorf("recursive analyzer fixture %s produced no unsafe result with tabulation evidence", function)
		}
	}
}

func TestProtocolUnknownEffectsAfterValidation(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	_, analysisErrors, results := collectDiagnosticsForPackages(t, h.Analyzer, "protocol_unknown_effects")
	inconclusiveCount := 0
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			if !strings.Contains(diagnostic.Message, "has inconclusive Validate() path analysis") {
				continue
			}
			inconclusiveCount++
			callChain := FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_witness_call_chain")
			wantReason := pathOutcomeReasonUnresolvedTarget
			switch {
			case strings.HasSuffix(callChain, ".ConcurrentMutation"):
				wantReason = pathOutcomeReasonConcurrentMutation
			case strings.HasSuffix(callChain, ".EscapedHeapMutation"):
				wantReason = pathOutcomeReasonEscapedHeapMutation
			case strings.HasSuffix(callChain, ".ReflectionMutation"):
				wantReason = pathOutcomeReasonReflection
			case strings.HasSuffix(callChain, ".UnsafeAccess"):
				wantReason = pathOutcomeReasonUnsafe
			}
			if got := FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_inconclusive_reason"); got != string(wantReason) {
				t.Errorf("diagnostic %q reason = %q, want %q", diagnostic.Message, got, wantReason)
			}
		}
	}
	requireMutationGuardObservation(
		t,
		"post-validation-unknown/inconclusive-count",
		mutationGuardState("post-validation-unknown-effect-diagnostics", "9"),
		mutationGuardState("post-validation-unknown-effect-diagnostics", strconv.Itoa(inconclusiveCount)),
	)
	if len(analysisErrors) != 0 {
		t.Fatalf("post-validation unknown-effect analysistest errors: %v", analysisErrors)
	}
	if inconclusiveCount != 9 {
		t.Errorf("post-validation unknown-effect diagnostic count = %d, want 9", inconclusiveCount)
	}
}

func TestPostValidationEscapeProductionBoundaries(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	results := runAnalysisTest(t, analysistest.TestData(), h.Analyzer, "post_validation_escapes")
	want := map[string]int{
		"PointerChannel":           1,
		"AggregatePointer":         1,
		"IndirectPointer":          1,
		"PackagePointer":           1,
		"StoredClosure":            1,
		"DeferredClosure":          1,
		"HelperPointer":            1,
		"RecursivePointer":         1,
		"ProtectedUseBeforeEscape": 1,
	}
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Category != CategoryUnvalidatedCastInconclusive {
				continue
			}
			if got := FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_inconclusive_reason"); got != string(pathOutcomeReasonEscapedHeapMutation) {
				t.Errorf("post-validation escape reason = %q, want %q", got, pathOutcomeReasonEscapedHeapMutation)
			}
			callChain := FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_witness_call_chain")
			for function := range want {
				if strings.Contains(callChain, "."+function) {
					want[function]--
					break
				}
			}
		}
	}
	for function, remaining := range want {
		if remaining != 0 {
			t.Errorf("post-validation escape %s remaining diagnostics = %d, want 0", function, remaining)
		}
	}
}

func TestProtocolJoinRefinementCounterexamples(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	results := runAnalysisTest(t, analysistest.TestData(), h.Analyzer, "cfa_join_refinement")
	seen := map[string]bool{
		"JoinedInfeasibleValidation":    false,
		"MultipleIncomparableWitnesses": false,
		"CallerCalleeBlockCollision":    false,
	}
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			if !strings.Contains(diagnostic.Message, "type conversion to CommandName") {
				continue
			}
			callChain := FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_witness_call_chain")
			for function := range seen {
				if strings.Contains(callChain, "."+function) {
					seen[function] = true
				}
			}
			if !strings.Contains(callChain, ".MultipleIncomparableWitnesses") {
				continue
			}
			iterations := FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_refinement_iterations")
			if iterations == "" || iterations == "0" {
				t.Errorf("multiple-witness counterexample refinement iterations = %q, want at least one checked discharge", iterations)
			}
		}
	}
	for function, found := range seen {
		if !found {
			t.Errorf("%s counterexample produced no real-analyzer cast diagnostic", function)
		}
	}
}

func TestProtocolAliasCounterexamples(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	results := runAnalysisTest(t, analysistest.TestData(), h.Analyzer, "cfa_alias_counterexamples")
	wantAmbiguous := map[string]int{
		"AmbiguousPhi":                  1,
		"DynamicIndex":                  1,
		"AmbiguousPointer":              2,
		"AmbiguousInterface":            2,
		"ClosureLocalAmbiguity":         2,
		"AmbiguousStoredCopy":           2,
		"EscapedPointer":                1,
		"EscapedClosure":                1,
		"AmbiguousPostValidationEffect": 1,
	}
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			if !strings.Contains(diagnostic.Message, "type conversion to CommandName") ||
				!strings.Contains(diagnostic.Message, "inconclusive Validate() path analysis") {
				continue
			}
			if got := FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_inconclusive_reason"); got != string(pathOutcomeReasonAmbiguousIdentity) {
				t.Errorf("alias counterexample reason = %q, want %q", got, pathOutcomeReasonAmbiguousIdentity)
			}
			callChain := FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_witness_call_chain")
			matched := false
			for function := range wantAmbiguous {
				if strings.Contains(callChain, "."+function) {
					wantAmbiguous[function]--
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("unexpected ambiguous-alias witness call chain %q", callChain)
			}
		}
	}
	for function, remaining := range wantAmbiguous {
		if remaining != 0 {
			t.Errorf("ambiguous alias counterexample %s remaining diagnostics = %d, want 0", function, remaining)
		}
	}
}

// TestCheckCastValidationCFAClosure verifies that canonical cast validation
// analyzes closure bodies with independent CFGs and validation scopes,
// including nested closures.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFAClosure(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	runAnalysisTest(t, testdata, h.Analyzer, "cfa_closure")
}

// TestCheckCastValidationCFASelectorCanonicalization verifies selector target
// canonicalization. Equivalent forms like (*h).Name and h.Name
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

// TestCheckCastValidationCFAMethodValue verifies protocol analysis recognizes Validate()
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
// calls (g := f; g()) resolve back to the bound literal for protocol analysis.
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
// expressions do not count as guaranteed validation.
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

	runAnalysisTest(t, testdata, h.Analyzer, "use_before_validate_closure")
}

// TestCheckCastValidationCFAConditionalContexts exercises conditional-context
// handling in path-analysis helpers (range/type-switch and init statements).
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFAConditionalContexts(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_conditional_contexts")
}

// TestCheckCastValidationCFADetachedClosure verifies detached closure literals
// are analyzed as procedures even without a visible invocation.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFADetachedClosure(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_non_executable_closure")
}

// TestCheckCastValidationCFAExecutableClosureVar verifies that closures bound
// to local variables are analyzed both when invoked (f(), defer f(), go f())
// and when stored without a visible invocation.
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
// canonical cast validation.
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
