// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestIncludePackagesFactExportOnly verifies excluded packages still run
// fact-export traversal so included packages can import those facts.
func TestIncludePackagesFactExportOnly(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")
	setFlag(t, h.Analyzer, "include-packages", "include_packages_factexport/app")

	runAnalysisTest(t, testdata, h.Analyzer,
		"include_packages_factexport/util",
		"include_packages_factexport/app")
}

func TestCheckConstructorErrorUsageCrossPackageSelector(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-error-usage", "true")

	runAnalysisTest(t, testdata, h.Analyzer,
		"constructorusage_crosspkg/lib",
		"constructorusage_crosspkg/app")
}

func TestCheckConstructorErrorUsageSuppression(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-error-usage", "true")
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", "constructorusage_suppressed", "goplint.toml"))
	setFlag(t, h.Analyzer, "baseline", filepath.Join(testdata, "src", "constructorusage_suppressed", "goplint-baseline.toml"))

	runAnalysisTest(t, testdata, h.Analyzer, "constructorusage_suppressed")
}

func TestCheckEnumSyncSwitchTagConversion(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-enum-sync", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "enumsync_switch_conversion")
}

func TestCheckEnumSyncQualifiedConstCases(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-enum-sync", "true")

	runAnalysisTest(t, testdata, h.Analyzer,
		"enumsync_qualified_consts/defs",
		"enumsync_qualified_consts")
}

func TestCheckEnumSyncIntMembers(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-enum-sync", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "enumsync_intmembers")
}

func TestCheckValidateDelegationHelperCycle(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-validate-delegation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "validatedelegation_helper_cycle")
}

func TestCheckValidateDelegationEmbeddedImportedPointer(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-validate-delegation", "true")

	runAnalysisTest(t, testdata, h.Analyzer,
		"validatedelegation_embedded_imported_pointer/dep",
		"validatedelegation_embedded_imported_pointer")
}

func TestCheckValidateDelegationHelperFunction(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-validate-delegation", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "validatedelegation_helper_function")
}
