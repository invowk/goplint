// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"slices"
)

const alwaysVisibleBaselinePolicy = "always-visible"

// protocolPolicySuppressesDefiniteFinding consults suppression policy only for
// a classified definite violation. Safe and inconclusive outcomes bypass every
// policy callback, so uncertainty remains visible and policy audit counters are
// not falsely credited.
func protocolPolicySuppressesDefiniteFinding(
	outcome pathOutcome,
	checks ...func() bool,
) bool {
	if outcome != pathOutcomeUnsafe {
		return false
	}
	for _, check := range checks {
		if check != nil && check() {
			return true
		}
	}
	return false
}

func validateProtocolInconclusivePolicy(
	rules []semanticCoverageRule,
	categories []CategorySpec,
	classifier func(string) bool,
) error {
	rulesByCategory := make(map[string]semanticCoverageRule, len(rules))
	for _, rule := range rules {
		rulesByCategory[rule.Category] = rule
	}
	for _, category := range categories {
		rule, exists := rulesByCategory[category.Name]
		if !exists {
			return fmt.Errorf("category %q has no semantic rule for inconclusive policy validation", category.Name)
		}
		exactProtocolInconclusive := category.SemanticKind == semanticKindProtocol &&
			slices.Equal(rule.OutcomeDomain, []string{"inconclusive"})
		classified := classifier(category.Name)
		if classified != exactProtocolInconclusive {
			return fmt.Errorf(
				"category %q inconclusive classifier = %t, exact registered meaning = %t",
				category.Name,
				classified,
				exactProtocolInconclusive,
			)
		}
		if !exactProtocolInconclusive {
			continue
		}
		if category.BaselinePolicy != BaselineAlwaysVisible {
			return fmt.Errorf("protocol inconclusive category %q is baseline suppressible", category.Name)
		}
		if category.BaselineLabel != "" {
			return fmt.Errorf("protocol inconclusive category %q has baseline label %q", category.Name, category.BaselineLabel)
		}
		if rule.BaselinePolicy != alwaysVisibleBaselinePolicy {
			return fmt.Errorf(
				"protocol inconclusive category %q semantic baseline_policy = %q, want %q",
				category.Name,
				rule.BaselinePolicy,
				alwaysVisibleBaselinePolicy,
			)
		}
	}
	return nil
}
