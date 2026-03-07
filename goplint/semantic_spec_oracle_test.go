// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestSemanticSpecOracleFixturesExist(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	fixtureRoot := filepath.Join(goplintPackageRootPath(), "testdata", "src")
	for _, oracle := range catalog.OracleMatrix {
		for _, entry := range append(oracle.MustReport, oracle.MustNotReport...) {
			fixtureDir := filepath.Join(fixtureRoot, entry.Fixture)
			info, err := os.Stat(fixtureDir)
			if err != nil {
				t.Fatalf("oracle fixture %q does not exist: %v", entry.Fixture, err)
			}
			if !info.IsDir() {
				t.Fatalf("oracle fixture %q is not a directory", entry.Fixture)
			}
			if !fixtureDefinesSymbol(t, fixtureDir, entry.Symbol) {
				t.Fatalf("oracle fixture %q does not define symbol %q", entry.Fixture, entry.Symbol)
			}
		}
	}
}

func TestSemanticSpecOracleBehavior(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	rulesByCategory := make(map[string]semanticRuleSpec, len(catalog.Rules))
	for _, rule := range catalog.Rules {
		rulesByCategory[rule.Category] = rule
	}

	for _, oracle := range catalog.OracleMatrix {
		t.Run(oracle.Category, func(t *testing.T) {
			t.Parallel()
			rule, ok := rulesByCategory[oracle.Category]
			if !ok {
				t.Fatalf("rule spec missing for oracle category %q", oracle.Category)
			}

			expectationsByFixture := map[string]oracleFixtureExpectations{}
			for _, entry := range oracle.MustReport {
				expectations := expectationsByFixture[entry.Fixture]
				if expectations.mustReport == nil {
					expectations.mustReport = map[string]struct{}{}
				}
				expectations.mustReport[entry.Symbol] = struct{}{}
				expectationsByFixture[entry.Fixture] = expectations
			}
			for _, entry := range oracle.MustNotReport {
				expectations := expectationsByFixture[entry.Fixture]
				if expectations.mustNotReport == nil {
					expectations.mustNotReport = map[string]struct{}{}
				}
				expectations.mustNotReport[entry.Symbol] = struct{}{}
				expectationsByFixture[entry.Fixture] = expectations
			}

			for fixture, expectations := range expectationsByFixture {
				hits := collectSemanticOracleCategoryHits(t, rule, oracle.Category, fixture)
				for symbol := range expectations.mustReport {
					if hits[symbol] == 0 {
						t.Fatalf("category %q fixture %q: must-report symbol %q not found (hits=%v)", oracle.Category, fixture, symbol, sortedHitSymbols(hits))
					}
				}
				for symbol := range expectations.mustNotReport {
					if hits[symbol] > 0 {
						t.Fatalf("category %q fixture %q: must-not-report symbol %q unexpectedly reported (hits=%v)", oracle.Category, fixture, symbol, sortedHitSymbols(hits))
					}
				}
			}
		})
	}
}

type oracleFixtureExpectations struct {
	mustReport    map[string]struct{}
	mustNotReport map[string]struct{}
}

type functionSpan struct {
	name  string
	start token.Pos
	end   token.Pos
}

func collectSemanticOracleCategoryHits(t *testing.T, rule semanticRuleSpec, category, fixture string) map[string]int {
	t.Helper()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	configureSemanticOracleRun(t, h.Analyzer, rule, category, fixture)

	_, _, results := collectDiagnosticsForPackages(t, h.Analyzer, fixture)
	hits := map[string]int{}
	for _, result := range results {
		if result == nil {
			continue
		}
		if result.Err != nil {
			t.Fatalf("category %q fixture %q analysis error: %v", category, fixture, result.Err)
		}
		if result.Pass == nil {
			continue
		}

		spansByFile := collectFunctionSpans(result.Pass)
		for _, diag := range result.Diagnostics {
			if diag.Category != category {
				continue
			}
			symbol, ok := symbolNameForDiagnostic(result.Pass.Fset, spansByFile, diag.Pos)
			if !ok {
				continue
			}
			hits[symbol]++
		}
	}

	return hits
}

func configureSemanticOracleRun(t *testing.T, analyzer *analysis.Analyzer, rule semanticRuleSpec, category, fixture string) {
	t.Helper()

	for _, flagName := range rule.EnabledByFlags {
		setFlag(t, analyzer, flagName, "true")
	}
	switch category {
	case CategoryUnvalidatedCastInconclusive:
		setFlag(t, analyzer, "cfg-max-states", "1")
		setFlag(t, analyzer, "cfg-inconclusive-policy", cfgInconclusivePolicyError)
	case CategoryUseBeforeValidateSameBlock, CategoryUseBeforeValidateCrossBlock:
		setFlag(t, analyzer, "check-cast-validation", "true")
		setFlag(t, analyzer, "check-use-before-validate", "true")
		setFlag(t, analyzer, "ubv-mode", ubvModeOrder)
	case CategoryUseBeforeValidateInconclusive:
		setFlag(t, analyzer, "check-cast-validation", "true")
		setFlag(t, analyzer, "check-use-before-validate", "true")
		setFlag(t, analyzer, "ubv-mode", ubvModeEscape)
		setFlag(t, analyzer, "cfg-backend", cfgBackendSSA)
		setFlag(t, analyzer, "cfg-inconclusive-policy", cfgInconclusivePolicyError)
	case CategoryMissingConstructorValidateInc:
		setFlag(t, analyzer, "cfg-max-states", "1")
		setFlag(t, analyzer, "cfg-inconclusive-policy", cfgInconclusivePolicyError)
	}

	switch fixture {
	case "cfa_cast_inconclusive", "constructorvalidates_inconclusive":
		setFlag(t, analyzer, "cfg-max-states", "1")
	case "use_before_validate_escape":
		setFlag(t, analyzer, "ubv-mode", ubvModeEscape)
		setFlag(t, analyzer, "cfg-backend", cfgBackendSSA)
	}
}

func collectFunctionSpans(pass *analysis.Pass) map[string][]functionSpan {
	spansByFile := map[string][]functionSpan{}
	for _, file := range pass.Files {
		filename := pass.Fset.Position(file.Pos()).Filename
		if filename == "" {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			fn, ok := node.(*ast.FuncDecl)
			if !ok {
				return true
			}
			spansByFile[filename] = append(spansByFile[filename], functionSpan{
				name:  fn.Name.Name,
				start: fn.Pos(),
				end:   fn.End(),
			})
			return false
		})
	}
	return spansByFile
}

func symbolNameForDiagnostic(fset *token.FileSet, spansByFile map[string][]functionSpan, pos token.Pos) (string, bool) {
	if !pos.IsValid() {
		return "", false
	}
	filename := fset.Position(pos).Filename
	if filename == "" {
		return "", false
	}
	for _, span := range spansByFile[filename] {
		if span.start <= pos && pos <= span.end {
			return span.name, true
		}
	}
	return "", false
}

func fixtureDefinesSymbol(t *testing.T, fixtureDir, symbol string) bool {
	t.Helper()
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatalf("read fixture directory %q: %v", fixtureDir, err)
	}
	target := "func " + symbol + "("
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(fixtureDir, entry.Name()))
		if readErr != nil {
			t.Fatalf("read fixture file %q: %v", entry.Name(), readErr)
		}
		if strings.Contains(string(data), target) {
			return true
		}
	}
	return false
}

func sortedHitSymbols(hits map[string]int) []string {
	symbols := make([]string, 0, len(hits))
	for symbol := range hits {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}
