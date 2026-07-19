// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestProtocolCounterexamplesBlackBox intentionally uses only the analyzer
// boundary and analysistest expectations. Keeping these counterexamples free
// of solver-internal types lets the same tests-only overlay be evaluated
// against the pre-hardening base revision when reconstructing the red baseline.
func TestProtocolCounterexamplesBlackBox(t *testing.T) {
	t.Parallel()

	for _, packageName := range []string{
		"protocol_validation_counterexamples",
		"cfa_join_refinement",
		"cfa_alias_counterexamples",
		"cfa_recursive_summary",
		"cfa_generic_constraints",
	} {
		t.Run(packageName, func(t *testing.T) {
			t.Parallel()

			harness := newAnalyzerHarness()
			setFlag(t, harness.Analyzer, "check-cast-validation", "true")
			analysistest.Run(t, analysistest.TestData(), harness.Analyzer, packageName)
		})
	}
}
