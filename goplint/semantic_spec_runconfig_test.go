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

func TestSemanticSpecInconclusiveRulesRequireWitnessControl(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	for _, rule := range catalog.Rules {
		if !slices.Contains(rule.OutcomeDomain, "inconclusive") {
			continue
		}
		if !slices.Contains(rule.RunControls, "cfg-witness-max-steps") {
			t.Fatalf("inconclusive rule %q must include cfg-witness-max-steps run control", rule.Category)
		}
	}
}

func TestSemanticSpecCFARulesRequireResourceControls(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	requiredControls := []string{
		"protocol-refinement-max-iterations",
		"protocol-feasibility-max-queries",
		"protocol-feasibility-timeout-ms",
	}

	for _, rule := range catalog.Rules {
		if !strings.HasPrefix(rule.Family, "cfa-") {
			continue
		}
		for _, control := range requiredControls {
			if !slices.Contains(rule.RunControls, control) {
				t.Fatalf("protocol rule %q must include resource control %q", rule.Category, control)
			}
		}
	}
}
