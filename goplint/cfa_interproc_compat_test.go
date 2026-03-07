// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestClassifyInterprocDowngrade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		legacy              interprocOutcomeClass
		ifds                interprocOutcomeClass
		hasEquivalentUnsafe bool
		wantViolation       bool
	}{
		{name: "unsafe to safe is forbidden", legacy: interprocOutcomeUnsafe, ifds: interprocOutcomeSafe, wantViolation: true},
		{name: "inconclusive to safe without unsafe is forbidden", legacy: interprocOutcomeInconclusive, ifds: interprocOutcomeSafe, wantViolation: true},
		{name: "inconclusive to safe with equivalent unsafe is allowed", legacy: interprocOutcomeInconclusive, ifds: interprocOutcomeSafe, hasEquivalentUnsafe: true},
		{name: "safe to unsafe is allowed", legacy: interprocOutcomeSafe, ifds: interprocOutcomeUnsafe},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, got := classifyInterprocDowngrade("cat", "id", tt.legacy, tt.ifds, tt.hasEquivalentUnsafe)
			if got != tt.wantViolation {
				t.Fatalf("classifyInterprocDowngrade() violation = %t, want %t", got, tt.wantViolation)
			}
		})
	}
}

func TestInterprocCompatTrackerErr(t *testing.T) {
	t.Parallel()

	tracker := newInterprocCompatTracker(cfgInterprocEngineCompare)
	tracker.Check(
		CategoryUnvalidatedCast,
		"finding-id",
		interprocPathResult{Class: interprocOutcomeUnsafe},
		interprocPathResult{Class: interprocOutcomeSafe},
		false,
	)
	if err := tracker.Err(); err == nil {
		t.Fatal("expected compare tracker error for forbidden downgrade")
	}
}

func TestCompareInterprocOutcomeSetsMissingIFDSDefaultsConservative(t *testing.T) {
	t.Parallel()

	legacy := map[string]interprocOutcomeClass{
		"compat|id": interprocOutcomeUnsafe,
	}
	ifds := map[string]interprocOutcomeClass{}

	violations := compareInterprocOutcomeSets(legacy, ifds, nil)
	if len(violations) != 0 {
		t.Fatalf("expected no violations for missing IFDS key treated as inconclusive, got %+v", violations)
	}
}

func TestIFDSCompatNoSilentDowngrade(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	rulesByCategory := make(map[string]semanticRuleSpec, len(catalog.Rules))
	for _, rule := range catalog.Rules {
		rulesByCategory[rule.Category] = rule
	}

	for _, oracle := range catalog.OracleMatrix {
		rule, ok := rulesByCategory[oracle.Category]
		if !ok {
			t.Fatalf("missing rule for category %q", oracle.Category)
		}
		fixtures := map[string]struct{}{}
		for _, entry := range oracle.MustReport {
			fixtures[entry.Fixture] = struct{}{}
		}
		for _, entry := range oracle.MustNotReport {
			fixtures[entry.Fixture] = struct{}{}
		}

		for fixture := range fixtures {
			legacy := collectCompatibilityOutcomes(t, rule, oracle.Category, fixture, cfgInterprocEngineLegacy)
			ifds := collectCompatibilityOutcomes(t, rule, oracle.Category, fixture, cfgInterprocEngineIFDS)
			unsafeIFDS := make([]string, 0, len(ifds))
			for key, class := range ifds {
				if class == interprocOutcomeUnsafe {
					unsafeIFDS = append(unsafeIFDS, key)
				}
			}
			violations := compareInterprocOutcomeSets(legacy, ifds, unsafeIFDS)
			if len(violations) != 0 {
				t.Fatalf(
					"expected no IFDS compatibility downgrade violations for category=%q fixture=%q, got %d: %+v",
					oracle.Category,
					fixture,
					len(violations),
					violations,
				)
			}
		}
	}
}

func collectCompatibilityOutcomes(
	t *testing.T,
	rule semanticRuleSpec,
	category string,
	fixture string,
	engine string,
) map[string]interprocOutcomeClass {
	t.Helper()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	configureSemanticOracleRun(t, h.Analyzer, rule, category, fixture)
	setFlag(t, h.Analyzer, "cfg-interproc-engine", engine)
	// Phase C/D are tested by dedicated gate tests; disable here to
	// isolate the interprocedural engine comparison.
	setFlag(t, h.Analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineOff)
	setFlag(t, h.Analyzer, "cfg-refinement-mode", cfgRefinementModeOff)
	setFlag(t, h.Analyzer, "cfg-alias-mode", cfgAliasModeOff)

	diagnostics, _, results := collectDiagnosticsForPackagesRespectCurrentEngine(t, h.Analyzer, fixture)
	for _, result := range results {
		if result != nil && result.Err != nil {
			t.Fatalf("analysis result error: %v", result.Err)
		}
	}

	out := make(map[string]interprocOutcomeClass)
	for _, diag := range diagnostics {
		class, ok := compatibilityClassForCategory(diag.Category)
		if !ok {
			continue
		}
		if diag.Category != category {
			continue
		}
		id := FindingIDFromDiagnosticURL(diag.URL)
		if id == "" {
			continue
		}
		out[diag.Category+"|"+id] = class
	}
	return out
}

func compatibilityClassForCategory(category string) (interprocOutcomeClass, bool) {
	switch category {
	case CategoryUnvalidatedCast,
		CategoryUseBeforeValidateSameBlock,
		CategoryUseBeforeValidateCrossBlock,
		CategoryMissingConstructorValidate:
		return interprocOutcomeUnsafe, true
	case CategoryUnvalidatedCastInconclusive,
		CategoryUseBeforeValidateInconclusive,
		CategoryMissingConstructorValidateInc:
		return interprocOutcomeInconclusive, true
	default:
		return interprocOutcomeSafe, false
	}
}
