// SPDX-License-Identifier: MPL-2.0

package soundnessevidence

import (
	"errors"
	"fmt"
	"slices"
)

type (
	// Registry is the reviewed, executable category/layer evidence contract.
	Registry struct {
		FormatVersion int            `json:"format_version"`
		Registrations []Registration `json:"registrations"`
	}

	// Registration binds one exact semantic category and layer to its producer
	// and expected machine observation.
	Registration struct {
		ID         string      `json:"id"`
		Category   string      `json:"category"`
		Layer      Layer       `json:"layer"`
		FeatureID  string      `json:"feature_id"`
		ProducerID string      `json:"producer_id"`
		TestID     string      `json:"test_id"`
		Boundary   Boundary    `json:"boundary"`
		Expected   Expectation `json:"expected"`
	}

	// Expectation describes the exact outcome and minimum nonzero population
	// that an evidence registration must observe.
	Expectation struct {
		Outcome            Outcome          `json:"outcome"`
		MinimumCases       int              `json:"minimum_cases"`
		Diagnostics        CountRange       `json:"diagnostics"`
		RequiredStages     []ExecutionStage `json:"required_stages"`
		RequiredProperties []string         `json:"required_properties"`
		RequiredDimensions []string         `json:"required_dimensions"`
	}

	// CountRange defines an inclusive diagnostic-count range. A nil maximum is
	// intentionally unbounded.
	CountRange struct {
		Minimum int  `json:"minimum"`
		Maximum *int `json:"maximum,omitempty"`
	}
)

// Validate verifies that the registry is canonical, bidirectionally
// identifiable, and non-vacuous.
func (registry Registry) Validate() error {
	if registry.FormatVersion != RegistryFormatVersion {
		return fmt.Errorf("evidence registry format_version = %d, want %d", registry.FormatVersion, RegistryFormatVersion)
	}
	if len(registry.Registrations) == 0 {
		return errors.New("evidence registry has no registrations")
	}
	seenIDs := make(map[string]bool, len(registry.Registrations))
	seenKeys := make(map[string]bool, len(registry.Registrations))
	previousID := ""
	for index, registration := range registry.Registrations {
		if err := registration.validate(index); err != nil {
			return err
		}
		if seenIDs[registration.ID] {
			return fmt.Errorf("evidence registry contains duplicate registration id %q", registration.ID)
		}
		seenIDs[registration.ID] = true
		key := registration.categoryLayerKey()
		if seenKeys[key] {
			return fmt.Errorf("evidence registry contains duplicate category/layer registration %q", key)
		}
		seenKeys[key] = true
		if previousID != "" && registration.ID < previousID {
			return fmt.Errorf("evidence registry registrations must use canonical id order: %q precedes %q", registration.ID, previousID)
		}
		previousID = registration.ID
	}
	return nil
}

// ValidateObservations checks the bidirectional registry/observation census.
// Each registration must have exactly one current, producer-bound observation;
// missing, duplicate, extra, stale, and zero-population evidence is rejected.
func ValidateObservations(
	registry Registry,
	observations []SemanticObservation,
	expectedBindings map[string]ObservationBinding,
) error {
	if err := registry.Validate(); err != nil {
		return err
	}
	registrations := make(map[string]Registration, len(registry.Registrations))
	for _, registration := range registry.Registrations {
		registrations[registration.ID] = registration
		expectedBinding, exists := expectedBindings[registration.ProducerID]
		if !exists {
			return fmt.Errorf("evidence registration %q producer %q has no expected binding", registration.ID, registration.ProducerID)
		}
		if err := expectedBinding.Validate(); err != nil {
			return fmt.Errorf("evidence registration %q producer binding: %w", registration.ID, err)
		}
		if expectedBinding.SubgateID != registration.ProducerID {
			return fmt.Errorf("evidence registration %q producer %q has binding subgate_id %q", registration.ID, registration.ProducerID, expectedBinding.SubgateID)
		}
	}

	seen := make(map[string]bool, len(observations))
	for index, observation := range observations {
		if err := observation.Validate(); err != nil {
			return fmt.Errorf("evidence observation[%d]: %w", index, err)
		}
		registration, exists := registrations[observation.RegistrationID]
		if !exists {
			return fmt.Errorf("evidence observation[%d] has extra registration id %q", index, observation.RegistrationID)
		}
		if seen[observation.RegistrationID] {
			return fmt.Errorf("evidence observations contain duplicate registration id %q", observation.RegistrationID)
		}
		seen[observation.RegistrationID] = true
		if err := registration.validateObservation(observation, expectedBindings[registration.ProducerID]); err != nil {
			return err
		}
	}
	for _, registration := range registry.Registrations {
		if !seen[registration.ID] {
			return fmt.Errorf("evidence registration %q has no observation", registration.ID)
		}
	}
	return nil
}

func (registration Registration) validate(index int) error {
	identifiers := []struct {
		name  string
		value string
	}{
		{name: "id", value: registration.ID},
		{name: "category", value: registration.Category},
		{name: "feature_id", value: registration.FeatureID},
		{name: "producer_id", value: registration.ProducerID},
		{name: "test_id", value: registration.TestID},
	}
	for _, identifier := range identifiers {
		name := fmt.Sprintf("evidence registry registrations[%d].%s", index, identifier.name)
		if err := validateIdentifier(name, identifier.value); err != nil {
			return err
		}
	}
	if !validLayer(registration.Layer) {
		return fmt.Errorf("evidence registry registrations[%d].layer %q is unsupported", index, registration.Layer)
	}
	if !validBoundary(registration.Boundary) {
		return fmt.Errorf("evidence registry registrations[%d].boundary %q is unsupported", index, registration.Boundary)
	}
	if !validOutcome(registration.Expected.Outcome) {
		return fmt.Errorf("evidence registry registrations[%d].expected.outcome %q is unsupported", index, registration.Expected.Outcome)
	}
	if registration.Expected.MinimumCases <= 0 {
		return fmt.Errorf("evidence registry registrations[%d].expected.minimum_cases = %d, want a positive population", index, registration.Expected.MinimumCases)
	}
	if err := registration.Expected.Diagnostics.validate(index); err != nil {
		return err
	}
	if err := validateStages(
		fmt.Sprintf("evidence registry registrations[%d].expected.required_stages", index),
		registration.Expected.RequiredStages,
	); err != nil {
		return err
	}
	if err := validateCanonicalStrings(
		fmt.Sprintf("evidence registry registrations[%d].expected.required_properties", index),
		registration.Expected.RequiredProperties,
	); err != nil {
		return err
	}
	if err := validateCanonicalStrings(
		fmt.Sprintf("evidence registry registrations[%d].expected.required_dimensions", index),
		registration.Expected.RequiredDimensions,
	); err != nil {
		return err
	}
	return nil
}

func (registration Registration) validateObservation(
	observation SemanticObservation,
	expectedBinding ObservationBinding,
) error {
	if observation.Binding != expectedBinding {
		return fmt.Errorf("evidence registration %q has stale or mismatched binding", registration.ID)
	}
	actualIdentity := []string{
		observation.Category,
		string(observation.Layer),
		observation.FeatureID,
		observation.ProducerID,
		observation.TestID,
		string(observation.Boundary),
	}
	expectedIdentity := []string{
		registration.Category,
		string(registration.Layer),
		registration.FeatureID,
		registration.ProducerID,
		registration.TestID,
		string(registration.Boundary),
	}
	if !slices.Equal(actualIdentity, expectedIdentity) {
		return fmt.Errorf("evidence registration %q observation identity does not match the registry", registration.ID)
	}
	if observation.Result.Outcome != registration.Expected.Outcome {
		return fmt.Errorf(
			"evidence registration %q outcome = %q, want %q",
			registration.ID,
			observation.Result.Outcome,
			registration.Expected.Outcome,
		)
	}
	if observation.Result.CaseCount < registration.Expected.MinimumCases {
		return fmt.Errorf(
			"evidence registration %q case_count = %d, want at least %d",
			registration.ID,
			observation.Result.CaseCount,
			registration.Expected.MinimumCases,
		)
	}
	if err := registration.Expected.Diagnostics.validateCount(registration.ID, observation.Result.DiagnosticCount); err != nil {
		return err
	}
	if !slices.Equal(observation.Stages, registration.Expected.RequiredStages) {
		return fmt.Errorf("evidence registration %q stages do not exactly match the registry", registration.ID)
	}
	if !slices.Equal(observation.Properties, registration.Expected.RequiredProperties) {
		return fmt.Errorf("evidence registration %q properties do not exactly match the registry", registration.ID)
	}
	if !slices.Equal(observation.Dimensions, registration.Expected.RequiredDimensions) {
		return fmt.Errorf("evidence registration %q dimensions do not exactly match the registry", registration.ID)
	}
	return nil
}

func (registration Registration) categoryLayerKey() string {
	return registration.Category + "\x00" + string(registration.Layer)
}

func (countRange CountRange) validate(registrationIndex int) error {
	if countRange.Minimum < 0 {
		return fmt.Errorf(
			"evidence registry registrations[%d].expected.diagnostics.minimum = %d, want non-negative",
			registrationIndex,
			countRange.Minimum,
		)
	}
	if countRange.Maximum != nil && *countRange.Maximum < countRange.Minimum {
		return fmt.Errorf(
			"evidence registry registrations[%d].expected.diagnostics.maximum = %d, want at least minimum %d",
			registrationIndex,
			*countRange.Maximum,
			countRange.Minimum,
		)
	}
	return nil
}

func (countRange CountRange) validateCount(registrationID string, count int) error {
	if count < countRange.Minimum {
		return fmt.Errorf(
			"evidence registration %q diagnostic_count = %d, want at least %d",
			registrationID,
			count,
			countRange.Minimum,
		)
	}
	if countRange.Maximum != nil && count > *countRange.Maximum {
		return fmt.Errorf(
			"evidence registration %q diagnostic_count = %d, want at most %d",
			registrationID,
			count,
			*countRange.Maximum,
		)
	}
	return nil
}
