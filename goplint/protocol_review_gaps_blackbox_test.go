// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestProtocolReviewGapsBlackBox locks the review counterexamples at the
// registered analyzer boundary. These fixtures intentionally contain no
// solver-internal assertions, so they also remain usable as red overlays when
// reconstructing the pre-fix observations.
func TestProtocolReviewGapsBlackBox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		packages []string
		flag     string
	}{
		{name: "ordered calls", packages: []string{"protocol_ordered_calls"}, flag: "check-cast-validation"},
		{name: "deferred constructors", packages: []string{"constructorvalidates_deferred"}, flag: "check-constructor-validates"},
		{name: "escaping closures", packages: []string{"cfa_escaping_closure"}, flag: "check-cast-validation"},
		{
			name:     "cross-package escaping closure",
			packages: []string{"cfa_escaping_closure_cross/lib", "cfa_escaping_closure_cross/app"},
			flag:     "check-cast-validation",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			harness := newAnalyzerHarness()
			resetFlags(t, harness)
			setFlag(t, harness.Analyzer, test.flag, "true")
			analysistest.Run(t, analysistest.TestData(), harness.Analyzer, test.packages...)
		})
	}
}
