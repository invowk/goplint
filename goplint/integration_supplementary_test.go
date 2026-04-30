// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCheckValidate exercises the --check-validate mode against the validate
// fixture, verifying named types without Validate() are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckValidate(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-validate", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "validate")
}

// TestCheckStringer exercises the --check-stringer mode against the stringer
// fixture, verifying named types without String() are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckStringer(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-stringer", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "stringer")
}

// TestCheckConstructors exercises the --check-constructors mode against the
// constructors fixture, verifying exported structs without NewXxx() are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckConstructors(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-constructors", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "constructors")
}

// TestAuditExceptions verifies that --audit-exceptions reports stale
// exception patterns that matched no diagnostics within a single package.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestAuditExceptions(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", "auditexceptions", "goplint.toml"))
	setFlag(t, h.Analyzer, "audit-exceptions", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "auditexceptions")
}

// TestAuditExceptionsMultiPackage verifies per-package stale detection across
// two packages sharing the same exception config. An exception matching in
// package A is reported as stale in package B. This documents the per-package
// limitation of go/analysis.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestAuditExceptionsMultiPackage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", "auditexceptions_pkga", "goplint.toml"))
	setFlag(t, h.Analyzer, "audit-exceptions", "true")

	runAnalysisTest(t, testdata, h.Analyzer,
		"auditexceptions_pkga", "auditexceptions_pkgb")
}

// TestCheckAll exercises the --check-all flag, confirming it enables all
// DDD compliance checks in a single run: primitive, validate, stringer,
// constructors, constructor-sig, func-options, immutability, struct-validate,
// cast-validation, validate-usage, constructor-error-usage,
// constructor-validates, validate-delegation, missing-struct-validate-fields,
// nonzero, use-before-validate, constructor-return-error, and
// redundant-conversion. Also explicitly enables --audit-exceptions to verify
// all diagnostic categories fire together.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckAll(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-all", "true")
	setFlag(t, h.Analyzer, "audit-exceptions", "true")
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", "checkall", "goplint.toml"))

	runAnalysisTest(t, testdata, h.Analyzer, "checkall")
}

// TestCheckConstructorSig exercises the --check-constructor-sig mode against
// the constructorsig fixture, verifying constructors with wrong return types
// are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckConstructorSig(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-constructor-sig", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "constructorsig")
}

// TestCheckFuncOptions exercises the --check-func-options mode against
// the funcoptions fixture, verifying both detection (too many params) and
// completeness (missing WithXxx, missing variadic) are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckFuncOptions(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-func-options", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "funcoptions")
}

// TestGenericsStructural exercises the structural modes (constructor-sig,
// immutability) against generic types to verify type parameter handling.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestGenericsStructural(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-constructor-sig", "true")
	setFlag(t, h.Analyzer, "check-immutability", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "generics_structural")
}

// TestCheckImmutability exercises the --check-immutability mode against
// the immutability fixture, verifying exported fields on structs with
// constructors are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckImmutability(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-immutability", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "immutability")
}

// TestCheckStructValidate exercises the --check-struct-validate mode against
// the structvalidate fixture, verifying exported structs with constructors
// but missing Validate() are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckStructValidate(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-struct-validate", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "structvalidate")
}

// TestCheckCastValidation exercises the --check-cast-validation mode with
// CFA (default) against the castvalidation fixture, verifying type conversions
// from raw primitives to DDD Value Types without Validate() are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckCastValidation(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-cast-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "castvalidation")
}

// TestBaselineSuppression verifies that the --baseline flag correctly
// suppresses findings present in the baseline while reporting new ones.
// The baseline fixture has two struct fields and two function params: two
// are in the baseline (suppressed) and two are not (reported).
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestBaselineSuppression(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "baseline", filepath.Join(testdata, "src", "baseline", "goplint-baseline.toml"))

	runAnalysisTest(t, testdata, h.Analyzer, "baseline")
}

// TestBaselineSupplementaryCategories verifies that baseline suppression
// works for supplementary modes (missing-validate, missing-stringer,
// missing-constructor). Some findings are baselined and suppressed; others
// are new and reported.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestBaselineSupplementaryCategories(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-validate", "true")
	setFlag(t, h.Analyzer, "check-stringer", "true")
	setFlag(t, h.Analyzer, "check-constructors", "true")
	setFlag(t, h.Analyzer, "baseline", filepath.Join(testdata, "src", "baseline_supplementary", "goplint-baseline.toml"))

	runAnalysisTest(t, testdata, h.Analyzer, "baseline_supplementary")
}

// TestCheckValidateUsage exercises the --check-validate-usage mode against
// the validateusage fixture, verifying that discarded Validate() results
// are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckValidateUsage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-validate-usage", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "validateusage")
}

// TestCheckBoundaryRequestValidation exercises
// --check-boundary-request-validation against exported Request/Options
// boundaries, including default-then-validate prologues and trusted-boundary
// exceptions.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckBoundaryRequestValidation(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-boundary-request-validation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "boundaryrequest")
}

// TestCheckConstructorErrorUsage exercises the --check-constructor-error-usage
// mode against the constructorusage fixture, verifying that constructor calls
// with error returns assigned to blank identifiers are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckConstructorErrorUsage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-error-usage", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "constructorusage")
}

// TestCheckValidateDelegation exercises the --check-validate-delegation mode
// against the validatedelegation fixture, verifying that structs with
// //goplint:validate-all are checked for complete field delegation.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckValidateDelegation(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-validate-delegation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "validatedelegation")
}

// TestCheckValidateDelegationMultiFile exercises --check-validate-delegation
// with a multi-file package where the struct is defined in one file and its
// Validate() method in another. Verifies cross-file delegation detection.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckValidateDelegationMultiFile(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-validate-delegation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "validatedelegation_multifile")
}

// TestCheckValidateDelegationVarAlias verifies delegation detection recognizes
// var aliasing patterns: var x = receiver.Field; x.Validate().
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckValidateDelegationVarAlias(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-validate-delegation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "validatedelegation_var_alias")
}

// TestCheckConstructorValidates exercises the --check-constructor-validates
// mode against the constructorvalidates fixture, verifying that constructors
// returning types with Validate() but not calling it are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckConstructorValidates(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "constructorvalidates")
}

// TestCheckConstructorValidatesInconclusiveStateBudget verifies constructor
// CFA reports an inconclusive category when exploration truncates via
// cfg-max-states.
func TestCheckConstructorValidatesInconclusiveStateBudget(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")
	setFlag(t, h.Analyzer, "cfg-max-states", "1")

	runAnalysisTest(t, testdata, h.Analyzer, "constructorvalidates_inconclusive")
}

// TestCheckConstructorValidatesMethodValue verifies constructor-validates
// recognizes Validate() calls made through method values.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckConstructorValidatesMethodValue(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "constructorvalidates_method_value")
}

// TestCheckConstructorValidatesCFABackendASTConservative verifies the AST
// backend treats calls conservatively and can report missing Validate() on
// no-return paths.
func TestCheckConstructorValidatesCFABackendASTConservative(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")
	setFlag(t, h.Analyzer, "cfg-backend", cfgBackendAST)

	runAnalysisTest(t, testdata, h.Analyzer, "constructor_backend_ast")
}

// TestCheckConstructorValidatesCFABackendSSAHandlesNoReturn verifies the SSA
// backend prunes no-return paths and avoids false positives for constructors
// that cannot reach a return.
func TestCheckConstructorValidatesCFABackendSSAHandlesNoReturn(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")
	setFlag(t, h.Analyzer, "cfg-backend", cfgBackendSSA)

	runAnalysisTest(t, testdata, h.Analyzer, "constructor_backend_ssa")
}

// TestCheckConstructorReturnError exercises the --check-constructor-return-error
// mode against the constructorreturn fixture. Verifies that constructors for
// types with Validate() that do not return error are flagged, while constructors
// that return error, return interfaces, or construct constant-only types are safe.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckConstructorReturnError(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-return-error", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "constructorreturn")
}

// TestCheckConstructorReturnErrorAlias verifies type aliases to the built-in
// error interface satisfy constructor-return-error requirements.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckConstructorReturnErrorAlias(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-return-error", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "constructorreturn_error_alias")
}

// TestConstructorValidatesCrossPackage exercises --check-constructor-validates
// with a cross-package helper annotated with //goplint:validates-type=TypeName.
// Verifies that the ValidatesTypeFact is exported from the helper package and
// imported correctly in the consuming package. Also enables
// --check-constructor-return-error since the shared fixture has expectations
// for both checks.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestConstructorValidatesCrossPackage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")
	setFlag(t, h.Analyzer, "check-constructor-return-error", "true")

	runAnalysisTest(t, testdata, h.Analyzer,
		"constructorvalidates_cross/util",
		"constructorvalidates_cross/myapp")
}

// TestConstructorValidatesCrossPackageThirdPartyType verifies validates-type
// facts carry the validated type identity (package path + type name), not the
// helper package path, when the helper validates a third package's type.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestConstructorValidatesCrossPackageThirdPartyType(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")

	runAnalysisTest(t, testdata, h.Analyzer,
		"constructorvalidates_cross_third/model",
		"constructorvalidates_cross_third/util",
		"constructorvalidates_cross_third/app")
}

// TestConstructorValidatesPackageCollision verifies constructor-validates uses
// full type identity (package path + type name), not just type name.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestConstructorValidatesPackageCollision(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")

	runAnalysisTest(t, testdata, h.Analyzer,
		"constructorvalidates_pkg_collision/util",
		"constructorvalidates_pkg_collision/myapp")
}

// TestConstructorValidatesGenericInstantiation verifies constructor-validates
// type identity distinguishes generic instantiations (for example Box[int] vs
// Box[string]) when evaluating transitive helper validation.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestConstructorValidatesGenericInstantiation(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "constructorvalidates_generic")
}

// TestCheckConstructorReturnErrorCrossPackage exercises
// --check-constructor-return-error against the cross-package fixture.
// Verifies that constructors returning cross-package types with Validate()
// but no error return are flagged, while those returning error are safe.
// Also enables --check-constructor-validates since the shared fixture has
// expectations for both checks.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckConstructorReturnErrorCrossPackage(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-return-error", "true")
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")

	runAnalysisTest(t, testdata, h.Analyzer,
		"constructorvalidates_cross/util",
		"constructorvalidates_cross/myapp")
}

// TestConstructorValidatesException exercises that --check-constructor-validates
// respects TOML exception patterns (e.g., "pkg.NewFoo.constructor-validate").
// The configexceptions fixture has NewServiceConfig which does not call
// Validate() but is excepted via TOML — no constructor-validates finding.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestConstructorValidatesException(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", "configexceptions", "goplint.toml"))
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "configexceptions")
}

// TestSuggestValidateAll exercises the --suggest-validate-all advisory mode
// against the suggestvalidateall fixture, verifying that structs with Validate()
// and validatable fields but no //goplint:validate-all directive are reported.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestSuggestValidateAll(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "suggest-validate-all", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "suggestvalidateall")
}

// TestCheckNonZero exercises the --check-nonzero mode against the nonzero
// fixture (same-package) and nonzero_consumer fixture (cross-package fact
// propagation), verifying that struct fields using nonzero-annotated types
// as value (non-pointer) fields are flagged both within and across packages.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckNonZero(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-nonzero", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "nonzero", "nonzero_consumer")
}

// TestCheckRedundantConversion exercises the --check-redundant-conversion mode
// against the redundantconversion fixture, verifying that type conversions with
// a redundant intermediate basic-type hop (e.g., TokenB(string(tokenA))) are
// flagged, while valid patterns (direct conversion, literal args, function calls)
// are not.
func TestCheckRedundantConversion(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-redundant-conversion", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "redundantconversion")
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
// NOT parallel: shares h.Analyzer.Flags state.
func TestAuditReviewDates(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", "auditreviewdates", "config.toml"))
	setFlag(t, h.Analyzer, "audit-review-dates", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "auditreviewdates")
}

// TestCheckEnumSync exercises the --check-enum-sync mode against the
// enumsync fixture, verifying that CUE disjunction members missing from
// Go Validate() switch cases and extra Go cases not in CUE are flagged.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckEnumSync(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-enum-sync", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "enumsync")
}

func TestCheckEnumSyncNoSchema(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-enum-sync", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "enumsync_noschema")
}

// TestCheckEnumSyncNoSchemaExceptionSuppression verifies no-schema enum-sync
// diagnostics respect TOML exceptions.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckEnumSyncNoSchemaExceptionSuppression(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-enum-sync", "true")
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", "enumsync_noschema_suppressed", "goplint.toml"))

	runAnalysisTest(t, testdata, h.Analyzer, "enumsync_noschema_suppressed")
}

// TestCheckEnumSyncNoSchemaBaselineSuppression verifies no-schema enum-sync
// diagnostics respect baseline suppression.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckEnumSyncNoSchemaBaselineSuppression(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-enum-sync", "true")
	setFlag(t, h.Analyzer, "baseline", filepath.Join(testdata, "src", "enumsync_noschema_suppressed", "goplint-baseline.toml"))

	runAnalysisTest(t, testdata, h.Analyzer, "enumsync_noschema_suppressed")
}

// TestCheckEnumSyncCueErrorExceptionSuppression verifies cue-error enum-sync
// diagnostics respect TOML exceptions.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckEnumSyncCueErrorExceptionSuppression(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-enum-sync", "true")
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", "enumsync_cueerror_suppressed", "goplint.toml"))

	runAnalysisTest(t, testdata, h.Analyzer, "enumsync_cueerror_suppressed")
}

// TestCheckEnumSyncMultiFile exercises enum-sync with multiple CUE schema
// files in the same package directory. Verifies that definitions from
// different schema files are correctly loaded after concatenation.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckEnumSyncMultiFile(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-enum-sync", "true")

	// No `want` annotations needed — both Mode and Format are
	// fully synced with their respective CUE schema files.
	runAnalysisTest(t, testdata, h.Analyzer, "enumsync_multifile")
}

// TestCheckEnumSyncScopedSwitches verifies enum-sync only considers switch
// statements that target the annotated receiver value.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestCheckEnumSyncScopedSwitches(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-enum-sync", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "enumsync_multiswitch_scoped")
}

// TestCheckValidateDelegationUniversal exercises the unified
// --check-validate-delegation mode against the validatedelegationall fixture.
// Verifies:
//   - Structs with validatable fields but no Validate() are flagged
//   - Structs with Validate() but incomplete delegation are flagged
//   - Error-type structs are excluded
//   - //goplint:no-delegate fields are respected
//   - Complete delegation is not flagged
//   - Structs without validatable fields are not flagged
func TestCheckValidateDelegationUniversal(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-validate-delegation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "validatedelegationall")
}
