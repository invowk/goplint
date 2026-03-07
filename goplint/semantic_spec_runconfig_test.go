// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"flag"
	"slices"
	"strings"
	"testing"
)

func TestSemanticSpecRunControlsAreKnownFlags(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	analyzer := NewAnalyzer()
	flagByName := make(map[string]*flag.Flag)
	analyzer.Flags.VisitAll(func(f *flag.Flag) {
		flagByName[f.Name] = f
	})

	for _, rule := range catalog.Rules {
		for _, enabledByFlag := range rule.EnabledByFlags {
			f, ok := flagByName[enabledByFlag]
			if !ok {
				t.Fatalf("rule %q references unknown enabled_by_flags entry %q", rule.Category, enabledByFlag)
			}
			if f.DefValue != "true" && f.DefValue != "false" {
				t.Fatalf("rule %q enabled flag %q must be bool (DefValue=%q)", rule.Category, enabledByFlag, f.DefValue)
			}
		}
		for _, control := range rule.RunControls {
			if _, ok := flagByName[control]; !ok {
				t.Fatalf("rule %q references unknown run_controls entry %q", rule.Category, control)
			}
		}
	}
}

func TestSemanticSpecInconclusiveRulesRequirePolicyControls(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	for _, rule := range catalog.Rules {
		if !slices.Contains(rule.OutcomeDomain, "inconclusive") {
			continue
		}
		if !slices.Contains(rule.RunControls, "cfg-inconclusive-policy") {
			t.Fatalf("inconclusive rule %q must include cfg-inconclusive-policy run control", rule.Category)
		}
		if !slices.Contains(rule.RunControls, "cfg-witness-max-steps") {
			t.Fatalf("inconclusive rule %q must include cfg-witness-max-steps run control", rule.Category)
		}
	}
}

func TestSemanticSpecCFARulesRequirePhaseCControls(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	requiredControls := []string{
		"cfg-feasibility-engine",
		"cfg-refinement-mode",
		"cfg-refinement-max-iterations",
		"cfg-feasibility-max-queries",
		"cfg-feasibility-timeout-ms",
	}

	for _, rule := range catalog.Rules {
		if !strings.HasPrefix(rule.Family, "cfa-") {
			continue
		}
		for _, control := range requiredControls {
			if !slices.Contains(rule.RunControls, control) {
				t.Fatalf("CFA rule %q must include Phase C run control %q", rule.Category, control)
			}
		}
	}
}

func TestSemanticSpecAliasSensitiveRulesRequireAliasControl(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	requiredCategories := map[string]bool{
		CategoryUnvalidatedCast:               true,
		CategoryUnvalidatedCastInconclusive:   true,
		CategoryUseBeforeValidateSameBlock:    true,
		CategoryUseBeforeValidateCrossBlock:   true,
		CategoryUseBeforeValidateInconclusive: true,
	}

	for _, rule := range catalog.Rules {
		if !requiredCategories[rule.Category] {
			continue
		}
		if !slices.Contains(rule.RunControls, "cfg-alias-mode") {
			t.Fatalf("alias-sensitive rule %q must include cfg-alias-mode run control", rule.Category)
		}
	}
}
