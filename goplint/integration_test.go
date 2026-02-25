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
	setFlag(t, "check-isvalid", "false")
	setFlag(t, "check-stringer", "false")
	setFlag(t, "check-constructors", "false")
	setFlag(t, "check-constructor-sig", "false")
	setFlag(t, "check-func-options", "false")
	setFlag(t, "check-immutability", "false")
	setFlag(t, "check-struct-isvalid", "false")
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

		if !rc.checkIsValid {
			t.Error("expected checkIsValid = true")
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
		if !rc.checkStructIsValid {
			t.Error("expected checkStructIsValid = true")
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
		if !rc.checkIsValid {
			t.Error("expected checkIsValid = true (from check-all)")
		}
	})

	t.Run("individual flags work independently", func(t *testing.T) {
		resetFlags(t)
		setFlag(t, "check-isvalid", "true")

		rc := newRunConfig()

		if !rc.checkIsValid {
			t.Error("expected checkIsValid = true")
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

// TestCheckIsValid exercises the --check-isvalid mode against the isvalid
// fixture, verifying named types without IsValid() are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckIsValid(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-isvalid", "true")

	analysistest.Run(t, testdata, Analyzer, "isvalid")
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

// TestCheckAll exercises the --check-all flag, confirming it enables all
// DDD compliance checks (isvalid, stringer, constructors, constructor-sig,
// func-options, immutability) in a single run. Also explicitly enables
// --audit-exceptions to verify all 8 diagnostic categories fire together.
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

// TestCheckStructIsValid exercises the --check-struct-isvalid mode against
// the structisvalid fixture, verifying exported structs with constructors
// but missing IsValid() are flagged.
//
// NOT parallel: shares Analyzer.Flags state.
func TestCheckStructIsValid(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-struct-isvalid", "true")

	analysistest.Run(t, testdata, Analyzer, "structisvalid")
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
// works for supplementary modes (missing-isvalid, missing-stringer,
// missing-constructor). Some findings are baselined and suppressed; others
// are new and reported.
//
// NOT parallel: shares Analyzer.Flags state.
func TestBaselineSupplementaryCategories(t *testing.T) {
	testdata := analysistest.TestData()
	t.Cleanup(func() { resetFlags(t) })
	setFlag(t, "check-isvalid", "true")
	setFlag(t, "check-stringer", "true")
	setFlag(t, "check-constructors", "true")
	setFlag(t, "baseline", filepath.Join(testdata, "src", "baseline_supplementary", "goplint-baseline.toml"))

	analysistest.Run(t, testdata, Analyzer, "baseline_supplementary")
}
