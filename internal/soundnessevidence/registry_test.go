// SPDX-License-Identifier: MPL-2.0

package soundnessevidence

import "testing"

func TestRegistryValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*Registry)
		wantError string
	}{
		{
			name:      "valid",
			mutate:    func(*Registry) {},
			wantError: "",
		},
		{
			name: "empty",
			mutate: func(registry *Registry) {
				registry.Registrations = nil
			},
			wantError: "no registrations",
		},
		{
			name: "zero population",
			mutate: func(registry *Registry) {
				registry.Registrations[0].Expected.MinimumCases = 0
			},
			wantError: "positive population",
		},
		{
			name: "duplicate id",
			mutate: func(registry *Registry) {
				registry.Registrations = append(registry.Registrations, validTestRegistration())
			},
			wantError: "duplicate registration id",
		},
		{
			name: "duplicate category layer",
			mutate: func(registry *Registry) {
				duplicate := validTestRegistration()
				duplicate.ID = "cast-validation.production-copy"
				registry.Registrations = append(registry.Registrations, duplicate)
			},
			wantError: "duplicate category/layer",
		},
		{
			name: "duplicate property",
			mutate: func(registry *Registry) {
				registry.Registrations[0].Expected.RequiredProperties = []string{"same", "same"}
			},
			wantError: "duplicate",
		},
		{
			name: "invalid diagnostic range",
			mutate: func(registry *Registry) {
				maximum := 0
				registry.Registrations[0].Expected.Diagnostics.Maximum = &maximum
			},
			wantError: "want at least minimum",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			registry := validTestRegistry()
			test.mutate(&registry)
			assertErrorContains(t, registry.Validate(), test.wantError)
		})
	}
}

func TestValidateObservationsRejectsCensusDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*[]SemanticObservation, map[string]ObservationBinding)
		wantError string
	}{
		{
			name:      "valid",
			mutate:    func(*[]SemanticObservation, map[string]ObservationBinding) {},
			wantError: "",
		},
		{
			name: "missing",
			mutate: func(observations *[]SemanticObservation, _ map[string]ObservationBinding) {
				*observations = nil
			},
			wantError: "has no observation",
		},
		{
			name: "duplicate",
			mutate: func(observations *[]SemanticObservation, _ map[string]ObservationBinding) {
				*observations = append(*observations, validTestObservation())
			},
			wantError: "duplicate registration id",
		},
		{
			name: "extra",
			mutate: func(observations *[]SemanticObservation, _ map[string]ObservationBinding) {
				(*observations)[0].RegistrationID = "unregistered"
			},
			wantError: "extra registration id",
		},
		{
			name: "stale",
			mutate: func(observations *[]SemanticObservation, _ map[string]ObservationBinding) {
				(*observations)[0].Binding.WorkspaceDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
			},
			wantError: "stale or mismatched binding",
		},
		{
			name: "successful zero population",
			mutate: func(observations *[]SemanticObservation, _ map[string]ObservationBinding) {
				(*observations)[0].Result.CaseCount = 0
			},
			wantError: "positive population",
		},
		{
			name: "producer identity",
			mutate: func(observations *[]SemanticObservation, _ map[string]ObservationBinding) {
				(*observations)[0].ProducerID = "other-producer"
			},
			wantError: "does not match binding",
		},
		{
			name: "generic category credit",
			mutate: func(observations *[]SemanticObservation, _ map[string]ObservationBinding) {
				(*observations)[0].Category = "generic-protocol"
			},
			wantError: "category",
		},
		{
			name: "missing producer binding",
			mutate: func(_ *[]SemanticObservation, bindings map[string]ObservationBinding) {
				delete(bindings, "semantic-production")
			},
			wantError: "has no expected binding",
		},
		{
			name: "wrong outcome",
			mutate: func(observations *[]SemanticObservation, _ map[string]ObservationBinding) {
				(*observations)[0].Result.Outcome = OutcomeMustNotReport
			},
			wantError: "result",
		},
		{
			name: "wrong dimensions",
			mutate: func(observations *[]SemanticObservation, _ map[string]ObservationBinding) {
				(*observations)[0].Dimensions = []string{"other"}
			},
			wantError: "dimensions are not derived",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			observations := []SemanticObservation{validTestObservation()}
			bindings := map[string]ObservationBinding{
				"semantic-production": validTestBinding(),
			}
			test.mutate(&observations, bindings)
			assertErrorContains(t, ValidateObservations(validTestRegistry(), observations, bindings), test.wantError)
		})
	}
}
