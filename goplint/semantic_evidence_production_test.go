// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

type semanticProductionExecution struct {
	hits   map[string]map[string]int
	traces *semanticEvidenceTraceRecorder
}

type semanticEvidenceTraceRecorder struct {
	mu     sync.Mutex
	events []semanticEvidenceTraceEvent
}

func TestSemanticEvidenceContract(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	registrations := semanticEvidenceRegistrations(t)
	rules := semanticRulesByCategory(catalog)
	oracles := semanticOraclesByCategory(catalog)

	for _, category := range protocolEvidenceCategories() {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			t.Run(string(soundnessevidence.LayerRuleContract), func(t *testing.T) {
				t.Parallel()

				rule, hasRule := rules[category]
				oracle, hasOracle := oracles[category]
				spec, err := semanticCategoryByName(category)
				if err != nil {
					t.Fatalf("semanticCategoryByName() error: %v", err)
				}
				if !hasRule || !hasOracle || spec.Kind != semanticKindProtocol || len(oracle.MustReport) == 0 ||
					len(oracle.MustNotReport) == 0 || len(oracle.MustBeInconclusive) == 0 {
					t.Fatalf("category %q lacks an exact executable protocol contract", category)
				}
				if rule.Category != category || oracle.Category != category {
					t.Fatalf("category %q rule/oracle identity drift", category)
				}
				registration := registrations[category+"."+string(soundnessevidence.LayerRuleContract)]
				emitExecutedSemanticObservation(t, registration, []soundnessevidence.SemanticCase{
					semanticCatalogCase(
						registration,
						category+"/catalog-contract",
						[]string{"catalog-rule-matched", "exact-category-bound"},
					),
				})
			})
		})
	}
}

func TestSemanticEvidenceProduction(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	registrations := semanticEvidenceRegistrations(t)
	rules := semanticRulesByCategory(catalog)

	for _, category := range protocolEvidenceCategories() {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			oracle := semanticOraclesByCategory(catalog)[category]
			execution := executeSemanticProductionEvidence(t, rules[category], category, oracle)
			execution.traces.requiredStages(t, category)

			t.Run(string(soundnessevidence.LayerMustReport), func(t *testing.T) {
				t.Parallel()

				requireOracleEntries(t, execution, category, oracle.MustReport, true)
				registration := registrations[category+"."+string(soundnessevidence.LayerMustReport)]
				emitExecutedSemanticObservation(t, registration, semanticOracleCases(
					t,
					registration,
					execution,
					category,
					"must-report",
					oracle.MustReport,
					[]string{"diagnostic-emitted", "fixture-executed"},
				))
			})

			t.Run(string(soundnessevidence.LayerMustNotReport), func(t *testing.T) {
				t.Parallel()

				requireOracleEntries(t, execution, category, oracle.MustNotReport, false)
				registration := registrations[category+"."+string(soundnessevidence.LayerMustNotReport)]
				emitExecutedSemanticObservation(t, registration, semanticOracleCases(
					t,
					registration,
					execution,
					category,
					"must-not-report",
					oracle.MustNotReport,
					[]string{"fixture-executed", "no-diagnostic-emitted"},
				))
			})

			t.Run(string(soundnessevidence.LayerMustBeInconclusive), func(t *testing.T) {
				t.Parallel()

				inconclusiveCategory := semanticInconclusiveCategory(category)
				requireOracleEntries(
					t,
					execution,
					inconclusiveCategory,
					oracle.MustBeInconclusive,
					true,
				)
				registration := registrations[category+"."+string(soundnessevidence.LayerMustBeInconclusive)]
				emitExecutedSemanticObservation(t, registration, semanticOracleCases(
					t,
					registration,
					execution,
					inconclusiveCategory,
					"must-be-inconclusive",
					oracle.MustBeInconclusive,
					[]string{"blocking-inconclusive-emitted", "fixture-executed"},
				))
			})

			t.Run(string(soundnessevidence.LayerProduction), func(t *testing.T) {
				t.Parallel()

				requireOracleEntries(t, execution, category, oracle.MustReport, true)
				requireOracleEntries(t, execution, category, oracle.MustNotReport, false)
				requireOracleEntries(
					t,
					execution,
					semanticInconclusiveCategory(category),
					oracle.MustBeInconclusive,
					true,
				)
				registration := registrations[category+"."+string(soundnessevidence.LayerProduction)]
				cases := semanticOracleCases(
					t,
					registration,
					execution,
					category,
					"negative",
					oracle.MustReport,
					[]string{"negative-path-executed"},
				)
				cases = append(cases, semanticOracleCases(
					t,
					registration,
					execution,
					category,
					"positive",
					oracle.MustNotReport,
					[]string{"positive-path-executed"},
				)...)
				cases = append(cases, semanticOracleCases(
					t,
					registration,
					execution,
					semanticInconclusiveCategory(category),
					"uncertainty",
					oracle.MustBeInconclusive,
					[]string{"uncertainty-path-executed"},
				)...)
				slices.SortFunc(cases, func(left, right soundnessevidence.SemanticCase) int {
					return strings.Compare(left.ID, right.ID)
				})
				emitExecutedSemanticObservation(t, registration, cases)
			})
		})
	}
}

func TestProtocolProductionRoutingEvidence(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	registrations := semanticEvidenceRegistrations(t)
	rules := semanticRulesByCategory(catalog)
	oracles := semanticOraclesByCategory(catalog)

	for _, category := range protocolEvidenceCategories() {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			execution := executeSemanticProductionEvidence(t, rules[category], category, oracles[category])
			execution.traces.requiredStages(t, category)
			registration := registrations[category+"."+string(soundnessevidence.LayerOwnerRoute)]
			routeIDs := execution.traces.routeCaseIDs(t, category)
			cases := make([]soundnessevidence.SemanticCase, 0, len(routeIDs))
			for _, caseID := range routeIDs {
				fixture, _, _ := strings.Cut(caseID, "/route/")
				cases = append(cases, soundnessevidence.SemanticCase{
					ID:        caseID,
					Category:  registration.Category,
					Layer:     registration.Layer,
					FeatureID: registration.FeatureID,
					Boundary:  registration.Boundary,
					ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
						soundnessevidence.BoundaryProductionAnalyzer,
					),
					Outcome:    registration.Expected.Outcome,
					Stages:     execution.traces.stagesForCase(t, category, fixture),
					Properties: []string{"category-owner-executed", "production-route-executed"},
					Dimensions: []string{registration.FeatureID},
				})
			}
			emitExecutedSemanticObservation(t, registration, cases)
		})
	}
}

func TestCategoryMutationGuardUnvalidatedCast(t *testing.T) {
	t.Parallel()

	runCategoryMutationGuard(t, CategoryUnvalidatedCast)
}

func TestCategoryMutationGuardUnvalidatedCastInconclusive(t *testing.T) {
	t.Parallel()

	runCategoryMutationGuard(t, CategoryUnvalidatedCastInconclusive)
}

func TestCategoryMutationGuardMissingConstructorValidate(t *testing.T) {
	t.Parallel()

	runCategoryMutationGuard(t, CategoryMissingConstructorValidate)
}

func TestCategoryMutationGuardMissingConstructorValidateInconclusive(t *testing.T) {
	t.Parallel()

	runCategoryMutationGuard(t, CategoryMissingConstructorValidateInc)
}

func TestCategoryMutationGuardUseBeforeValidateSameBlock(t *testing.T) {
	t.Parallel()

	runCategoryMutationGuard(t, CategoryUseBeforeValidateSameBlock)
}

func TestCategoryMutationGuardUseBeforeValidateCrossBlock(t *testing.T) {
	t.Parallel()

	runCategoryMutationGuard(t, CategoryUseBeforeValidateCrossBlock)
}

func TestCategoryMutationGuardUseBeforeValidateInconclusive(t *testing.T) {
	t.Parallel()

	runCategoryMutationGuard(t, CategoryUseBeforeValidateInconclusive)
}

func TestCategoryMutationGuardUnvalidatedBoundaryRequest(t *testing.T) {
	t.Parallel()

	runCategoryMutationGuard(t, CategoryUnvalidatedBoundaryRequest)
}

func runCategoryMutationGuard(t *testing.T, category string) {
	t.Helper()

	catalog := mustLoadSemanticRuleCatalog(t)
	rule := semanticRulesByCategory(catalog)[category]
	oracle := semanticOraclesByCategory(catalog)[category]
	execution := executeSemanticProductionEvidence(t, rule, category, oracle)
	requireCategoryMutationGuardDiagnostic(t, execution, category, oracle.MustReport)
	requireOracleEntries(t, execution, category, oracle.MustReport, true)
	requireOracleEntries(t, execution, category, oracle.MustNotReport, false)
	requireOracleEntries(
		t,
		execution,
		semanticInconclusiveCategory(category),
		oracle.MustBeInconclusive,
		true,
	)
	execution.traces.requiredStages(t, category)
}

func requireCategoryMutationGuardDiagnostic(
	t *testing.T,
	execution semanticProductionExecution,
	category string,
	entries []semanticOracleEntry,
) {
	t.Helper()

	if len(entries) == 0 {
		t.Fatalf("category %q has no must-report mutation guard entry", category)
	}
	entry := entries[0]
	state := "absent"
	if execution.hits[category][entry.Fixture+"\x00"+entry.Symbol] > 0 {
		state = "present"
	}
	subject := "diagnostic-category/" + category
	requireMutationGuardObservation(
		t,
		"category-report/"+category,
		mutationGuardState(subject, "present"),
		mutationGuardState(subject, state),
	)
}

func executeSemanticProductionEvidence(
	t *testing.T,
	rule semanticRuleSpec,
	category string,
	oracle semanticOracleSpec,
) semanticProductionExecution {
	t.Helper()

	execution := semanticProductionExecution{
		hits:   make(map[string]map[string]int),
		traces: &semanticEvidenceTraceRecorder{},
	}
	fixtures := semanticOracleFixtures(oracle)
	for _, fixture := range fixtures {
		harness := newAnalyzerHarness()
		resetFlags(t, harness)
		configureSemanticOracleRun(t, harness.Analyzer, rule, category, fixture)
		harness.state.semanticEvidenceObserver = func(event semanticEvidenceTraceEvent) {
			event.CaseID = fixture
			execution.traces.observe(event)
		}
		_, _, results := collectDiagnosticsForPackages(t, harness.Analyzer, fixture)
		for _, result := range results {
			if result == nil || result.Pass == nil {
				continue
			}
			if result.Err != nil {
				t.Fatalf("category %q fixture %q analysis error: %v", category, fixture, result.Err)
			}
			spansByFile := collectFunctionSpans(result.Pass)
			for _, diagnostic := range result.Diagnostics {
				symbol, ok := symbolNameForDiagnostic(result.Pass.Fset, spansByFile, diagnostic.Pos)
				if !ok {
					continue
				}
				if execution.hits[diagnostic.Category] == nil {
					execution.hits[diagnostic.Category] = make(map[string]int)
				}
				execution.hits[diagnostic.Category][fixture+"\x00"+symbol]++
			}
		}
	}
	return execution
}

func (recorder *semanticEvidenceTraceRecorder) observe(event semanticEvidenceTraceEvent) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.events = append(recorder.events, event)
}

func (recorder *semanticEvidenceTraceRecorder) requiredStages(t *testing.T, category string) []soundnessevidence.ExecutionStage {
	t.Helper()

	featureID, owner, route := semanticEvidenceRouteIdentity(t, category)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	seen := make(map[string]bool)
	for _, event := range recorder.events {
		if event.FeatureID == featureID && event.Owner == owner && event.Route == route {
			seen[event.Stage] = true
		}
	}
	stages := make([]soundnessevidence.ExecutionStage, 0, 2)
	for _, stage := range []string{semanticEvidenceStageSourceExtraction, semanticEvidenceStageReporting} {
		if !seen[stage] {
			t.Fatalf("category %q production trace did not execute stage %q", category, stage)
		}
		stages = append(stages, soundnessevidence.ExecutionStage(stage))
	}
	return stages
}

func (recorder *semanticEvidenceTraceRecorder) stagesForCase(
	t *testing.T,
	category,
	caseID string,
) []soundnessevidence.ExecutionStage {
	t.Helper()

	featureID, owner, route := semanticEvidenceRouteIdentity(t, category)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	seen := make(map[string]bool)
	for _, event := range recorder.events {
		if event.CaseID == caseID && event.FeatureID == featureID && event.Owner == owner && event.Route == route {
			seen[event.Stage] = true
		}
	}
	stages := make([]soundnessevidence.ExecutionStage, 0, 2)
	for _, stage := range []string{semanticEvidenceStageSourceExtraction, semanticEvidenceStageReporting} {
		if !seen[stage] {
			t.Fatalf("category %q case %q production trace did not execute stage %q", category, caseID, stage)
		}
		stages = append(stages, soundnessevidence.ExecutionStage(stage))
	}
	return stages
}

func (recorder *semanticEvidenceTraceRecorder) routeCaseIDs(t *testing.T, category string) []string {
	t.Helper()

	featureID, owner, route := semanticEvidenceRouteIdentity(t, category)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	caseCounts := make(map[string]int)
	caseIDs := make([]string, 0)
	for _, event := range recorder.events {
		if event.FeatureID != featureID || event.Owner != owner || event.Route != route ||
			event.Stage != semanticEvidenceStageReporting {
			continue
		}
		caseCounts[event.CaseID]++
		caseIDs = append(caseIDs, fmt.Sprintf("%s/route/%06d", event.CaseID, caseCounts[event.CaseID]))
	}
	if len(caseIDs) == 0 {
		t.Fatalf("category %q production owner route did not complete", category)
	}
	slices.Sort(caseIDs)
	return caseIDs
}

func semanticEvidenceRouteIdentity(t *testing.T, category string) (string, semanticOwnerKey, semanticProductionRoute) {
	t.Helper()

	spec, err := semanticCategoryByName(category)
	if err != nil {
		t.Fatalf("semanticCategoryByName() error: %v", err)
	}
	for _, owner := range semanticOwnerRegistry() {
		if owner.Key == spec.Owner {
			return semanticFeatureForCategory(t, category), owner.Key, owner.Route
		}
	}
	t.Fatalf("category %q owner %q has no production route", category, spec.Owner)
	return "", "", ""
}

func requireOracleEntries(
	t *testing.T,
	execution semanticProductionExecution,
	category string,
	entries []semanticOracleEntry,
	wantDiagnostic bool,
) int {
	t.Helper()

	diagnostics := 0
	for _, entry := range entries {
		count := execution.hits[category][entry.Fixture+"\x00"+entry.Symbol]
		if wantDiagnostic && count == 0 {
			t.Fatalf("category %q fixture %q symbol %q produced no diagnostic", category, entry.Fixture, entry.Symbol)
		}
		if !wantDiagnostic && count != 0 {
			t.Fatalf("category %q fixture %q symbol %q produced %d diagnostics", category, entry.Fixture, entry.Symbol, count)
		}
		diagnostics += count
	}
	return diagnostics
}

func semanticEvidenceRegistrations(t *testing.T) map[string]soundnessevidence.Registration {
	t.Helper()

	registry, err := soundnessevidence.LoadRegistry(
		t.Context(),
		filepath.Join(goplintModuleRootPath(), "spec", "semantic-evidence.v2.json"),
	)
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	registrations := make(map[string]soundnessevidence.Registration, len(registry.Registrations))
	for _, registration := range registry.Registrations {
		registrations[registration.ID] = registration
	}
	return registrations
}

func emitExecutedSemanticObservation(
	t *testing.T,
	registration soundnessevidence.Registration,
	cases []soundnessevidence.SemanticCase,
) {
	t.Helper()

	if subgateID := os.Getenv(soundnessevidence.EnvSubgateID); subgateID != "" && subgateID != registration.ProducerID {
		return
	}
	emitSemanticObservation(t, semanticObservationFromCases(
		t,
		registration.ID,
		registration.ProducerID,
		registration.TestID,
		cases,
	))
}

func semanticCatalogCase(
	registration soundnessevidence.Registration,
	caseID string,
	properties []string,
) soundnessevidence.SemanticCase {
	return soundnessevidence.SemanticCase{
		ID:        caseID,
		Category:  registration.Category,
		Layer:     registration.Layer,
		FeatureID: registration.FeatureID,
		Boundary:  registration.Boundary,
		ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
			soundnessevidence.BoundaryCatalogValidation,
		),
		Outcome:    registration.Expected.Outcome,
		Properties: slices.Clone(properties),
		Dimensions: []string{registration.FeatureID},
	}
}

func semanticOracleCases(
	t *testing.T,
	registration soundnessevidence.Registration,
	execution semanticProductionExecution,
	diagnosticCategory,
	idPrefix string,
	entries []semanticOracleEntry,
	properties []string,
) []soundnessevidence.SemanticCase {
	t.Helper()

	cases := make([]soundnessevidence.SemanticCase, 0, len(entries))
	for _, entry := range entries {
		caseKey := entry.Fixture + "\x00" + entry.Symbol
		cases = append(cases, soundnessevidence.SemanticCase{
			ID:        idPrefix + "/" + entry.Fixture + "/" + entry.Symbol,
			Category:  registration.Category,
			Layer:     registration.Layer,
			FeatureID: registration.FeatureID,
			Boundary:  registration.Boundary,
			ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
				soundnessevidence.BoundaryProductionAnalyzer,
			),
			Outcome:         registration.Expected.Outcome,
			DiagnosticCount: execution.hits[diagnosticCategory][caseKey],
			Stages: execution.traces.stagesForCase(
				t,
				registration.Category,
				entry.Fixture,
			),
			Properties: slices.Clone(properties),
			Dimensions: []string{registration.FeatureID},
		})
	}
	slices.SortFunc(cases, func(left, right soundnessevidence.SemanticCase) int {
		return strings.Compare(left.ID, right.ID)
	})
	return cases
}

func semanticRulesByCategory(catalog semanticRuleCatalog) map[string]semanticRuleSpec {
	rules := make(map[string]semanticRuleSpec, len(catalog.Rules))
	for _, rule := range catalog.Rules {
		rules[rule.Category] = rule
	}
	return rules
}

func semanticOraclesByCategory(catalog semanticRuleCatalog) map[string]semanticOracleSpec {
	oracles := make(map[string]semanticOracleSpec, len(catalog.OracleMatrix))
	for _, oracle := range catalog.OracleMatrix {
		oracles[oracle.Category] = oracle
	}
	return oracles
}

func semanticOracleFixtures(oracle semanticOracleSpec) []string {
	seen := make(map[string]bool)
	for _, entry := range append(append(slices.Clone(oracle.MustReport), oracle.MustNotReport...), oracle.MustBeInconclusive...) {
		seen[entry.Fixture] = true
	}
	fixtures := make([]string, 0, len(seen))
	for fixture := range seen {
		fixtures = append(fixtures, fixture)
	}
	slices.Sort(fixtures)
	return fixtures
}

func protocolEvidenceCategories() []string {
	return []string{
		CategoryMissingConstructorValidateInc,
		CategoryMissingConstructorValidate,
		CategoryUnvalidatedBoundaryRequest,
		CategoryUnvalidatedCastInconclusive,
		CategoryUnvalidatedCast,
		CategoryUseBeforeValidateCrossBlock,
		CategoryUseBeforeValidateInconclusive,
		CategoryUseBeforeValidateSameBlock,
	}
}

func semanticFeatureForCategory(t *testing.T, category string) string {
	t.Helper()

	switch category {
	case CategoryUnvalidatedCast, CategoryUnvalidatedCastInconclusive:
		return semanticFeatureCastValidation
	case CategoryMissingConstructorValidate, CategoryMissingConstructorValidateInc:
		return semanticFeatureConstructorValidation
	case CategoryUseBeforeValidateSameBlock, CategoryUseBeforeValidateCrossBlock, CategoryUseBeforeValidateInconclusive:
		return semanticFeatureUseBeforeValidation
	case CategoryUnvalidatedBoundaryRequest:
		return semanticFeatureBoundaryRequest
	default:
		t.Fatalf("category %q has no semantic evidence feature", category)
		return ""
	}
}
