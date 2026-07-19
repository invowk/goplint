// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/protocoloracle"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

func TestCategoryGeneratedEvidence(t *testing.T) {
	t.Parallel()

	t.Run(CategoryUnvalidatedCast, func(t *testing.T) {
		t.Parallel()

		runCategoryGeneratedCastEvidence(
			t,
			CategoryUnvalidatedCast,
			"scheduled/integrated/consume/none/copy/none/sat/needs-validation",
		)
	})
	t.Run(CategoryUnvalidatedCastInconclusive, func(t *testing.T) {
		t.Parallel()

		runCategoryGeneratedCastEvidence(
			t,
			CategoryUnvalidatedCastInconclusive,
			"scheduled/integrated/validate/nil/copy/unresolved/sat/needs-validation",
		)
	})
	runCategoryGeneratedNonCastEvidence(t)
}

func TestCategoryMetamorphicEvidence(t *testing.T) {
	t.Parallel()

	t.Run(CategoryUnvalidatedCast, func(t *testing.T) {
		t.Parallel()

		base := protocolMetamorphicPrefix + `
func consume(Value) {}
func Probe(raw string) {
	value := Value(raw)
	consume(value)
}`
		transformed := protocolMetamorphicPrefix + `
func consume(Value) {}
func Probe(raw string) {
	renamed := Value(raw)
	consume(renamed)
}`
		runCategoryMetamorphicCastEvidence(
			t,
			CategoryUnvalidatedCast,
			"identifier-renaming-preserves-violation",
			base,
			transformed,
		)
	})
	t.Run(CategoryUnvalidatedCastInconclusive, func(t *testing.T) {
		t.Parallel()

		base := protocolMetamorphicPrefix + `
type mutator interface { Mutate(*Value) }
func consume(Value) {}
func Probe(raw string, effect mutator) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	effect.Mutate(&value)
	consume(value)
	return nil
}`
		transformed := strings.Replace(base, "\tvalue := Value(raw)\n", "\t_ = len(raw)\n\tvalue := Value(raw)\n", 1)
		runCategoryMetamorphicCastEvidence(
			t,
			CategoryUnvalidatedCastInconclusive,
			"unrelated-read-insertion-preserves-inconclusive",
			base,
			transformed,
		)
	})
	runCategoryMetamorphicNonCastEvidence(t)
}

func TestCategoryFuzzSeedEvidence(t *testing.T) {
	t.Parallel()

	t.Run(CategoryUnvalidatedCast, func(t *testing.T) {
		t.Parallel()

		runCategoryFuzzSeedCastEvidence(t, "unvalidated-cast.fuzz", CategoryUnvalidatedCast)
	})
	t.Run(CategoryUnvalidatedCastInconclusive, func(t *testing.T) {
		t.Parallel()

		runCategoryFuzzSeedCastEvidence(
			t,
			"unvalidated-cast-inconclusive.fuzz",
			CategoryUnvalidatedCastInconclusive,
		)
	})
	runCategoryFuzzSeedNonCastEvidence(t)
}

func runCategoryGeneratedCastEvidence(t *testing.T, category, caseID string) {
	t.Helper()

	manifest := loadProtocolOracleManifest(t)
	program := findProtocolProfileProgram(t, manifest, "scheduled", caseID)
	counts, err := compareGeneratedGoProgram(t, program, manifest.Scheduled.MaxStates, "")
	if err != nil {
		t.Fatal(err)
	}
	caseCount, diagnosticCount := generatedCategoryCounts(t, category, counts)
	wantStages := requiredIntegratedStages()
	wantProperties := []string{"independent-model-compared", "production-analyzer-executed"}
	wantDimensions := []string{"aliases", "call-sites", "cast-validation", "constraints", "facts", "return-edges"}
	requireGeneratedTrace(t, counts.Trace, wantStages, wantProperties, wantDimensions)
	wantOutcome := protocoloracle.OutcomeViolation
	if category == CategoryUnvalidatedCastInconclusive {
		wantOutcome = protocoloracle.OutcomeInconclusive
	}
	cases := make([]soundnessevidence.SemanticCase, 0, caseCount)
	for _, comparison := range counts.Cases {
		if comparison.Outcome != wantOutcome {
			continue
		}
		cases = append(cases, soundnessevidence.SemanticCase{
			ID:        fmt.Sprintf("%s/identity/%06d", comparison.CaseID, comparison.Identity),
			Category:  category,
			Layer:     soundnessevidence.LayerGenerated,
			FeatureID: "cast-validation",
			Boundary:  soundnessevidence.BoundaryProductionAnalyzer,
			ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
				soundnessevidence.BoundaryIndependentModel,
				soundnessevidence.BoundaryProductionAnalyzer,
			),
			Outcome:         soundnessevidence.OutcomeModelAgrees,
			DiagnosticCount: comparison.DiagnosticCount,
			Stages:          comparison.Trace.orderedStages(),
			Properties:      comparison.Trace.sortedProperties(),
			Dimensions:      comparison.Trace.sortedDimensions(),
		})
	}
	if len(cases) != caseCount {
		t.Fatalf("generated cast semantic cases = %d, want %d", len(cases), caseCount)
	}
	if gotDiagnostics := semanticCaseDiagnosticCount(cases); gotDiagnostics != diagnosticCount {
		t.Fatalf("generated cast case diagnostics = %d, want %d", gotDiagnostics, diagnosticCount)
	}
	slices.SortFunc(cases, func(left, right soundnessevidence.SemanticCase) int {
		return strings.Compare(left.ID, right.ID)
	})
	emitSemanticObservation(t, semanticObservationFromCases(
		t,
		category+".generated",
		"",
		t.Name(),
		cases,
	))
}

func runCategoryMetamorphicCastEvidence(t *testing.T, category, relationID, base, transformed string) {
	t.Helper()

	baseResult, err := runGeneratedGoAnalyzer(t, base, defaultCFGMaxStates)
	if err != nil {
		t.Fatalf("analyze base relation: %v", err)
	}
	transformedResult, err := runGeneratedGoAnalyzer(t, transformed, defaultCFGMaxStates)
	if err != nil {
		t.Fatalf("analyze transformed relation: %v", err)
	}
	wantOutcome := protocoloracle.OutcomeViolation
	if category == CategoryUnvalidatedCastInconclusive {
		wantOutcome = protocoloracle.OutcomeInconclusive
	}
	if baseResult.Outcome != wantOutcome || transformedResult.Outcome != wantOutcome {
		t.Fatalf(
			"metamorphic outcomes = %s/%s, want preserved %s",
			baseResult.Outcome,
			transformedResult.Outcome,
			wantOutcome,
		)
	}
	trace := baseResult.Trace
	trace.merge(transformedResult.Trace)
	if strings.Contains(base, "Value(raw)") && strings.Contains(transformed, "Value(raw)") {
		trace.addStage(soundnessevidence.StageIdentity)
	}
	trace.addDimensions("cast-validation", "relations")
	trace.addProperties("relation-checked")
	wantStages := requiredIntegratedStages()
	wantProperties := []string{"production-analyzer-executed", "relation-checked"}
	wantDimensions := []string{"cast-validation", "relations"}
	requireGeneratedTrace(t, trace, wantStages, wantProperties, wantDimensions)
	result := soundnessevidence.ObservationResult{
		Outcome:         soundnessevidence.OutcomeRelationPreserved,
		CaseCount:       1,
		DiagnosticCount: baseResult.DiagnosticCount + transformedResult.DiagnosticCount,
	}
	cases := []soundnessevidence.SemanticCase{
		{
			ID:        relationID,
			Category:  category,
			Layer:     soundnessevidence.LayerMetamorphic,
			FeatureID: "cast-validation",
			Boundary:  soundnessevidence.BoundaryMetamorphicAnalyzer,
			ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
				soundnessevidence.BoundaryMetamorphicAnalyzer,
				soundnessevidence.BoundaryProductionAnalyzer,
			),
			Outcome:         result.Outcome,
			DiagnosticCount: result.DiagnosticCount,
			Stages:          trace.orderedStages(),
			Properties:      trace.sortedProperties(),
			Dimensions:      trace.sortedDimensions(),
		},
	}
	emitSemanticObservation(t, semanticObservationFromCases(
		t,
		category+".metamorphic",
		"",
		t.Name(),
		cases,
	))
}

func runCategoryFuzzSeedCastEvidence(t *testing.T, registrationID, category string) {
	t.Helper()

	manifestPath := filepath.Join("testdata", "fuzz", "seed-coverage.v1.json")
	var manifest fuzzSeedCoverageManifest
	readStrictJSONTestFile(t, manifestPath, &manifest)
	cases := make([]soundnessevidence.SemanticCase, 0)
	for _, entry := range manifest.Entries {
		if entry.RegistrationID != registrationID {
			continue
		}
		observation := validateCommittedFuzzSeedExpectation(t, fuzzSeedCoverageEntryExpectation(entry))
		caseCount, diagnosticCount := generatedCategoryCounts(t, category, observation.Comparisons)
		wantOutcome := protocoloracle.OutcomeViolation
		if category == CategoryUnvalidatedCastInconclusive {
			wantOutcome = protocoloracle.OutcomeInconclusive
		}
		seedCases := make([]soundnessevidence.SemanticCase, 0, caseCount)
		for _, comparison := range observation.Comparisons.Cases {
			if comparison.Outcome != wantOutcome {
				continue
			}
			trace := generatedExecutionTrace{stages: comparison.Trace.stages}
			trace.addProperties("decoded-seed-matched-category", "independent-property-checked")
			trace.addDimensions("cast-validation", "historical-counterexamples", "seed-structures")
			requireGeneratedTrace(
				t,
				trace,
				requiredIntegratedStages(),
				[]string{"decoded-seed-matched-category", "independent-property-checked"},
				[]string{"cast-validation", "historical-counterexamples", "seed-structures"},
			)
			seedCases = append(seedCases, soundnessevidence.SemanticCase{
				ID: fmt.Sprintf(
					"%s/identity/%06d",
					entry.SeedDigest,
					comparison.Identity,
				),
				Category:  category,
				Layer:     soundnessevidence.LayerFuzz,
				FeatureID: "cast-validation",
				Boundary:  soundnessevidence.BoundaryFuzzDecoder,
				ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
					soundnessevidence.BoundaryFuzzDecoder,
					soundnessevidence.BoundaryIndependentModel,
					soundnessevidence.BoundaryProductionAnalyzer,
				),
				Outcome:         soundnessevidence.OutcomePropertyDetected,
				DiagnosticCount: comparison.DiagnosticCount,
				Stages:          trace.orderedStages(),
				Properties:      trace.sortedProperties(),
				Dimensions:      trace.sortedDimensions(),
			})
		}
		if len(seedCases) != caseCount || semanticCaseDiagnosticCount(seedCases) != diagnosticCount {
			t.Fatalf(
				"fuzz seed %q semantic cases/diagnostics = %d/%d, want %d/%d",
				entry.Seed,
				len(seedCases),
				semanticCaseDiagnosticCount(seedCases),
				caseCount,
				diagnosticCount,
			)
		}
		cases = append(cases, seedCases...)
	}
	if len(cases) == 0 {
		t.Fatalf("registration %s has no exact committed historical seed", registrationID)
	}
	slices.SortFunc(cases, func(left, right soundnessevidence.SemanticCase) int {
		return strings.Compare(left.ID, right.ID)
	})
	emitSemanticObservation(t, semanticObservationFromCases(
		t,
		registrationID,
		"",
		t.Name(),
		cases,
	))
}

func loadProtocolOracleManifest(t testing.TB) protocoloracle.BoundsManifest {
	t.Helper()

	manifest, err := protocoloracle.LoadBoundsManifest(filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		t.Fatalf("LoadBoundsManifest() error: %v", err)
	}
	return manifest
}

func findProtocolProfileProgram(
	t testing.TB,
	manifest protocoloracle.BoundsManifest,
	profileName,
	caseID string,
) protocoloracle.Program {
	t.Helper()

	var found protocoloracle.Program
	if err := protocoloracle.EnumerateProfile(manifest, profileName, func(program protocoloracle.Program) error {
		if program.CaseID == caseID {
			found = program
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if found.CaseID == "" {
		t.Fatalf("profile %s omitted integrated case %q", profileName, caseID)
	}
	return found
}

func generatedCategoryCounts(
	t testing.TB,
	category string,
	counts generatedComparisonCounts,
) (int, int) {
	t.Helper()

	switch category {
	case CategoryUnvalidatedCast:
		if counts.ViolationCases == 0 {
			t.Fatalf("generated evidence produced no %s cases: %+v", category, counts)
		}
		return counts.ViolationCases, counts.ViolationDiagnostics
	case CategoryUnvalidatedCastInconclusive:
		if counts.InconclusiveCases == 0 {
			t.Fatalf("generated evidence produced no %s cases: %+v", category, counts)
		}
		return counts.InconclusiveCases, counts.InconclusiveDiagnostics
	default:
		t.Fatalf("unsupported cast evidence category %q", category)
		return 0, 0
	}
}

func requiredIntegratedStages() []soundnessevidence.ExecutionStage {
	return []soundnessevidence.ExecutionStage{
		soundnessevidence.StageSourceExtraction,
		soundnessevidence.StageIdentity,
		soundnessevidence.StageGraphConstruction,
		soundnessevidence.StagePropagation,
		soundnessevidence.StageRefinement,
		soundnessevidence.StageAggregation,
		soundnessevidence.StageReporting,
	}
}
