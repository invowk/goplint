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
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-cast-validation", "true")
	// CFA is default — no explicit flag needed.

	analysistest.Run(t, testdata, Analyzer, "cfa_castvalidation")
}

// TestCheckCastValidationCFAClosure exercises --check-cast-validation with
// CFA (enabled by default) against the cfa_closure fixture. This verifies
// that CFA mode analyzes closure bodies (which AST mode skips entirely)
// with independent CFGs and validation scopes, including nested closures.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidationCFAClosure(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-cast-validation", "true")
	// CFA is default — no explicit flag needed.

	analysistest.Run(t, testdata, Analyzer, "cfa_closure")
}

// TestCFAEnabledByDefault verifies that CFA is enabled by default when
// --check-cast-validation is active.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCFAEnabledByDefault(t *testing.T) {
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-all", "true")

	rc := newRunConfig()
	if rc.noCFA {
		t.Error("expected noCFA = false (CFA enabled) when --check-all is set")
	}
}
