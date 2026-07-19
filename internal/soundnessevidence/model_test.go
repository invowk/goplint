// SPDX-License-Identifier: MPL-2.0

package soundnessevidence

import (
	"slices"
	"strings"
	"testing"
)

func TestObservationBindingValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*ObservationBinding)
		wantError string
	}{
		{
			name:      "valid",
			mutate:    func(*ObservationBinding) {},
			wantError: "",
		},
		{
			name: "empty run",
			mutate: func(binding *ObservationBinding) {
				binding.RunID = ""
			},
			wantError: "run_id is empty",
		},
		{
			name: "noncanonical workspace digest",
			mutate: func(binding *ObservationBinding) {
				binding.WorkspaceDigest = "AA"
			},
			wantError: "workspace_digest",
		},
		{
			name: "noncanonical manifest digest",
			mutate: func(binding *ObservationBinding) {
				binding.ManifestDigest = "sha256:abc"
			},
			wantError: "manifest_digest",
		},
		{
			name: "noncanonical command digest",
			mutate: func(binding *ObservationBinding) {
				binding.CommandDigest = ""
			},
			wantError: "command_digest",
		},
		{
			name: "empty subgate",
			mutate: func(binding *ObservationBinding) {
				binding.SubgateID = ""
			},
			wantError: "subgate_id is empty",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			binding := validTestBinding()
			test.mutate(&binding)
			err := binding.Validate()
			assertErrorContains(t, err, test.wantError)
		})
	}
}

func TestSemanticObservationValidateRejectsNoncanonicalLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*SemanticObservation)
		wantError string
	}{
		{
			name: "duplicate stage",
			mutate: func(observation *SemanticObservation) {
				observation.Stages = append(observation.Stages, StageReporting)
			},
			wantError: "stages contains duplicate",
		},
		{
			name: "duplicate property",
			mutate: func(observation *SemanticObservation) {
				observation.Properties = append(observation.Properties, "diagnostic-emitted")
			},
			wantError: "properties contains duplicate",
		},
		{
			name: "duplicate dimension",
			mutate: func(observation *SemanticObservation) {
				observation.Dimensions = append(observation.Dimensions, "casts")
			},
			wantError: "dimensions contains duplicate",
		},
		{
			name: "duplicate executed boundary",
			mutate: func(observation *SemanticObservation) {
				observation.ExecutedBoundaries = append(
					observation.ExecutedBoundaries,
					observation.ExecutedBoundaries[0],
				)
			},
			wantError: "executed_boundaries must be unique",
		},
		{
			name: "unsorted properties",
			mutate: func(observation *SemanticObservation) {
				observation.Properties = []string{"zeta", "alpha"}
			},
			wantError: "canonical lexical order",
		},
		{
			name: "zero cases",
			mutate: func(observation *SemanticObservation) {
				observation.Result.CaseCount = 0
			},
			wantError: "positive population",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			observation := validTestObservation()
			test.mutate(&observation)
			assertErrorContains(t, observation.Validate(), test.wantError)
		})
	}
}

func TestSemanticObservationValidateAcceptsStrictObservation(t *testing.T) {
	t.Parallel()

	if err := validTestObservation().Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSemanticObservationRejectsClaimsAbsentFromExecutedCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*SemanticObservation)
		wantError string
	}{
		{
			name: "forged stage",
			mutate: func(observation *SemanticObservation) {
				observation.Stages = append(observation.Stages, StageRefinement)
			},
			wantError: "stages are not derived",
		},
		{
			name: "forged executed boundary",
			mutate: func(observation *SemanticObservation) {
				observation.ExecutedBoundaries = []Boundary{
					BoundaryIndependentModel,
					BoundaryProductionAnalyzer,
				}
			},
			wantError: "executed_boundaries are not derived",
		},
		{
			name: "credited boundary not executed",
			mutate: func(observation *SemanticObservation) {
				observation.Cases[0].ExecutedBoundaries = []Boundary{BoundaryIndependentModel}
			},
			wantError: "credited boundary",
		},
		{
			name: "forged property",
			mutate: func(observation *SemanticObservation) {
				observation.Properties = append(observation.Properties, "forged-property")
			},
			wantError: "properties are not derived",
		},
		{
			name: "forged dimension",
			mutate: func(observation *SemanticObservation) {
				observation.Dimensions = append(observation.Dimensions, "forged-dimension")
			},
			wantError: "dimensions are not derived",
		},
		{
			name: "forged category",
			mutate: func(observation *SemanticObservation) {
				observation.Category = "other-category"
			},
			wantError: "category",
		},
		{
			name: "duplicate case",
			mutate: func(observation *SemanticObservation) {
				observation.Cases = append(observation.Cases, observation.Cases[0])
				observation.Result.CaseCount++
				observation.Result.DiagnosticCount += observation.Cases[0].DiagnosticCount
			},
			wantError: "case ids must be unique",
		},
		{
			name: "missing cases",
			mutate: func(observation *SemanticObservation) {
				observation.Cases = nil
			},
			wantError: "case population is empty",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			observation := validTestObservation()
			test.mutate(&observation)
			assertErrorContains(t, observation.Validate(), test.wantError)
		})
	}
}

func TestObservationFromCasesDerivesEverySemanticClaim(t *testing.T) {
	t.Parallel()

	caseRecord := validTestObservation().Cases[0]
	observation, err := ObservationFromCases("registration", "producer", "test", []SemanticCase{caseRecord})
	if err != nil {
		t.Fatalf("ObservationFromCases() error = %v", err)
	}
	if observation.Category != caseRecord.Category || observation.Layer != caseRecord.Layer ||
		observation.FeatureID != caseRecord.FeatureID || observation.Boundary != caseRecord.Boundary ||
		!slices.Equal(observation.ExecutedBoundaries, caseRecord.ExecutedBoundaries) {
		t.Fatalf("ObservationFromCases() identity = %+v, case = %+v", observation, caseRecord)
	}
	if observation.Result.CaseCount != 1 || observation.Result.DiagnosticCount != caseRecord.DiagnosticCount {
		t.Fatalf("ObservationFromCases() result = %+v", observation.Result)
	}
}

func TestSemanticEvidenceRejectsAdversarialFalseCredit(t *testing.T) {
	t.Parallel()

	base := validTestObservation().Cases[0]
	t.Run("reordered fixtures", func(t *testing.T) {
		t.Parallel()
		first := base
		first.ID = "case-001"
		second := base
		second.ID = "case-002"
		_, err := ObservationFromCases("registration", "producer", "test", []SemanticCase{second, first})
		assertErrorContains(t, err, "canonical lexical order")
	})

	tests := []struct {
		name      string
		caseValue SemanticCase
		wantError string
	}{
		{
			name: "relabeled fuzz seed",
			caseValue: adversarialFuzzCase(
				"renamed-seed",
				CanonicalBoundaries(BoundaryFuzzDecoder, BoundaryIndependentModel, BoundaryProductionAnalyzer),
			),
			wantError: "canonical seed digest",
		},
		{
			name: "disconnected fuzz structure",
			caseValue: adversarialFuzzCase(
				"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				CanonicalBoundaries(BoundaryFuzzDecoder, BoundaryIndependentModel),
			),
			wantError: "disconnected from the production analyzer",
		},
		{
			name:      "unrelated deterministic program",
			caseValue: adversarialDeterminismCase([]string{"unrelated-feature"}),
			wantError: "dimensions omit category feature",
		},
		{
			name:      "fixed fixture claims independent oracle",
			caseValue: adversarialIndependentOracleCase(),
			wantError: "disconnected from the production analyzer",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := ObservationFromCases("registration", "producer", "test", []SemanticCase{test.caseValue})
			assertErrorContains(t, err, test.wantError)
		})
	}
}

func adversarialFuzzCase(id string, boundaries []Boundary) SemanticCase {
	return SemanticCase{
		ID:                 id,
		Category:           "unvalidated-cast",
		Layer:              LayerFuzz,
		FeatureID:          "cast-validation",
		Boundary:           BoundaryFuzzDecoder,
		ExecutedBoundaries: boundaries,
		Outcome:            OutcomePropertyDetected,
		DiagnosticCount:    1,
		Stages:             []ExecutionStage{StageSourceExtraction, StageReporting},
		Properties:         []string{"decoded-seed-matched-category", "independent-property-checked"},
		Dimensions:         []string{"cast-validation", "seed-structures"},
	}
}

func adversarialDeterminismCase(dimensions []string) SemanticCase {
	return SemanticCase{
		ID:                 "determinism/case-001",
		Category:           "unvalidated-cast",
		Layer:              LayerDeterminism,
		FeatureID:          "cast-validation",
		Boundary:           BoundaryDeterminismAnalyzer,
		ExecutedBoundaries: CanonicalBoundaries(BoundaryDeterminismAnalyzer, BoundaryProductionAnalyzer),
		Outcome:            OutcomeDeterministic,
		Stages:             []ExecutionStage{StageSourceExtraction, StageReporting},
		Properties:         []string{"equivalent-schedules-compared", "production-analyzer-executed"},
		Dimensions:         dimensions,
	}
}

func adversarialIndependentOracleCase() SemanticCase {
	return SemanticCase{
		ID:                 "fixture/case-001",
		Category:           "missing-constructor-validate",
		Layer:              LayerGenerated,
		FeatureID:          "constructor-validation",
		Boundary:           BoundaryIndependentOracle,
		ExecutedBoundaries: []Boundary{BoundaryIndependentOracle},
		Outcome:            OutcomeModelAgrees,
		Stages:             []ExecutionStage{StageSourceExtraction, StageReporting},
		Properties:         []string{"boundary-oracle-compared", "production-analyzer-executed"},
		Dimensions:         []string{"constructor-validation"},
	}
}

func assertErrorContains(t *testing.T, err error, substring string) {
	t.Helper()
	if substring == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error containing %q", substring)
	}
	if !strings.Contains(err.Error(), substring) {
		t.Fatalf("error = %q, want substring %q", err, substring)
	}
}
