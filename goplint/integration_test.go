// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"flag"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

type analyzerHarness struct {
	Analyzer *analysis.Analyzer
	state    *flagState
}

var analysistestParallelLimiter = make(chan struct{}, 2)

func newAnalyzerHarness() analyzerHarness {
	state := &flagState{}
	return analyzerHarness{
		Analyzer: newAnalyzerWithState(state),
		state:    state,
	}
}

// setFlag sets an analyzer flag by name using the framework-standard
// h.Analyzer.Flags.Set() API. This is the same pattern used by x/tools
// tests (testinggoroutine, findcall).
func setFlag(t *testing.T, analyzer *analysis.Analyzer, name, value string) {
	t.Helper()
	if err := analyzer.Flags.Set(name, value); err != nil {
		t.Fatalf("failed to set flag %q to %q: %v", name, value, err)
	}
}

// resetFlags restores all analyzer flags to their default values.
// Called via t.Cleanup() to ensure clean state between tests.
func resetFlags(t *testing.T, h analyzerHarness) {
	t.Helper()
	resetFlagStateDefaults(h.state)
	// Keep tracked string flags at defaults without marking them explicit.
	// resetFlagStateDefaults updates the underlying bound state directly.
	for _, spec := range modeFlagSpecs() {
		setFlag(t, h.Analyzer, spec.flagName, strconv.FormatBool(spec.defaultValue))
	}
}

func runAnalysisTest(t *testing.T, testdata string, analyzer *analysis.Analyzer, pkgs ...string) {
	t.Helper()

	// Keep fixture expectations stable across default-engine rollouts.
	// IFDS/compare behavior is covered by dedicated compatibility tests.
	// Phase C/D behavior is covered by dedicated gate tests.
	setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineLegacy)
	setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineOff)
	setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOff)
	setFlag(t, analyzer, "cfg-alias-mode", cfgAliasModeOff)

	analysistestParallelLimiter <- struct{}{}
	defer func() { <-analysistestParallelLimiter }()

	analysistest.Run(t, testdata, analyzer, pkgs...)
}

// TestResetFlagsCompleteness ensures resetFlags restores every analyzer flag
// to its declared default, preventing drift when new flags are introduced.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestResetFlagsCompleteness(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)

	specsByName := make(map[string]modeFlagSpec, len(modeFlagSpecs()))
	for _, spec := range modeFlagSpecs() {
		specsByName[spec.flagName] = spec
	}

	boolFlags := make(map[string]*flag.Flag)
	h.Analyzer.Flags.VisitAll(func(f *flag.Flag) {
		if f.DefValue == "false" || f.DefValue == "true" {
			boolFlags[f.Name] = f
		}
	})
	if len(boolFlags) != len(modeFlagSpecs()) {
		t.Fatalf("mode specs mismatch: analyzer bool flags=%d specs=%d", len(boolFlags), len(modeFlagSpecs()))
	}
	for name, analyzerFlag := range boolFlags {
		spec, ok := specsByName[name]
		if !ok {
			t.Fatalf("analyzer bool flag %q missing from modeFlagSpecs", name)
		}
		if analyzerFlag.DefValue != strconv.FormatBool(spec.defaultValue) {
			t.Fatalf("mode spec default mismatch for %q: analyzer=%q spec=%t", name, analyzerFlag.DefValue, spec.defaultValue)
		}
	}
	for _, spec := range modeFlagSpecs() {
		if _, ok := boolFlags[spec.flagName]; !ok {
			t.Fatalf("modeFlagSpecs entry %q missing from analyzer flags", spec.flagName)
		}
	}

	// Mutate each flag away from its default.
	setFlag(t, h.Analyzer, "config", "__non_default__")
	setFlag(t, h.Analyzer, "baseline", "__non_default__")
	for _, spec := range modeFlagSpecs() {
		setFlag(t, h.Analyzer, spec.flagName, strconv.FormatBool(!spec.defaultValue))
	}

	// Restore defaults and verify all flags are reset.
	resetFlags(t, h)
	for _, name := range []string{"config", "baseline", "include-packages"} {
		f := h.Analyzer.Flags.Lookup(name)
		if f == nil {
			t.Fatalf("missing analyzer flag %q", name)
		}
		if got := f.Value.String(); got != f.DefValue {
			t.Errorf("flag %q reset mismatch: got %q, want default %q", f.Name, got, f.DefValue)
		}
	}
	h.Analyzer.Flags.VisitAll(func(f *flag.Flag) {
		if f.DefValue != "false" && f.DefValue != "true" {
			return
		}
		if got := f.Value.String(); got != f.DefValue {
			t.Errorf("flag %q reset mismatch: got %q, want default %q", f.Name, got, f.DefValue)
		}
	})
}

// TestNewRunConfig verifies the --check-all expansion logic and the
// deliberate exclusion of --audit-exceptions.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestNewRunConfig(t *testing.T) {
	h := newAnalyzerHarness()

	t.Run("check-all expansion follows mode specs", func(t *testing.T) {
		resetFlags(t, h)
		setFlag(t, h.Analyzer, "check-all", "true")

		rc := newRunConfigForState(h.state)

		for _, spec := range modeFlagSpecs() {
			want := spec.defaultValue
			if spec.flagName == "check-all" || spec.includeInCheckAll {
				want = true
			}
			if got := spec.runConfigValue(&rc); got != want {
				t.Errorf("mode %q mismatch: got %t, want %t (check-all=%t, include=%t)",
					spec.flagName, got, want, rc.checkAll, spec.includeInCheckAll)
			}
		}
	})

	t.Run("check-all with explicit audit-exceptions preserves both", func(t *testing.T) {
		resetFlags(t, h)
		setFlag(t, h.Analyzer, "check-all", "true")
		setFlag(t, h.Analyzer, "audit-exceptions", "true")

		rc := newRunConfigForState(h.state)

		for _, spec := range modeFlagSpecs() {
			if spec.flagName == "audit-exceptions" && !spec.runConfigValue(&rc) {
				t.Fatal("expected audit-exceptions = true (explicitly set)")
			}
			if spec.flagName == "check-validate" && !spec.runConfigValue(&rc) {
				t.Fatal("expected check-validate = true (from check-all)")
			}
		}
	})

	t.Run("check-all does NOT enable opt-in and audit modes", func(t *testing.T) {
		resetFlags(t, h)
		setFlag(t, h.Analyzer, "check-all", "true")

		rc := newRunConfigForState(h.state)
		excludedModes := []struct {
			name string
			got  bool
		}{
			{name: "suggest-validate-all", got: rc.suggestValidateAll},
			{name: "check-enum-sync", got: rc.checkEnumSync},
			{name: "audit-exceptions", got: rc.auditExceptions},
			{name: "audit-review-dates", got: rc.auditReviewDates},
		}
		for _, mode := range excludedModes {
			if mode.got {
				t.Errorf("expected %s = false under --check-all", mode.name)
			}
		}
	})

	t.Run("individual flags work independently", func(t *testing.T) {
		resetFlags(t, h)
		setFlag(t, h.Analyzer, "check-validate", "true")

		rc := newRunConfigForState(h.state)
		for _, spec := range modeFlagSpecs() {
			want := spec.defaultValue
			if spec.flagName == "check-validate" {
				want = true
			}
			if got := spec.runConfigValue(&rc); got != want {
				t.Errorf("mode %q mismatch: got %t, want %t", spec.flagName, got, want)
			}
		}
	})

	t.Run("ubv flags auto-enable cast-validation", func(t *testing.T) {
		resetFlags(t, h)
		setFlag(t, h.Analyzer, "check-use-before-validate", "true")
		setFlag(t, h.Analyzer, "ubv-mode", ubvModeOrder)

		rc := newRunConfigForState(h.state)
		if !rc.checkCastValidation {
			t.Fatal("expected check-cast-validation to auto-enable with UBV flag")
		}
	})

	t.Run("cfg and ubv defaults are normalized", func(t *testing.T) {
		resetFlags(t, h)

		rc := newRunConfigForState(h.state)
		if rc.ubvMode != defaultUBVMode {
			t.Fatalf("expected ubvMode default %q, got %q", defaultUBVMode, rc.ubvMode)
		}
		if rc.cfgBackend != defaultCFGBackend {
			t.Fatalf("expected cfgBackend default %q, got %q", defaultCFGBackend, rc.cfgBackend)
		}
		if rc.cfgInterprocEngine != defaultCFGInterprocEngine {
			t.Fatalf("expected cfgInterprocEngine default %q, got %q", defaultCFGInterprocEngine, rc.cfgInterprocEngine)
		}
		if rc.cfgMaxStates != defaultCFGMaxStates {
			t.Fatalf("expected cfgMaxStates default %d, got %d", defaultCFGMaxStates, rc.cfgMaxStates)
		}
		if rc.cfgMaxDepth != defaultCFGMaxDepth {
			t.Fatalf("expected cfgMaxDepth default %d, got %d", defaultCFGMaxDepth, rc.cfgMaxDepth)
		}
		if rc.cfgInconclusivePolicy != defaultCFGInconclusivePolicy {
			t.Fatalf("expected cfgInconclusivePolicy default %q, got %q", defaultCFGInconclusivePolicy, rc.cfgInconclusivePolicy)
		}
		if rc.cfgWitnessMaxSteps != defaultCFGWitnessMaxSteps {
			t.Fatalf("expected cfgWitnessMaxSteps default %d, got %d", defaultCFGWitnessMaxSteps, rc.cfgWitnessMaxSteps)
		}
		if rc.cfgFeasibilityEngine != defaultCFGFeasibilityEngine {
			t.Fatalf("expected cfgFeasibilityEngine default %q, got %q", defaultCFGFeasibilityEngine, rc.cfgFeasibilityEngine)
		}
		if rc.cfgRefinementMode != defaultCFGRefinementMode {
			t.Fatalf("expected cfgRefinementMode default %q, got %q", defaultCFGRefinementMode, rc.cfgRefinementMode)
		}
		if rc.cfgRefinementMaxIterations != defaultCFGRefinementMaxIterations {
			t.Fatalf("expected cfgRefinementMaxIterations default %d, got %d", defaultCFGRefinementMaxIterations, rc.cfgRefinementMaxIterations)
		}
		if rc.cfgFeasibilityMaxQueries != defaultCFGFeasibilityMaxQueries {
			t.Fatalf("expected cfgFeasibilityMaxQueries default %d, got %d", defaultCFGFeasibilityMaxQueries, rc.cfgFeasibilityMaxQueries)
		}
		if rc.cfgFeasibilityTimeoutMS != defaultCFGFeasibilityTimeoutMS {
			t.Fatalf("expected cfgFeasibilityTimeoutMS default %d, got %d", defaultCFGFeasibilityTimeoutMS, rc.cfgFeasibilityTimeoutMS)
		}
	})
}

// TestTrackedStringFlagsExplicitness verifies config/baseline tracked string
// flags preserve explicit-set markers even when set to empty string.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestTrackedStringFlagsExplicitness(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)

	h.state.configPathExplicit = false
	h.state.baselinePathExplicit = false
	h.state.includePackagesExplicit = false

	setFlag(t, h.Analyzer, "config", "")
	if !h.state.configPathExplicit {
		t.Fatal("expected configPathExplicit = true after setting --config")
	}

	setFlag(t, h.Analyzer, "baseline", "")
	if !h.state.baselinePathExplicit {
		t.Fatal("expected baselinePathExplicit = true after setting --baseline")
	}

	setFlag(t, h.Analyzer, "include-packages", "")
	if !h.state.includePackagesExplicit {
		t.Fatal("expected includePackagesExplicit = true after setting --include-packages")
	}
}

// TestNewRunConfigCarriesTrackedStringExplicitness verifies that explicit-set
// markers for --config/--baseline are copied into runConfig.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestNewRunConfigCarriesTrackedStringExplicitness(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)

	setFlag(t, h.Analyzer, "config", "")
	setFlag(t, h.Analyzer, "baseline", "")

	rc := newRunConfigForState(h.state)
	if !rc.configPathExplicit {
		t.Fatal("expected runConfig.configPathExplicit = true after setting --config")
	}
	if !rc.baselinePathExplicit {
		t.Fatal("expected runConfig.baselinePathExplicit = true after setting --baseline")
	}

	setFlag(t, h.Analyzer, "include-packages", "")
	rc = newRunConfigForState(h.state)
	if !rc.includePackagesExplicit {
		t.Fatal("expected runConfig.includePackagesExplicit = true after setting --include-packages")
	}
}

// TestAnalyzerWithConfig exercises the full analyzer pipeline with a
// TOML config file loaded, verifying that exception patterns and
// skip_types correctly suppress findings.
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestAnalyzerWithConfig(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", "configexceptions", "goplint.toml"))

	runAnalysisTest(t, testdata, h.Analyzer, "configexceptions")
}

// TestAnalyzerWithRealExceptionsToml loads the real project exceptions.toml
// and runs against the basic fixture to verify:
//  1. The real config file parses without error.
//  2. It doesn't accidentally suppress basic findings (the basic fixture
//     has no patterns matching the real exceptions).
//
// NOT parallel: shares h.Analyzer.Flags state.
func TestAnalyzerWithRealExceptionsToml(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "..", "..", "exceptions.toml"))
	// Override include_packages from the real config — test fixtures use
	// short package paths (e.g., "basic") that don't match the production
	// prefix "github.com/invowk/invowk".
	setFlag(t, h.Analyzer, "include-packages", "basic")

	runAnalysisTest(t, testdata, h.Analyzer, "basic")
}
