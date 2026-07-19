// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

type analysisErrorCollector struct {
	errors []string
}

func (c *analysisErrorCollector) Errorf(format string, args ...any) {
	c.errors = append(c.errors, fmt.Sprintf(format, args...))
}

func mustLoadSemanticRuleCatalog(t *testing.T) semanticRuleCatalog {
	t.Helper()
	catalog, err := loadSemanticRuleCatalog()
	if err != nil {
		t.Fatalf("loadSemanticRuleCatalog() error: %v", err)
	}
	return catalog
}

func collectDiagnosticsForPackages(t *testing.T, analyzer *analysis.Analyzer, pkgs ...string) ([]analysis.Diagnostic, []string, []*analysistest.Result) {
	t.Helper()
	analysistestParallelLimiter <- struct{}{}
	defer func() { <-analysistestParallelLimiter }()

	collector := &analysisErrorCollector{}
	testdata := analysistest.TestData()
	results := analysistest.Run(collector, testdata, analyzer, pkgs...)
	diagnostics := make([]analysis.Diagnostic, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		diagnostics = append(diagnostics, result.Diagnostics...)
	}
	return diagnostics, collector.errors, results
}
