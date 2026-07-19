// SPDX-License-Identifier: MPL-2.0

package soundnessevidence

func validTestBinding() ObservationBinding {
	return ObservationBinding{
		RunID:           "run-1",
		WorkspaceDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestDigest:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		CommandDigest:   "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		SubgateID:       "semantic-production",
	}
}

func validTestRegistration() Registration {
	maximumDiagnostics := 1
	return Registration{
		ID:         "cast-validation.production",
		Category:   "invalid-cast-validation",
		Layer:      LayerProduction,
		FeatureID:  "cast-validation",
		ProducerID: "semantic-production",
		TestID:     "TestSemanticProduction/cast-validation",
		Boundary:   BoundaryProductionAnalyzer,
		Expected: Expectation{
			Outcome:            OutcomeMustReport,
			MinimumCases:       1,
			Diagnostics:        CountRange{Minimum: 1, Maximum: &maximumDiagnostics},
			RequiredStages:     []ExecutionStage{StageSourceExtraction, StageIdentity, StageGraphConstruction, StagePropagation, StageAggregation, StageReporting},
			RequiredProperties: []string{"diagnostic-emitted"},
			RequiredDimensions: []string{"casts"},
		},
	}
}

func validTestRegistry() Registry {
	return Registry{
		FormatVersion: RegistryFormatVersion,
		Registrations: []Registration{validTestRegistration()},
	}
}

func validTestObservation() SemanticObservation {
	registration := validTestRegistration()
	return SemanticObservation{
		FormatVersion:      ObservationFormatVersion,
		Binding:            validTestBinding(),
		RegistrationID:     registration.ID,
		Category:           registration.Category,
		Layer:              registration.Layer,
		FeatureID:          registration.FeatureID,
		ProducerID:         registration.ProducerID,
		TestID:             registration.TestID,
		Boundary:           registration.Boundary,
		ExecutedBoundaries: []Boundary{registration.Boundary},
		Result: ObservationResult{
			Outcome:         registration.Expected.Outcome,
			CaseCount:       registration.Expected.MinimumCases,
			DiagnosticCount: registration.Expected.Diagnostics.Minimum,
		},
		Stages:     slicesClone(registration.Expected.RequiredStages),
		Properties: slicesClone(registration.Expected.RequiredProperties),
		Dimensions: slicesClone(registration.Expected.RequiredDimensions),
		Cases: []SemanticCase{
			{
				ID:                 "case-001",
				Category:           registration.Category,
				Layer:              registration.Layer,
				FeatureID:          registration.FeatureID,
				Boundary:           registration.Boundary,
				ExecutedBoundaries: []Boundary{registration.Boundary},
				Outcome:            registration.Expected.Outcome,
				DiagnosticCount:    registration.Expected.Diagnostics.Minimum,
				Stages:             slicesClone(registration.Expected.RequiredStages),
				Properties:         slicesClone(registration.Expected.RequiredProperties),
				Dimensions:         slicesClone(registration.Expected.RequiredDimensions),
			},
		},
	}
}

func slicesClone[T ~[]E, E any](values T) T {
	return append(T(nil), values...)
}
