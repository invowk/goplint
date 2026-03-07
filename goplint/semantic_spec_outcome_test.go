// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"maps"
	"slices"
	"testing"
)

func TestSemanticSpecInconclusiveReasonContract(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	knownReasons := map[string]struct{}{
		string(pathOutcomeReasonStateBudget):      {},
		string(pathOutcomeReasonDepthBudget):      {},
		string(pathOutcomeReasonRecursionCycle):   {},
		string(pathOutcomeReasonUnresolvedTarget): {},
	}

	for _, rule := range catalog.Rules {
		if !slices.Contains(rule.OutcomeDomain, "inconclusive") {
			continue
		}
		if len(rule.InconclusiveReasons) == 0 {
			t.Fatalf("rule %q must declare inconclusive_reasons", rule.Category)
		}
		for _, reason := range rule.InconclusiveReasons {
			if _, ok := knownReasons[reason]; !ok {
				t.Fatalf("rule %q has unknown inconclusive reason %q", rule.Category, reason)
			}
		}
	}
}

func TestSemanticSpecInconclusiveMetaContract(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	meta := cfgOutcomeMetaWithWitness(
		cfgBackendSSA,
		defaultCFGMaxStates,
		defaultCFGMaxDepth,
		pathOutcomeReasonStateBudget,
		[]int32{0, 1, 2},
		defaultCFGWitnessMaxSteps,
	)
	addCFGWitnessCallChainMeta(meta, []string{"pkg.Func", "pkg.helper"}, defaultCFGWitnessMaxSteps)
	meta["cfg_inconclusive_policy"] = cfgInconclusivePolicyWarn
	meta["cfg_inconclusive_severity"] = "warning"
	available := map[string]struct{}{}
	maps.Copy(available, mapKeys(meta))

	for _, rule := range catalog.Rules {
		if !slices.Contains(rule.OutcomeDomain, "inconclusive") {
			continue
		}
		for _, key := range rule.RequiredMetaOnInconclusive {
			if _, ok := available[key]; !ok {
				t.Fatalf("rule %q requires unsupported inconclusive meta key %q", rule.Category, key)
			}
		}
	}
}

func TestSemanticSpecRefinementStatusContract(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	knownStatuses := map[string]struct{}{
		cfgRefinementStatusUnsafe:              {},
		cfgRefinementStatusInconclusiveRefined: {},
		cfgRefinementStatusInconclusiveRaw:     {},
		cfgRefinementStatusProvenSafe:          {},
	}

	for _, rule := range catalog.Rules {
		if len(rule.RefinementStatuses) == 0 {
			continue
		}
		for _, status := range rule.RefinementStatuses {
			if _, ok := knownStatuses[status]; !ok {
				t.Fatalf("rule %q has unknown refinement status %q", rule.Category, status)
			}
		}
	}
}

func TestSemanticSpecRefinementMetaContract(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	meta := cfgOutcomeMetaWithWitness(
		cfgBackendSSA,
		defaultCFGMaxStates,
		defaultCFGMaxDepth,
		pathOutcomeReasonStateBudget,
		[]int32{0, 1, 2},
		defaultCFGWitnessMaxSteps,
	)
	meta = appendPhaseCMeta(meta, interprocPathResult{
		PhaseC: cfgPhaseCResult{
			Enabled:              true,
			FeasibilityEngine:    cfgFeasibilityEngineSMT,
			FeasibilityResult:    cfgFeasibilityResultSAT,
			RefinementStatus:     cfgRefinementStatusUnsafe,
			RefinementIterations: 1,
			RefinementTrigger:    cfgRefinementTriggerUnsafeCandidate,
			WitnessHash:          "cfgw1_deadbeef",
		},
	})
	available := mapKeys(meta)

	for _, rule := range catalog.Rules {
		for _, key := range rule.RequiredMetaOnRefinement {
			if _, ok := available[key]; !ok {
				t.Fatalf("rule %q requires unsupported refinement meta key %q", rule.Category, key)
			}
		}
	}
}

func mapKeys(input map[string]string) map[string]struct{} {
	keys := make(map[string]struct{}, len(input))
	for key := range input {
		keys[key] = struct{}{}
	}
	return keys
}
