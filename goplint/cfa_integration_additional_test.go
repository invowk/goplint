// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCheckUseBeforeValidateClosureCastCFA(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	h := newAnalyzerHarness()
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	setFlag(t, h.Analyzer, "check-use-before-validate", "true")
	setFlag(t, h.Analyzer, "check-use-before-validate-cross", "true")

	runAnalysisTest(t, testdata, h.Analyzer, "cfa_closure_ubv")
}
