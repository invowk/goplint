// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestSemanticSpecOracleFixturesExist(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	fixtureRoot := filepath.Join(goplintPackageRootPath(), "testdata", "src")
	for _, oracle := range catalog.OracleMatrix {
		entries := append(slices.Clone(oracle.MustReport), oracle.MustNotReport...)
		entries = append(entries, oracle.MustBeInconclusive...)
		for _, entry := range entries {
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
	expectationsByFixture := make(map[string]map[string]oracleFixtureExpectations)
	for _, oracle := range catalog.OracleMatrix {
		if _, ok := rulesByCategory[oracle.Category]; !ok {
			t.Fatalf("rule spec missing for oracle category %q", oracle.Category)
		}
		for _, entry := range oracle.MustReport {
			if expectationsByFixture[entry.Fixture] == nil {
				expectationsByFixture[entry.Fixture] = make(map[string]oracleFixtureExpectations)
			}
			expectations := expectationsByFixture[entry.Fixture][oracle.Category]
			if expectations.mustReport == nil {
				expectations.mustReport = map[string]struct{}{}
			}
			expectations.mustReport[entry.Symbol] = struct{}{}
			expectationsByFixture[entry.Fixture][oracle.Category] = expectations
		}
		for _, entry := range oracle.MustNotReport {
			if expectationsByFixture[entry.Fixture] == nil {
				expectationsByFixture[entry.Fixture] = make(map[string]oracleFixtureExpectations)
			}
			expectations := expectationsByFixture[entry.Fixture][oracle.Category]
			if expectations.mustNotReport == nil {
				expectations.mustNotReport = map[string]struct{}{}
			}
			expectations.mustNotReport[entry.Symbol] = struct{}{}
			expectationsByFixture[entry.Fixture][oracle.Category] = expectations
		}
		for _, entry := range oracle.MustBeInconclusive {
			if expectationsByFixture[entry.Fixture] == nil {
				expectationsByFixture[entry.Fixture] = make(map[string]oracleFixtureExpectations)
			}
			expectations := expectationsByFixture[entry.Fixture][oracle.Category]
			if expectations.mustBeInconclusive == nil {
				expectations.mustBeInconclusive = map[string]struct{}{}
			}
			expectations.mustBeInconclusive[entry.Symbol] = struct{}{}
			expectationsByFixture[entry.Fixture][oracle.Category] = expectations
		}
	}

	fixtures := make([]string, 0, len(expectationsByFixture))
	for fixture := range expectationsByFixture {
		fixtures = append(fixtures, fixture)
	}
	slices.Sort(fixtures)
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			categoryExpectations := expectationsByFixture[fixture]
			categories := make([]string, 0, len(categoryExpectations))
			for category := range categoryExpectations {
				categories = append(categories, category)
			}
			slices.Sort(categories)
			hitsByCategory := collectSemanticOracleFixtureHits(t, rulesByCategory, categories, fixture)
			for _, category := range categories {
				expectations := categoryExpectations[category]
				hits := hitsByCategory[category]
				for symbol := range expectations.mustReport {
					if hits[symbol] == 0 {
						t.Fatalf("category %q fixture %q: must-report symbol %q not found (hits=%v)", category, fixture, symbol, sortedHitSymbols(hits))
					}
				}
				for symbol := range expectations.mustNotReport {
					if hits[symbol] > 0 {
						t.Fatalf("category %q fixture %q: must-not-report symbol %q unexpectedly reported (hits=%v)", category, fixture, symbol, sortedHitSymbols(hits))
					}
				}
				inconclusiveCategory := semanticInconclusiveCategory(category)
				inconclusiveHits := hitsByCategory[inconclusiveCategory]
				for symbol := range expectations.mustBeInconclusive {
					if inconclusiveHits[symbol] == 0 {
						t.Fatalf("category %q fixture %q: must-be-inconclusive symbol %q not found in %q (hits=%v)", category, fixture, symbol, inconclusiveCategory, sortedHitSymbols(inconclusiveHits))
					}
				}
			}
		})
	}
}

func collectSemanticOracleFixtureHits(
	t *testing.T,
	rulesByCategory map[string]semanticRuleSpec,
	categories []string,
	fixture string,
) map[string]map[string]int {
	t.Helper()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	for _, category := range categories {
		configureSemanticOracleRun(t, h.Analyzer, rulesByCategory[category], category, fixture)
	}

	_, _, results := collectDiagnosticsForPackages(t, h.Analyzer, fixture)
	hitsByCategory := make(map[string]map[string]int)
	for _, result := range results {
		if result == nil {
			continue
		}
		if result.Err != nil {
			t.Fatalf("fixture %q analysis error: %v", fixture, result.Err)
		}
		if result.Pass == nil {
			continue
		}

		spansByFile := collectFunctionSpans(result.Pass)
		for _, diag := range result.Diagnostics {
			symbol, ok := symbolNameForDiagnostic(result.Pass.Fset, spansByFile, diag.Pos)
			if !ok {
				continue
			}
			if hitsByCategory[diag.Category] == nil {
				hitsByCategory[diag.Category] = make(map[string]int)
			}
			hitsByCategory[diag.Category][symbol]++
		}
	}

	return hitsByCategory
}

func TestSemanticSpecHistoricalMissOracleBehavior(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	rulesByCategory := make(map[string]semanticRuleSpec, len(catalog.Rules))
	for _, rule := range catalog.Rules {
		rulesByCategory[rule.Category] = rule
	}
	for _, oracle := range catalog.HistoricalMissOracles {
		t.Run(oracle.Fixture, func(t *testing.T) {
			t.Parallel()
			rule, ok := rulesByCategory[oracle.Category]
			if !ok {
				t.Fatalf("historical oracle category %q has no rule", oracle.Category)
			}
			hits := collectSemanticOracleCategoryHits(t, rule, oracle.Category, oracle.Fixture)
			for _, entry := range oracle.MustReport {
				if hits[entry.Symbol] == 0 {
					t.Fatalf("historical fixture %q must report %q for %q (hits=%v)", oracle.Fixture, entry.Symbol, oracle.Category, sortedHitSymbols(hits))
				}
			}
			for _, entry := range oracle.MustNotReport {
				if hits[entry.Symbol] > 0 {
					t.Fatalf("historical fixture %q must not report %q for %q (hits=%v)", oracle.Fixture, entry.Symbol, oracle.Category, sortedHitSymbols(hits))
				}
			}
		})
	}
}

type oracleFixtureExpectations struct {
	mustReport         map[string]struct{}
	mustNotReport      map[string]struct{}
	mustBeInconclusive map[string]struct{}
}

type symbolSpan struct {
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

	// Keep production diagnostics scoped to the oracle fixture. Imported
	// packages still execute the analyzer's fact-export-only path, so
	// cross-package protocol evidence remains available without recursively
	// auditing unrelated standard-library and support dependencies.
	setFlag(t, analyzer, "include-packages", fixture)

	for _, flagName := range rule.EnabledByFlags {
		// Primitive and directive validation are always-on analyzer behavior.
		// Their catalog entry records check-all as a public enabling surface,
		// but setting it here would activate every independent semantic family
		// and turn a focused boundary oracle into a full analyzer-system run.
		if flagName == "check-all" {
			continue
		}
		setFlag(t, analyzer, flagName, "true")
	}
	switch category {
	case CategoryUseBeforeValidateSameBlock, CategoryUseBeforeValidateCrossBlock:
		setFlag(t, analyzer, "check-cast-validation", "true")
		setFlag(t, analyzer, "check-use-before-validate", "true")
	case CategoryUseBeforeValidateInconclusive:
		setFlag(t, analyzer, "check-cast-validation", "true")
		setFlag(t, analyzer, "check-use-before-validate", "true")
	}

	switch fixture {
	case "cfa_cast_inconclusive", "constructorvalidates_inconclusive":
		setFlag(t, analyzer, "cfg-max-states", "1")
	case "auditexceptions":
		fixtureDir := filepath.Join(goplintPackageRootPath(), "testdata", "src", fixture)
		setFlag(t, analyzer, "config", filepath.Join(fixtureDir, "goplint.toml"))
	case "auditreviewdates":
		fixtureDir := filepath.Join(goplintPackageRootPath(), "testdata", "src", fixture)
		setFlag(t, analyzer, "config", filepath.Join(fixtureDir, "config.toml"))
	case "castvalidation_suppression":
		fixtureDir := filepath.Join(goplintPackageRootPath(), "testdata", "src", fixture)
		setFlag(t, analyzer, "config", filepath.Join(fixtureDir, "goplint.toml"))
		setFlag(t, analyzer, "baseline", filepath.Join(fixtureDir, "goplint-baseline.toml"))
	case "use_before_validate_escape":
	}
}

func collectFunctionSpans(pass *analysis.Pass) map[string][]symbolSpan {
	spansByFile := map[string][]symbolSpan{}
	for _, file := range pass.Files {
		filename := pass.Fset.Position(file.Pos()).Filename
		if filename == "" {
			continue
		}
		spansByFile[filename] = append(spansByFile[filename], symbolSpan{name: "package", start: file.Pos(), end: file.End()})
		ast.Inspect(file, func(node ast.Node) bool {
			var name string
			switch declaration := node.(type) {
			case *ast.FuncDecl:
				name = declaration.Name.Name
			case *ast.TypeSpec:
				name = declaration.Name.Name
			default:
				return true
			}
			spansByFile[filename] = append(spansByFile[filename], symbolSpan{name: name, start: node.Pos(), end: node.End()})
			return true
		})
		slices.SortFunc(spansByFile[filename], func(left, right symbolSpan) int {
			return cmp.Compare(int(left.end-left.start), int(right.end-right.start))
		})
	}
	return spansByFile
}

func symbolNameForDiagnostic(fset *token.FileSet, spansByFile map[string][]symbolSpan, pos token.Pos) (string, bool) {
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
	if symbol == "package" {
		return true
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(fixtureDir, entry.Name()))
		if readErr != nil {
			t.Fatalf("read fixture file %q: %v", entry.Name(), readErr)
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), entry.Name(), data, 0)
		if parseErr != nil {
			t.Fatalf("parse fixture file %q: %v", entry.Name(), parseErr)
		}
		defined := false
		ast.Inspect(file, func(node ast.Node) bool {
			switch declaration := node.(type) {
			case *ast.FuncDecl:
				defined = defined || declaration.Name.Name == symbol
			case *ast.TypeSpec:
				defined = defined || declaration.Name.Name == symbol
			}
			return !defined
		})
		if defined {
			return true
		}
	}
	return false
}

func semanticInconclusiveCategory(category string) string {
	switch category {
	case CategoryUnvalidatedCast:
		return CategoryUnvalidatedCastInconclusive
	case CategoryUseBeforeValidateSameBlock, CategoryUseBeforeValidateCrossBlock:
		return CategoryUseBeforeValidateInconclusive
	case CategoryMissingConstructorValidate:
		return CategoryMissingConstructorValidateInc
	default:
		return category
	}
}

func sortedHitSymbols(hits map[string]int) []string {
	symbols := make([]string, 0, len(hits))
	for symbol := range hits {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}
