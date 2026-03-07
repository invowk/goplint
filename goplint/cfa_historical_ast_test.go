// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestSemanticSpecHistoricalFixturesReplay(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	testdata := analysistest.TestData()
	oraclesByFixture := make(map[string]semanticHistoricalMissOracle, len(catalog.HistoricalMissOracles))
	for _, oracle := range catalog.HistoricalMissOracles {
		oraclesByFixture[oracle.Fixture] = oracle
	}

	for _, fixture := range catalog.HistoricalMissFixtures {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			oracle, ok := oraclesByFixture[fixture]
			if !ok {
				t.Fatalf("fixture %q is missing historical oracle coverage", fixture)
			}
			h := newAnalyzerHarness()
			resetFlags(t, h)
			configureHistoricalFixtureReplay(t, testdata, h, fixture)
			assertHistoricalFixtureAnalyzes(t, h.Analyzer, fixture, oracle)
		})
	}
}

func configureHistoricalFixtureReplay(t *testing.T, testdata string, h analyzerHarness, fixture string) {
	t.Helper()

	switch {
	case fixture == "constructorvalidates_nocfa_ast":
		setFlag(t, h.Analyzer, "check-constructor-validates", "true")
		setFlag(t, h.Analyzer, "cfg-backend", cfgBackendAST)
	case strings.HasPrefix(fixture, "castvalidation_nocfa_"):
		setFlag(t, h.Analyzer, "check-cast-validation", "true")
		setFlag(t, h.Analyzer, "cfg-backend", cfgBackendAST)
		if fixture == "castvalidation_nocfa_suppression" {
			setFlag(t, h.Analyzer, "config", filepath.Join(testdata, "src", fixture, "goplint.toml"))
			setFlag(t, h.Analyzer, "baseline", filepath.Join(testdata, "src", fixture, "goplint-baseline.toml"))
		}
	default:
		t.Fatalf("no replay configuration for historical fixture %q", fixture)
	}
}

func assertHistoricalFixtureAnalyzes(t *testing.T, analyzer *analysis.Analyzer, fixture string, oracle semanticHistoricalMissOracle) {
	t.Helper()

	diagnostics, _, results := collectDiagnosticsForPackages(t, analyzer, fixture)
	hits := map[string]int{}
	for _, result := range results {
		if result != nil && result.Err != nil {
			t.Fatalf("fixture %q analysis error: %v", fixture, result.Err)
		}
		if result == nil || result.Pass == nil {
			continue
		}
		spansByFile := collectFunctionSpans(result.Pass)
		for _, diag := range result.Diagnostics {
			if diag.Category != oracle.Category {
				continue
			}
			symbol, ok := symbolNameForDiagnostic(result.Pass.Fset, spansByFile, diag.Pos)
			if !ok {
				continue
			}
			hits[symbol]++
		}
	}
	for _, entry := range oracle.MustReport {
		if entry.Fixture != fixture {
			t.Fatalf("historical oracle fixture mismatch: expected %q, got %q", fixture, entry.Fixture)
		}
		if hits[entry.Symbol] == 0 {
			t.Fatalf(
				"fixture %q category %q: must-report symbol %q not found (hits=%v)",
				fixture,
				oracle.Category,
				entry.Symbol,
				sortedHitSymbols(hits),
			)
		}
	}
	for _, entry := range oracle.MustNotReport {
		if entry.Fixture != fixture {
			t.Fatalf("historical oracle fixture mismatch: expected %q, got %q", fixture, entry.Fixture)
		}
		if hits[entry.Symbol] > 0 {
			t.Fatalf(
				"fixture %q category %q: must-not-report symbol %q unexpectedly reported (hits=%v)",
				fixture,
				oracle.Category,
				entry.Symbol,
				sortedHitSymbols(hits),
			)
		}
	}
	if len(diagnostics) == 0 {
		t.Fatalf("fixture %q emitted no diagnostics", fixture)
	}
}
