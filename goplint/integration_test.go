// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// setFlag sets an analyzer flag by name using the framework-standard
// Analyzer.Flags.Set() API. This is the same pattern used by x/tools
// tests (testinggoroutine, findcall).
func setFlag(t *testing.T, name, value string) {
	t.Helper()
	if err := Analyzer.Flags.Set(name, value); err != nil {
		t.Fatalf("failed to set flag %q to %q: %v", name, value, err)
	}
}

// resetFlags restores all analyzer flags to their default values.
// Called via t.Cleanup() to ensure clean state between tests.
func resetFlags(t *testing.T) {
	t.Helper()
	setFlag(t, "config", "")
	setFlag(t, "baseline", "")
	setFlag(t, "audit-exceptions", "false")
	setFlag(t, "check-all", "false")
	setFlag(t, "check-validate", "false")
	setFlag(t, "check-stringer", "false")
	setFlag(t, "check-constructors", "false")
	setFlag(t, "check-constructor-sig", "false")
	setFlag(t, "check-func-options", "false")
	setFlag(t, "check-immutability", "false")
	setFlag(t, "check-struct-validate", "false")
	setFlag(t, "check-cast-validation", "false")
	setFlag(t, "check-validate-usage", "false")
	setFlag(t, "check-constructor-error-usage", "false")
	setFlag(t, "check-constructor-validates", "false")
	setFlag(t, "check-validate-delegation", "false")
	setFlag(t, "check-nonzero", "false")
	setFlag(t, "no-cfa", "false")
	setFlag(t, "audit-review-dates", "false")
	setFlag(t, "check-enum-sync", "false")
	setFlag(t, "suggest-validate-all", "false")
}

// TestNewRunConfig verifies the --check-all expansion logic and the
// deliberate exclusion of --audit-exceptions.
//
// NOT parallel: shares Analyzer.Flags state.
func TestNewRunConfig(t *testing.T) {
	t.Cleanup(func() { resetFlags(t) })

	t.Run("check-all enables all supplementary modes", func(t *testing.T) {
		resetFlags(t)
		setFlag(t, "check-all", "true")

		rc := newRunConfig()

		if !rc.checkValidate {
			t.Error("expected checkValidate = true")
		}
		if !rc.checkStringer {
			t.Error("expected checkStringer = true")
		}
		if !rc.checkConstructors {
			t.Error("expected checkConstructors = true")
		}
		if !rc.checkConstructorSig {
			t.Error("expected checkConstructorSig = true")
		}
		if !rc.checkFuncOptions {
			t.Error("expected checkFuncOptions = true")
		}
		if !rc.checkImmutability {
			t.Error("expected checkImmutability = true")
		}
		if !rc.checkStructValidate {
			t.Error("expected checkStructValidate = true")
		}
		if !rc.checkCastValidation {
			t.Error("expected checkCastValidation = true")
		}
		if !rc.checkValidateUsage {
			t.Error("expected checkValidateUsage = true")
		}
		if !rc.checkConstructorErrUsage {
			t.Error("expected checkConstructorErrUsage = true")
		}
		if !rc.checkConstructorValidates {
			t.Error("expected checkConstructorValidates = true")
		}
		if !rc.checkValidateDelegation {
			t.Error("expected checkValidateDelegation = true")
		}
		if !rc.checkNonZero {
			t.Error("expected checkNonZero = true")
		}
	})

	t.Run("check-all does NOT enable audit-exceptions", func(t *testing.T) {
		resetFlags(t)
		setFlag(t, "check-all", "true")

		rc := newRunConfig()

		if rc.auditExceptions {
			t.Error("expected auditExceptions = false (--check-all should NOT enable it)")
		}
	})

	t.Run("check-all with explicit audit-exceptions preserves both", func(t *testing.T) {
		resetFlags(t)
		setFlag(t, "check-all", "true")
		setFlag(t, "audit-exceptions", "true")

		rc := newRunConfig()

		if !rc.auditExceptions {
			t.Error("expected auditExceptions = true (explicitly set)")
		}
		if !rc.checkValidate {
			t.Error("expected checkValidate = true (from check-all)")
		}
	})

	t.Run("individual flags work independently", func(t *testing.T) {
		resetFlags(t)
		setFlag(t, "check-validate", "true")

		rc := newRunConfig()

		if !rc.checkValidate {
			t.Error("expected checkValidate = true")
		}
		if rc.checkStringer {
			t.Error("expected checkStringer = false (not set)")
		}
		if rc.checkAll {
			t.Error("expected checkAll = false (not set)")
		}
	})
}

// TestAnalyzerWithConfig exercises the full analyzer pipeline with a
// TOML config file loaded, verifying that exception patterns and
// skip_types correctly suppress findings.
//
// NOT parallel: shares Analyzer.Flags state.
func TestAnalyzerWithConfig(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "config", filepath.Join(testdata, "src", "configexceptions", "goplint.toml"))

	analysistest.Run(t, testdata, Analyzer, "configexceptions")
}

// TestAnalyzerWithRealExceptionsToml loads the real project exceptions.toml
// and runs against the basic fixture to verify:
//  1. The real config file parses without error.
//  2. It doesn't accidentally suppress basic findings (the basic fixture
//     has no patterns matching the real exceptions).
//
// NOT parallel: shares Analyzer.Flags state.
func TestAnalyzerWithRealExceptionsToml(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "config", filepath.Join(testdata, "..", "..", "exceptions.toml"))

	analysistest.Run(t, testdata, Analyzer, "basic")
}

// TestCheckValidate exercises the --check-validate mode against the validate
// fixture, verifying named types without Validate() are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckValidate(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-validate", "true")

	analysistest.Run(t, testdata, Analyzer, "validate")
}

// TestCheckStringer exercises the --check-stringer mode against the stringer
// fixture, verifying named types without String() are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckStringer(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-stringer", "true")

	analysistest.Run(t, testdata, Analyzer, "stringer")
}

// TestCheckConstructors exercises the --check-constructors mode against the
// constructors fixture, verifying exported structs without NewXxx() are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckConstructors(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-constructors", "true")

	analysistest.Run(t, testdata, Analyzer, "constructors")
}

// TestAuditExceptions verifies that --audit-exceptions reports stale
// exception patterns that matched no diagnostics within a single package.
//
// NOT parallel: shares Analyzer.Flags state.
func TestAuditExceptions(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "config", filepath.Join(testdata, "src", "auditexceptions", "goplint.toml"))
	setFlag(t, "audit-exceptions", "true")

	analysistest.Run(t, testdata, Analyzer, "auditexceptions")
}

// TestAuditExceptionsMultiPackage verifies per-package stale detection across
// two packages sharing the same exception config. An exception matching in
// package A is reported as stale in package B. This documents the per-package
// limitation of go/analysis.
//
// NOT parallel: shares Analyzer.Flags state.
func TestAuditExceptionsMultiPackage(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "config", filepath.Join(testdata, "src", "auditexceptions_pkga", "goplint.toml"))
	setFlag(t, "audit-exceptions", "true")

	analysistest.Run(t, testdata, Analyzer,
		"auditexceptions_pkga", "auditexceptions_pkgb")
}

// TestCheckAll exercises the --check-all flag, confirming it enables all
// 14 DDD compliance checks in a single run: primitive, validate, stringer,
// constructors, constructor-sig, func-options, immutability, struct-validate,
// cast-validation, validate-usage, constructor-error-usage,
// constructor-validates, validate-delegation, and nonzero. Also explicitly
// enables --audit-exceptions to verify all diagnostic categories fire together.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckAll(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-all", "true")
	setFlag(t, "audit-exceptions", "true")
	setFlag(t, "config", filepath.Join(testdata, "src", "checkall", "goplint.toml"))

	analysistest.Run(t, testdata, Analyzer, "checkall")
}

// TestCheckConstructorSig exercises the --check-constructor-sig mode against
// the constructorsig fixture, verifying constructors with wrong return types
// are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckConstructorSig(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-constructor-sig", "true")

	analysistest.Run(t, testdata, Analyzer, "constructorsig")
}

// TestCheckFuncOptions exercises the --check-func-options mode against
// the funcoptions fixture, verifying both detection (too many params) and
// completeness (missing WithXxx, missing variadic) are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckFuncOptions(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-func-options", "true")

	analysistest.Run(t, testdata, Analyzer, "funcoptions")
}

// TestGenericsStructural exercises the structural modes (constructor-sig,
// immutability) against generic types to verify type parameter handling.
//
// NOT parallel: shares Analyzer.Flags state.
func TestGenericsStructural(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-constructor-sig", "true")
	setFlag(t, "check-immutability", "true")

	analysistest.Run(t, testdata, Analyzer, "generics_structural")
}

// TestCheckImmutability exercises the --check-immutability mode against
// the immutability fixture, verifying exported fields on structs with
// constructors are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckImmutability(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-immutability", "true")

	analysistest.Run(t, testdata, Analyzer, "immutability")
}

// TestCheckStructValidate exercises the --check-struct-validate mode against
// the structvalidate fixture, verifying exported structs with constructors
// but missing Validate() are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckStructValidate(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-struct-validate", "true")

	analysistest.Run(t, testdata, Analyzer, "structvalidate")
}

// TestCheckCastValidation exercises the --check-cast-validation mode with
// --no-cfa against the castvalidation fixture, verifying type conversions
// from raw primitives to DDD Value Types without Validate() are flagged
// using the AST name-based heuristic.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckCastValidation(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	resetFlags(t)
	setFlag(t, "check-cast-validation", "true")
	setFlag(t, "no-cfa", "true")

	analysistest.Run(t, testdata, Analyzer, "castvalidation")
}

// TestBaselineSuppression verifies that the --baseline flag correctly
// suppresses findings present in the baseline while reporting new ones.
// The baseline fixture has two struct fields and two function params: two
// are in the baseline (suppressed) and two are not (reported).
//
// NOT parallel: shares Analyzer.Flags state.
func TestBaselineSuppression(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "baseline", filepath.Join(testdata, "src", "baseline", "goplint-baseline.toml"))

	analysistest.Run(t, testdata, Analyzer, "baseline")
}

// TestBaselineSupplementaryCategories verifies that baseline suppression
// works for supplementary modes (missing-validate, missing-stringer,
// missing-constructor). Some findings are baselined and suppressed; others
// are new and reported.
//
// NOT parallel: shares Analyzer.Flags state.
func TestBaselineSupplementaryCategories(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-validate", "true")
	setFlag(t, "check-stringer", "true")
	setFlag(t, "check-constructors", "true")
	setFlag(t, "baseline", filepath.Join(testdata, "src", "baseline_supplementary", "goplint-baseline.toml"))

	analysistest.Run(t, testdata, Analyzer, "baseline_supplementary")
}

// TestCheckValidateUsage exercises the --check-validate-usage mode against
// the validateusage fixture, verifying that discarded Validate() results
// are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckValidateUsage(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	resetFlags(t)
	setFlag(t, "check-validate-usage", "true")

	analysistest.Run(t, testdata, Analyzer, "validateusage")
}

// TestCheckConstructorErrorUsage exercises the --check-constructor-error-usage
// mode against the constructorusage fixture, verifying that constructor calls
// with error returns assigned to blank identifiers are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckConstructorErrorUsage(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	resetFlags(t)
	setFlag(t, "check-constructor-error-usage", "true")

	analysistest.Run(t, testdata, Analyzer, "constructorusage")
}

// TestCheckValidateDelegation exercises the --check-validate-delegation mode
// against the validatedelegation fixture, verifying that structs with
// //goplint:validate-all are checked for complete field delegation.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckValidateDelegation(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	resetFlags(t)
	setFlag(t, "check-validate-delegation", "true")

	analysistest.Run(t, testdata, Analyzer, "validatedelegation")
}

// TestCheckValidateDelegationMultiFile exercises --check-validate-delegation
// with a multi-file package where the struct is defined in one file and its
// Validate() method in another. Verifies cross-file delegation detection.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckValidateDelegationMultiFile(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	resetFlags(t)
	setFlag(t, "check-validate-delegation", "true")

	analysistest.Run(t, testdata, Analyzer, "validatedelegation_multifile")
}

// TestCheckConstructorValidates exercises the --check-constructor-validates
// mode against the constructorvalidates fixture, verifying that constructors
// returning types with Validate() but not calling it are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckConstructorValidates(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	resetFlags(t)
	setFlag(t, "check-constructor-validates", "true")

	analysistest.Run(t, testdata, Analyzer, "constructorvalidates")
}

// TestConstructorValidatesException exercises that --check-constructor-validates
// respects TOML exception patterns (e.g., "pkg.NewFoo.constructor-validate").
// The configexceptions fixture has NewServiceConfig which does not call
// Validate() but is excepted via TOML — no constructor-validates finding.
//
// NOT parallel: shares Analyzer.Flags state.
func TestConstructorValidatesException(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "config", filepath.Join(testdata, "src", "configexceptions", "goplint.toml"))
	setFlag(t, "check-constructor-validates", "true")

	analysistest.Run(t, testdata, Analyzer, "configexceptions")
}

// TestSuggestValidateAll exercises the --suggest-validate-all advisory mode
// against the suggestvalidateall fixture, verifying that structs with Validate()
// and validatable fields but no //goplint:validate-all directive are reported.
//
// NOT parallel: shares Analyzer.Flags state.
func TestSuggestValidateAll(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	resetFlags(t)
	setFlag(t, "suggest-validate-all", "true")

	analysistest.Run(t, testdata, Analyzer, "suggestvalidateall")
}

// TestCheckNonZero exercises the --check-nonzero mode against the nonzero
// fixture (same-package) and nonzero_consumer fixture (cross-package fact
// propagation), verifying that struct fields using nonzero-annotated types
// as value (non-pointer) fields are flagged both within and across packages.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckNonZero(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	resetFlags(t)
	setFlag(t, "check-nonzero", "true")

	analysistest.Run(t, testdata, Analyzer, "nonzero", "nonzero_consumer")
}

// TestAuditReviewDates exercises the --audit-review-dates mode against
// a dedicated fixture with overdue, future, invalid, and blocked_by entries.
// Verifies that:
//   - Past review_after dates produce overdue-review diagnostics
//   - Future review_after dates do NOT produce diagnostics
//   - Invalid review_after dates produce invalid-date diagnostics
//   - blocked_by text is included in the diagnostic message
//   - Entries without review_after are ignored
//
// NOT parallel: shares Analyzer.Flags state.
func TestAuditReviewDates(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "config", filepath.Join(testdata, "src", "auditreviewdates", "config.toml"))
	setFlag(t, "audit-review-dates", "true")

	analysistest.Run(t, testdata, Analyzer, "auditreviewdates")
}

// TestCheckEnumSync exercises the --check-enum-sync mode against the
// enumsync fixture, verifying that CUE disjunction members missing from
// Go Validate() switch cases and extra Go cases not in CUE are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckEnumSync(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	resetFlags(t)
	setFlag(t, "check-enum-sync", "true")

	analysistest.Run(t, testdata, Analyzer, "enumsync")
}

func TestCheckEnumSyncNoSchema(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	resetFlags(t)
	setFlag(t, "check-enum-sync", "true")

	analysistest.Run(t, testdata, Analyzer, "enumsync_noschema")
}
