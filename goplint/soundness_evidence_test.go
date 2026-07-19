// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"slices"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func emitSoundnessSubgateReport(t *testing.T, populations []soundnessgate.Population) {
	t.Helper()

	if _, err := soundnessgate.EmitReportFromEnvironment(t.Context(), populations); err != nil {
		t.Fatalf("EmitReportFromEnvironment() error: %v", err)
	}
}

func observedPopulations(t testing.TB, observations []soundnessgate.ObservedMember) []soundnessgate.Population {
	t.Helper()

	populations, err := soundnessgate.PopulationsFromObservedMembers(observations)
	if err != nil {
		t.Fatalf("PopulationsFromObservedMembers() error: %v", err)
	}
	return populations
}

func emitSemanticObservation(t *testing.T, observation soundnessevidence.SemanticObservation) {
	t.Helper()

	if _, err := soundnessevidence.EmitObservationFromEnvironment(t.Context(), observation); err != nil {
		t.Fatalf("EmitObservationFromEnvironment() error: %v", err)
	}
}

// syntheticSemanticCasesFromResult is limited to registry contract tests that
// need structurally valid observations without executing a producer. Real
// evidence producers must construct case records from executed identities.
func syntheticSemanticCasesFromResult(
	t testing.TB,
	prefix,
	category string,
	layer soundnessevidence.Layer,
	featureID string,
	boundary soundnessevidence.Boundary,
	result soundnessevidence.ObservationResult,
	stages []soundnessevidence.ExecutionStage,
	properties,
	dimensions []string,
) []soundnessevidence.SemanticCase {
	t.Helper()

	if result.CaseCount <= 0 {
		t.Fatalf("semantic case population = %d, want positive", result.CaseCount)
	}
	cases := make([]soundnessevidence.SemanticCase, result.CaseCount)
	quotient := result.DiagnosticCount / result.CaseCount
	remainder := result.DiagnosticCount % result.CaseCount
	executedBoundaries := []soundnessevidence.Boundary{boundary}
	switch layer {
	case soundnessevidence.LayerGenerated:
		if boundary == soundnessevidence.BoundaryIndependentModel ||
			boundary == soundnessevidence.BoundaryIndependentOracle ||
			slices.Contains(properties, "independent-model-compared") ||
			slices.Contains(properties, "boundary-oracle-compared") {
			executedBoundaries = append(executedBoundaries, soundnessevidence.BoundaryProductionAnalyzer)
		}
		if slices.Contains(properties, "independent-model-compared") {
			executedBoundaries = append(executedBoundaries, soundnessevidence.BoundaryIndependentModel)
		}
	case soundnessevidence.LayerMetamorphic,
		soundnessevidence.LayerFuzz,
		soundnessevidence.LayerDeterminism:
		executedBoundaries = append(executedBoundaries, soundnessevidence.BoundaryProductionAnalyzer)
		if layer == soundnessevidence.LayerFuzz {
			executedBoundaries = append(executedBoundaries, soundnessevidence.BoundaryIndependentModel)
		}
	case soundnessevidence.LayerRuleContract,
		soundnessevidence.LayerOwnerRoute,
		soundnessevidence.LayerMustReport,
		soundnessevidence.LayerMustNotReport,
		soundnessevidence.LayerMustBeInconclusive,
		soundnessevidence.LayerArtifactParity,
		soundnessevidence.LayerProduction,
		soundnessevidence.LayerMutation:
	}
	executedBoundaries = soundnessevidence.CanonicalBoundaries(executedBoundaries...)
	fuzzDigest := soundnessevidence.DigestBytes([]byte(prefix))
	for index := range cases {
		diagnosticCount := quotient
		if index < remainder {
			diagnosticCount++
		}
		caseID := fmt.Sprintf("%s/%06d", prefix, index+1)
		if layer == soundnessevidence.LayerFuzz {
			caseID = fmt.Sprintf("%s/%06d", fuzzDigest, index+1)
		}
		cases[index] = soundnessevidence.SemanticCase{
			ID:                 caseID,
			Category:           category,
			Layer:              layer,
			FeatureID:          featureID,
			Boundary:           boundary,
			ExecutedBoundaries: slices.Clone(executedBoundaries),
			Outcome:            result.Outcome,
			DiagnosticCount:    diagnosticCount,
			Stages:             slices.Clone(stages),
			Properties:         slices.Clone(properties),
			Dimensions:         slices.Clone(dimensions),
		}
	}
	return cases
}

func semanticObservationFromCases(
	t testing.TB,
	registrationID,
	producerID,
	testID string,
	cases []soundnessevidence.SemanticCase,
) soundnessevidence.SemanticObservation {
	t.Helper()

	observation, err := soundnessevidence.ObservationFromCases(
		registrationID,
		producerID,
		testID,
		cases,
	)
	if err != nil {
		t.Fatalf("ObservationFromCases() error: %v", err)
	}
	return observation
}

func semanticCaseDiagnosticCount(cases []soundnessevidence.SemanticCase) int {
	total := 0
	for _, current := range cases {
		total += current.DiagnosticCount
	}
	return total
}
