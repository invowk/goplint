// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestProtocolInconclusivePolicyMatchesLiveCatalog(t *testing.T) {
	t.Parallel()

	catalog := loadInconclusivePolicyCatalog(t)
	if err := validateProtocolInconclusivePolicy(
		catalog.Rules,
		diagnosticCategoryRegistry(),
		IsProtocolInconclusiveCategory,
	); err != nil {
		t.Fatalf("validateProtocolInconclusivePolicy() error = %v", err)
	}
}

func TestProtocolSuppressionPolicyIsConsultedOnlyAfterDefiniteClassification(t *testing.T) {
	t.Parallel()

	for _, outcome := range []pathOutcome{pathOutcomeSafe, pathOutcomeInconclusive} {
		consulted := 0
		suppressed := protocolPolicySuppressesDefiniteFinding(outcome, func() bool {
			consulted++
			return true
		})
		if suppressed || consulted != 0 {
			t.Fatalf("outcome %v suppression = %t, consultations = %d; want false/0", outcome, suppressed, consulted)
		}
	}
	consulted := 0
	if !protocolPolicySuppressesDefiniteFinding(pathOutcomeUnsafe, func() bool {
		consulted++
		return true
	}) || consulted != 1 {
		t.Fatalf("definite violation suppression consultations = %d, want exactly 1", consulted)
	}
}

func TestProtocolInconclusivePolicyRejectsMutations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func([]semanticCoverageRule, []CategorySpec)
		classifier func(string) bool
		wantError  string
	}{
		{
			name: "suppressible policy",
			mutate: func(_ []semanticCoverageRule, categories []CategorySpec) {
				category := categorySpecIndex(categories, CategoryUnvalidatedCastInconclusive)
				categories[category].BaselinePolicy = BaselineSuppressible
			},
			classifier: IsProtocolInconclusiveCategory,
			wantError:  "baseline suppressible",
		},
		{
			name: "baseline label",
			mutate: func(_ []semanticCoverageRule, categories []CategorySpec) {
				category := categorySpecIndex(categories, CategoryUnvalidatedCastInconclusive)
				categories[category].BaselineLabel = "hidden uncertainty"
			},
			classifier: IsProtocolInconclusiveCategory,
			wantError:  "has baseline label",
		},
		{
			name: "semantic policy",
			mutate: func(rules []semanticCoverageRule, _ []CategorySpec) {
				rule := semanticRuleIndex(rules, CategoryUnvalidatedCastInconclusive)
				rules[rule].BaselinePolicy = "suppressible"
			},
			classifier: IsProtocolInconclusiveCategory,
			wantError:  "semantic baseline_policy",
		},
		{
			name:   "classifier omits exact inconclusive",
			mutate: func([]semanticCoverageRule, []CategorySpec) {},
			classifier: func(category string) bool {
				return category != CategoryUnvalidatedCastInconclusive && IsProtocolInconclusiveCategory(category)
			},
			wantError: "exact registered meaning = true",
		},
		{
			name:   "classifier marks mixed category",
			mutate: func([]semanticCoverageRule, []CategorySpec) {},
			classifier: func(category string) bool {
				return category == CategoryUnvalidatedBoundaryRequest || IsProtocolInconclusiveCategory(category)
			},
			wantError: "exact registered meaning = false",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			catalog := loadInconclusivePolicyCatalog(t)
			categories := diagnosticCategoryRegistry()
			test.mutate(catalog.Rules, categories)
			err := validateProtocolInconclusivePolicy(catalog.Rules, categories, test.classifier)
			assertInconclusivePolicyErrorContains(t, err, test.wantError)
		})
	}
}

func loadInconclusivePolicyCatalog(t *testing.T) semanticCoverageCatalog {
	t.Helper()
	data, err := os.ReadFile("../spec/semantic-rules.v1.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var catalog semanticCoverageCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return catalog
}

func categorySpecIndex(categories []CategorySpec, category string) int {
	for index := range categories {
		if categories[index].Name == category {
			return index
		}
	}
	panic("missing test category " + category)
}

func semanticRuleIndex(rules []semanticCoverageRule, category string) int {
	for index := range rules {
		if rules[index].Category == category {
			return index
		}
	}
	panic("missing test rule " + category)
}

func assertInconclusivePolicyErrorContains(t *testing.T, err error, substring string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", substring)
	}
	if !strings.Contains(err.Error(), substring) {
		t.Fatalf("error = %q, want substring %q", err, substring)
	}
}
