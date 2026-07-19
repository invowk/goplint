// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

var nonCastEvidenceCategories = []string{
	CategoryMissingConstructorValidateInc,
	CategoryMissingConstructorValidate,
	CategoryUnvalidatedBoundaryRequest,
	CategoryUseBeforeValidateCrossBlock,
	CategoryUseBeforeValidateInconclusive,
	CategoryUseBeforeValidateSameBlock,
}

type nonCastCategoryExecution struct {
	production  semanticProductionExecution
	trace       generatedExecutionTrace
	cases       int
	diagnostics int
	aliases     int
	callSites   int
	constraints int
	facts       int
	returnEdges int
}

type nonCastMetamorphicRelation struct {
	id              string
	base            string
	transformed     string
	wantDiagnostics int
}

type focusedCategorySourceResult struct {
	diagnostics int
	trace       generatedExecutionTrace
}

func nonCastMetamorphicRelationForCategory(
	t testing.TB,
	category string,
) nonCastMetamorphicRelation {
	t.Helper()

	const constructorBase = `package testpkg
type Value struct { name string }
func (v *Value) Validate() error { return nil }
func NewValue(raw string) (*Value, error) {
	value := &Value{name: raw}
	return value, nil
}`
	const constructorTransformed = `package testpkg
const unrelated = "semantic-preserving"
type Value struct { name string }
func (v *Value) Validate() error { return nil }
func NewValue(raw string) (*Value, error) {
	candidate := &Value{name: raw}
	return candidate, nil
}`
	const constructorInconclusiveBase = `package testpkg
type Value struct { name string }
func (v *Value) Validate() error { return nil }
func NewValue(raw string) (*Value, error) {
	value := &Value{name: raw}
	if err := value.Validate(); err != nil { return nil, err }
	return value, nil
}`
	const constructorInconclusiveTransformed = `package testpkg
const unrelated = "semantic-preserving"
type Value struct { name string }
func (v *Value) Validate() error { return nil }
func NewValue(raw string) (*Value, error) {
	candidate := &Value{name: raw}
	if err := candidate.Validate(); err != nil { return nil, err }
	return candidate, nil
}`
	const boundaryBase = `package testpkg
type Request struct { Name string }
func (r Request) Validate() error { return nil }
func Handle(req Request) error {
	_ = req.Name
	return nil
}`
	const boundaryTransformed = `package testpkg
const unrelated = "semantic-preserving"
type Request struct { Name string }
func (r Request) Validate() error { return nil }
func Handle(request Request) error {
	_ = request.Name
	return nil
}`
	const sameBlockBase = `package testpkg
type Value string
func (v Value) Validate() error { return nil }
func consume(Value) {}
func Probe(raw string) error {
	value := Value(raw)
	consume(value)
	return value.Validate()
}`
	const sameBlockTransformed = `package testpkg
const unrelated = "semantic-preserving"
type Value string
func (v Value) Validate() error { return nil }
func consume(Value) {}
func Probe(raw string) error {
	candidate := Value(raw)
	consume(candidate)
	return candidate.Validate()
}`
	const crossBlockBase = `package testpkg
type Value string
func (v Value) Validate() error { return nil }
func consume(Value) {}
func Probe(raw string, use bool) error {
	value := Value(raw)
	if use { consume(value) }
	return value.Validate()
}`
	const crossBlockTransformed = `package testpkg
const unrelated = "semantic-preserving"
type Value string
func (v Value) Validate() error { return nil }
func consume(Value) {}
func Probe(raw string, use bool) error {
	candidate := Value(raw)
	if use { consume(candidate) }
	return candidate.Validate()
}`
	const inconclusiveBase = `package testpkg
type Value string
func (v Value) Validate() error { return nil }
type consumer interface { Consume(Value) }
func Probe(raw string, effect consumer) error {
	value := Value(raw)
	effect.Consume(value)
	return value.Validate()
}`
	const inconclusiveTransformed = `package testpkg
const unrelated = "semantic-preserving"
type Value string
func (v Value) Validate() error { return nil }
type consumer interface { Consume(Value) }
func Probe(raw string, effect consumer) error {
	candidate := Value(raw)
	effect.Consume(candidate)
	return candidate.Validate()
}`

	relation := nonCastMetamorphicRelation{
		id:              "alpha-renaming-and-unrelated-declaration-preserve-outcome",
		wantDiagnostics: 1,
	}
	switch category {
	case CategoryMissingConstructorValidate:
		relation.base, relation.transformed = constructorBase, constructorTransformed
	case CategoryMissingConstructorValidateInc:
		relation.base, relation.transformed = constructorInconclusiveBase, constructorInconclusiveTransformed
	case CategoryUnvalidatedBoundaryRequest:
		relation.base, relation.transformed = boundaryBase, boundaryTransformed
	case CategoryUseBeforeValidateSameBlock:
		relation.base, relation.transformed = sameBlockBase, sameBlockTransformed
	case CategoryUseBeforeValidateCrossBlock:
		relation.base, relation.transformed = crossBlockBase, crossBlockTransformed
	case CategoryUseBeforeValidateInconclusive:
		relation.base, relation.transformed = inconclusiveBase, inconclusiveTransformed
	default:
		t.Fatalf("category %q has no non-cast metamorphic relation", category)
	}
	return relation
}

func runFocusedCategorySourceAnalyzer(
	t *testing.T,
	category,
	source string,
) focusedCategorySourceResult {
	t.Helper()

	pass, _ := buildTypedPassFromSource(t, source)
	harness := newAnalyzerHarness()
	resetFlags(t, harness)
	rule := semanticRulesByCategory(mustLoadSemanticRuleCatalog(t))[category]
	for _, flagName := range rule.EnabledByFlags {
		if flagName != "check-all" {
			setFlag(t, harness.Analyzer, flagName, "true")
		}
	}
	switch category {
	case CategoryUseBeforeValidateSameBlock, CategoryUseBeforeValidateCrossBlock, CategoryUseBeforeValidateInconclusive:
		setFlag(t, harness.Analyzer, "check-cast-validation", "true")
		setFlag(t, harness.Analyzer, "check-use-before-validate", "true")
	case CategoryMissingConstructorValidateInc:
		setFlag(t, harness.Analyzer, "cfg-max-states", "1")
	}

	result := focusedCategorySourceResult{}
	featureID := semanticFeatureForCategory(t, category)
	harness.state.semanticEvidenceObserver = func(event semanticEvidenceTraceEvent) {
		if event.FeatureID == featureID {
			result.trace.addStage(soundnessevidence.ExecutionStage(event.Stage))
		}
	}
	pass.Analyzer = harness.Analyzer
	pass.ResultOf = map[*analysis.Analyzer]any{inspect.Analyzer: inspector.New(pass.Files)}
	pass.Report = func(diagnostic analysis.Diagnostic) {
		if diagnostic.Category == category {
			result.diagnostics++
		}
	}
	pass.ImportObjectFact = func(types.Object, analysis.Fact) bool { return false }
	pass.ImportPackageFact = func(*types.Package, analysis.Fact) bool { return false }
	pass.ExportObjectFact = func(types.Object, analysis.Fact) {}
	pass.ExportPackageFact = func(analysis.Fact) {}
	pass.AllPackageFacts = func() []analysis.PackageFact { return nil }
	pass.AllObjectFacts = func() []analysis.ObjectFact { return nil }
	if _, err := harness.Analyzer.Run(pass); err != nil {
		t.Fatalf("run category %q source analyzer: %v", category, err)
	}
	result.trace.addProperties("production-analyzer-executed")
	return result
}

func runCategoryGeneratedNonCastEvidence(t *testing.T) {
	t.Helper()

	for _, category := range nonCastEvidenceCategories {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			execution := executeNonCastCategoryEvidence(t, category, false)
			oracle := semanticOraclesByCategory(mustLoadSemanticRuleCatalog(t))[category]
			featureID := semanticFeatureForCategory(t, category)
			properties := []string{"boundary-oracle-compared", "production-analyzer-executed"}
			dimensions := sortedStringsTest(featureID, "oracle-outcomes")
			cases := nonCastOracleSemanticCases(
				t,
				category,
				soundnessevidence.LayerGenerated,
				soundnessevidence.BoundaryIndependentOracle,
				soundnessevidence.OutcomeModelAgrees,
				execution,
				category,
				"must-report",
				oracle.MustReport,
				properties,
				dimensions,
			)
			cases = append(cases, nonCastOracleSemanticCases(
				t,
				category,
				soundnessevidence.LayerGenerated,
				soundnessevidence.BoundaryIndependentOracle,
				soundnessevidence.OutcomeModelAgrees,
				execution,
				category,
				"must-not-report",
				oracle.MustNotReport,
				properties,
				dimensions,
			)...)
			cases = append(cases, nonCastOracleSemanticCases(
				t,
				category,
				soundnessevidence.LayerGenerated,
				soundnessevidence.BoundaryIndependentOracle,
				soundnessevidence.OutcomeModelAgrees,
				execution,
				semanticInconclusiveCategory(category),
				"must-be-inconclusive",
				oracle.MustBeInconclusive,
				properties,
				dimensions,
			)...)
			slices.SortFunc(cases, func(left, right soundnessevidence.SemanticCase) int {
				return strings.Compare(left.ID, right.ID)
			})
			emitSemanticObservation(t, semanticObservationFromCases(
				t,
				category+"."+string(soundnessevidence.LayerGenerated),
				"",
				t.Name(),
				cases,
			))
		})
	}
}

func runCategoryMetamorphicNonCastEvidence(t *testing.T) {
	t.Helper()

	for _, category := range nonCastEvidenceCategories {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			relation := nonCastMetamorphicRelationForCategory(t, category)
			base := runFocusedCategorySourceAnalyzer(t, category, relation.base)
			transformed := runFocusedCategorySourceAnalyzer(t, category, relation.transformed)
			if base.diagnostics != relation.wantDiagnostics || transformed.diagnostics != relation.wantDiagnostics {
				t.Fatalf(
					"category %q relation %q diagnostics = %d/%d, want %d/%d",
					category,
					relation.id,
					base.diagnostics,
					transformed.diagnostics,
					relation.wantDiagnostics,
					relation.wantDiagnostics,
				)
			}
			trace := base.trace
			trace.merge(transformed.trace)
			trace.addProperties("production-analyzer-executed", "relation-checked")
			trace.addDimensions(semanticFeatureForCategory(t, category), "relations")
			requireGeneratedTrace(
				t,
				trace,
				[]soundnessevidence.ExecutionStage{
					soundnessevidence.StageSourceExtraction,
					soundnessevidence.StageReporting,
				},
				[]string{"production-analyzer-executed", "relation-checked"},
				sortedStringsTest(semanticFeatureForCategory(t, category), "relations"),
			)
			caseRecord := soundnessevidence.SemanticCase{
				ID:        relation.id,
				Category:  category,
				Layer:     soundnessevidence.LayerMetamorphic,
				FeatureID: semanticFeatureForCategory(t, category),
				Boundary:  soundnessevidence.BoundaryMetamorphicAnalyzer,
				ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
					soundnessevidence.BoundaryMetamorphicAnalyzer,
					soundnessevidence.BoundaryProductionAnalyzer,
				),
				Outcome:         soundnessevidence.OutcomeRelationPreserved,
				DiagnosticCount: base.diagnostics + transformed.diagnostics,
				Stages:          trace.orderedStages(),
				Properties:      trace.sortedProperties(),
				Dimensions:      trace.sortedDimensions(),
			}
			emitSemanticObservation(t, semanticObservationFromCases(
				t,
				category+"."+string(soundnessevidence.LayerMetamorphic),
				"",
				t.Name(),
				[]soundnessevidence.SemanticCase{caseRecord},
			))
		})
	}
}

func runCategoryFuzzSeedNonCastEvidence(t *testing.T) {
	t.Helper()

	manifestPath := filepath.Join("testdata", "fuzz", "seed-coverage.v1.json")
	var manifest fuzzSeedCoverageManifest
	readStrictJSONTestFile(t, manifestPath, &manifest)
	for _, category := range nonCastEvidenceCategories {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			entry, ok := findCategoryFuzzEvidenceEntry(manifest, category)
			if !ok {
				t.Fatalf("category %q committed fuzz seed is missing", category)
			}
			decoded := validateCommittedFuzzSeedExpectation(t, fuzzSeedCategoryExpectation(entry))
			wantOutcome := string(semanticCategoryOutcomeViolation)
			if category == CategoryMissingConstructorValidateInc ||
				category == CategoryUseBeforeValidateInconclusive {
				wantOutcome = string(semanticCategoryOutcomeInconclusive)
			}
			if decoded.Category != category || decoded.Outcome != wantOutcome {
				t.Fatalf("category %q decoded mismatched seed identity: %+v", category, decoded)
			}
			execution := executeNonCastCategoryEvidence(t, category, false)
			diagnostics := execution.production.hits[category][decoded.Fixture+"\x00"+decoded.Symbol]
			if diagnostics == 0 {
				t.Fatalf(
					"decoded category seed %q/%q did not execute the matching production diagnostic",
					decoded.Fixture,
					decoded.Symbol,
				)
			}
			properties := []string{"decoded-structure-matched-category", "independent-property-checked"}
			dimensions := sortedStringsTest(
				semanticFeatureForCategory(t, category),
				"historical-counterexamples",
				"seed-structures",
			)
			caseRecord := soundnessevidence.SemanticCase{
				ID:        entry.SeedDigest,
				Category:  category,
				Layer:     soundnessevidence.LayerFuzz,
				FeatureID: semanticFeatureForCategory(t, category),
				Boundary:  soundnessevidence.BoundaryFuzzDecoder,
				ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
					soundnessevidence.BoundaryFuzzDecoder,
					soundnessevidence.BoundaryIndependentOracle,
					soundnessevidence.BoundaryProductionAnalyzer,
				),
				Outcome:         soundnessevidence.OutcomePropertyDetected,
				DiagnosticCount: diagnostics,
				Stages: execution.production.traces.stagesForCase(
					t,
					category,
					decoded.Fixture,
				),
				Properties: properties,
				Dimensions: dimensions,
			}
			emitSemanticObservation(t, semanticObservationFromCases(
				t,
				category+"."+string(soundnessevidence.LayerFuzz),
				"",
				t.Name(),
				[]soundnessevidence.SemanticCase{caseRecord},
			))
		})
	}
}

func executeNonCastCategoryEvidence(t *testing.T, category string, reverseFixtures bool) nonCastCategoryExecution {
	t.Helper()

	catalog := mustLoadSemanticRuleCatalog(t)
	rule := semanticRulesByCategory(catalog)[category]
	oracle := semanticOraclesByCategory(catalog)[category]
	fixtures := semanticOracleFixtures(oracle)
	if reverseFixtures {
		slices.Reverse(fixtures)
	}
	execution := nonCastCategoryExecution{
		production: semanticProductionExecution{
			hits:   make(map[string]map[string]int),
			traces: &semanticEvidenceTraceRecorder{},
		},
	}
	for _, fixture := range fixtures {
		harness := newAnalyzerHarness()
		resetFlags(t, harness)
		configureSemanticOracleRun(t, harness.Analyzer, rule, category, fixture)
		harness.state.semanticEvidenceObserver = func(event semanticEvidenceTraceEvent) {
			event.CaseID = fixture
			execution.production.traces.observe(event)
		}
		_, _, results := collectDiagnosticsForPackages(t, harness.Analyzer, fixture)
		for _, result := range results {
			if result == nil || result.Pass == nil {
				continue
			}
			if result.Err != nil {
				t.Fatalf("category %q fixture %q analysis error: %v", category, fixture, result.Err)
			}
			execution.trace.addStage(soundnessevidence.StageSourceExtraction)
			spansByFile := collectFunctionSpans(result.Pass)
			for _, diagnostic := range result.Diagnostics {
				symbol, ok := symbolNameForDiagnostic(result.Pass.Fset, spansByFile, diagnostic.Pos)
				if !ok {
					continue
				}
				execution.trace.addStage(soundnessevidence.StageIdentity)
				if execution.production.hits[diagnostic.Category] == nil {
					execution.production.hits[diagnostic.Category] = make(map[string]int)
				}
				execution.production.hits[diagnostic.Category][fixture+"\x00"+symbol]++
				if diagnostic.Category == category || diagnostic.Category == semanticInconclusiveCategory(category) {
					execution.trace.addStage(soundnessevidence.StageReporting)
					addGeneratedDiagnosticStages(&execution.trace, diagnostic)
					execution.observeDiagnosticEvidence(diagnostic)
				}
			}
		}
	}
	positive := requireOracleEntries(t, execution.production, category, oracle.MustReport, true)
	requireOracleEntries(t, execution.production, category, oracle.MustNotReport, false)
	uncertainty := requireOracleEntries(
		t,
		execution.production,
		semanticInconclusiveCategory(category),
		oracle.MustBeInconclusive,
		true,
	)
	execution.cases = len(oracle.MustReport) + len(oracle.MustNotReport) + len(oracle.MustBeInconclusive)
	// Each exact category/fixture/symbol/outcome tuple admitted by the
	// declarative oracle is one independently modeled protocol fact.
	execution.facts += execution.cases
	execution.diagnostics = positive + uncertainty
	if execution.cases > 0 {
		execution.trace.addStage(soundnessevidence.StageAggregation)
	}
	if execution.diagnostics > 0 {
		execution.trace.addStage(soundnessevidence.StagePropagation)
	}
	return execution
}

func (execution *nonCastCategoryExecution) observeDiagnosticEvidence(diagnostic analysis.Diagnostic) {
	if FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_refinement_ssa_subjects") != "" {
		execution.aliases++
	}
	if FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_witness_call_chain") != "" ||
		FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_tabulation_summaries") != "" {
		execution.callSites++
	}
	if FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_feasibility_engine") != "" ||
		FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_refinement_status") != "" {
		execution.constraints++
	}
	if FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_tabulation_dependencies") != "" ||
		FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_tabulation_path_edges") != "" {
		execution.facts++
	}
	if FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_witness_edges") != "" ||
		FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_witness_call_chain") != "" {
		execution.returnEdges++
	}
}

func findCategoryFuzzEvidenceEntry(
	manifest fuzzSeedCoverageManifest,
	category string,
) (fuzzSeedCategoryEntry, bool) {
	for _, entry := range manifest.CategoryEntries {
		if entry.Category == category {
			return entry, true
		}
	}
	return fuzzSeedCategoryEntry{}, false
}

func sortedStringsTest(values ...string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return result
}

func nonCastOracleSemanticCases(
	t *testing.T,
	category string,
	layer soundnessevidence.Layer,
	boundary soundnessevidence.Boundary,
	outcome soundnessevidence.Outcome,
	execution nonCastCategoryExecution,
	diagnosticCategory,
	idPrefix string,
	entries []semanticOracleEntry,
	properties,
	dimensions []string,
) []soundnessevidence.SemanticCase {
	t.Helper()

	cases := make([]soundnessevidence.SemanticCase, 0, len(entries))
	for _, entry := range entries {
		caseKey := entry.Fixture + "\x00" + entry.Symbol
		cases = append(cases, soundnessevidence.SemanticCase{
			ID:        idPrefix + "/" + entry.Fixture + "/" + entry.Symbol,
			Category:  category,
			Layer:     layer,
			FeatureID: semanticFeatureForCategory(t, category),
			Boundary:  boundary,
			ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
				boundary,
				soundnessevidence.BoundaryProductionAnalyzer,
			),
			Outcome:         outcome,
			DiagnosticCount: execution.production.hits[diagnosticCategory][caseKey],
			Stages: execution.production.traces.stagesForCase(
				t,
				category,
				entry.Fixture,
			),
			Properties: slices.Clone(properties),
			Dimensions: slices.Clone(dimensions),
		})
	}
	slices.SortFunc(cases, func(left, right soundnessevidence.SemanticCase) int {
		return strings.Compare(left.ID, right.ID)
	})
	return cases
}
