// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestProtocolRedBaselineBlackBox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		flags   map[string]string
	}{
		{
			name:    "conditional-validation-edge",
			fixture: "red_baseline_conditional_validation",
			flags:   map[string]string{"check-cast-validation": "true"},
		},
		{
			name:    "constructor-return-identity",
			fixture: "red_baseline_constructor_identity",
			flags:   map[string]string{"check-constructor-validates": "true"},
		},
		{
			name:    "post-validation-unknown-effect",
			fixture: "red_baseline_unknown_effect",
			flags:   map[string]string{"check-cast-validation": "true"},
		},
		{
			name:    "ambiguous-alias-optimism",
			fixture: "red_baseline_ambiguous_alias",
			flags:   map[string]string{"check-cast-validation": "true"},
		},
		{
			name:    "concrete-stack-recursion",
			fixture: "red_baseline_recursion",
			flags:   map[string]string{"check-cast-validation": "true"},
		},
		{
			name:    "no-return-alias-rebinding",
			fixture: "red_baseline_no_return_alias",
			flags:   map[string]string{"check-cast-validation": "true"},
		},
		{
			name:    "generic-obligation-disappears",
			fixture: "red_baseline_generic_constraint",
			flags:   map[string]string{"check-cast-validation": "true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := newAnalyzerHarness()
			for name, value := range tt.flags {
				setFlag(t, harness.Analyzer, name, value)
			}
			analysistest.Run(t, analysistest.TestData(), harness.Analyzer, tt.fixture)
		})
	}
}
