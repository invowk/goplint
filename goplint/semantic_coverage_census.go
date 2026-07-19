// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const semanticCoverageCensusVersion = 3

type (
	semanticCoverageCatalog struct {
		Rules        []semanticCoverageRule   `json:"rules"`
		OracleMatrix []semanticCoverageOracle `json:"oracle_matrix"`
	}

	semanticCoverageRule struct {
		Category       string   `json:"category"`
		OutcomeDomain  []string `json:"outcome_domain"`
		BaselinePolicy string   `json:"baseline_policy"`
	}

	semanticCoverageOracle struct {
		Category           string            `json:"category"`
		MustReport         []json.RawMessage `json:"must_report"`
		MustNotReport      []json.RawMessage `json:"must_not_report"`
		MustBeInconclusive []json.RawMessage `json:"must_be_inconclusive"`
	}

	semanticCoverageCensus struct {
		FormatVersion int                         `json:"format_version"`
		Categories    []semanticCoverageCensusRow `json:"categories"`
	}

	semanticCoverageCensusRow struct {
		Category string                        `json:"category"`
		Kind     string                        `json:"kind"`
		Owner    string                        `json:"owner"`
		Route    string                        `json:"production_route"`
		Layers   []semanticCoverageLayerResult `json:"layers"`
	}

	semanticCoverageLayerResult struct {
		Layer              string   `json:"layer"`
		RegistrationID     string   `json:"registration_id"`
		FeatureID          string   `json:"feature_id"`
		ProducerID         string   `json:"producer_id"`
		TestID             string   `json:"test_id"`
		Boundary           string   `json:"boundary"`
		ExecutedBoundaries []string `json:"executed_boundaries"`
		Outcome            string   `json:"outcome"`
		CaseIDs            []string `json:"case_ids"`
		CaseCount          int      `json:"case_count"`
		DiagnosticCount    int      `json:"diagnostic_count"`
		Stages             []string `json:"stages"`
		Properties         []string `json:"properties"`
		Dimensions         []string `json:"dimensions"`
		CommandDigest      string   `json:"command_digest"`
	}
)

// ValidateSemanticCatalogInconclusivePolicy verifies the exact registered
// meaning and no-suppression invariant for protocol uncertainty.
func ValidateSemanticCatalogInconclusivePolicy(ctx context.Context, catalogPath string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("validate semantic catalog inconclusive policy: %w", err)
	}
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("read semantic catalog: %w", err)
	}
	var catalog semanticCoverageCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return fmt.Errorf("decode semantic catalog: %w", err)
	}
	if err := validateProtocolInconclusivePolicy(
		catalog.Rules,
		diagnosticCategoryRegistry(),
		IsProtocolInconclusiveCategory,
	); err != nil {
		return err
	}
	return nil
}

// ValidateSemanticEvidenceRegistry verifies exact bidirectional category/layer
// coverage for every live protocol category. Generic semantic-kind credit and
// registrations for stale or unsupported categories are rejected.
func ValidateSemanticEvidenceRegistry(registry soundnessevidence.Registry) error {
	if err := registry.Validate(); err != nil {
		return fmt.Errorf("validate semantic evidence registry: %w", err)
	}
	if err := validateSemanticRegistries(); err != nil {
		return err
	}
	protocolCategories := make(map[string]CategorySpec)
	for _, category := range diagnosticCategoryRegistry() {
		if category.SemanticKind == semanticKindProtocol {
			protocolCategories[category.Name] = category
		}
	}
	registrations := make(map[string]soundnessevidence.Registration, len(registry.Registrations))
	for _, registration := range registry.Registrations {
		category, exists := protocolCategories[registration.Category]
		if !exists {
			return fmt.Errorf("evidence registration %q has stale or non-protocol category %q", registration.ID, registration.Category)
		}
		layer := semanticEvidenceLayer(registration.Layer)
		if !slices.Contains(category.RequiredOracleLayers, layer) {
			return fmt.Errorf("evidence registration %q has unsupported layer %q", registration.ID, registration.Layer)
		}
		key := semanticEvidenceKey(registration.Category, layer)
		if _, duplicate := registrations[key]; duplicate {
			return fmt.Errorf("duplicate semantic evidence key %q", key)
		}
		registrations[key] = registration
	}
	expectedCount := 0
	for _, category := range protocolCategories {
		expectedCount += len(category.RequiredOracleLayers)
		for _, layer := range category.RequiredOracleLayers {
			key := semanticEvidenceKey(category.Name, layer)
			if _, exists := registrations[key]; !exists {
				return fmt.Errorf("protocol category %q has no executable observation registration for layer %q", category.Name, layer)
			}
		}
	}
	if len(registry.Registrations) != expectedCount {
		return fmt.Errorf("semantic evidence registration count = %d, want exactly %d", len(registry.Registrations), expectedCount)
	}
	return nil
}

// WriteSemanticCoverageCensus consumes executed observations and emits the
// deterministic category/layer census accepted by the blocking aggregate gate.
func WriteSemanticCoverageCensus(
	writer io.Writer,
	registry soundnessevidence.Registry,
	observations []soundnessevidence.SemanticObservation,
) error {
	census, err := buildSemanticCoverageCensus(registry, observations)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(census); err != nil {
		return fmt.Errorf("encode semantic coverage census: %w", err)
	}
	return nil
}

func buildSemanticCoverageCensus(
	registry soundnessevidence.Registry,
	observations []soundnessevidence.SemanticObservation,
) (semanticCoverageCensus, error) {
	if err := ValidateSemanticEvidenceRegistry(registry); err != nil {
		return semanticCoverageCensus{}, err
	}
	expectedBindings := make(map[string]soundnessevidence.ObservationBinding)
	for index, observation := range observations {
		if err := observation.Validate(); err != nil {
			return semanticCoverageCensus{}, fmt.Errorf("semantic census observation[%d]: %w", index, err)
		}
		binding, exists := expectedBindings[observation.ProducerID]
		if exists && binding != observation.Binding {
			return semanticCoverageCensus{}, fmt.Errorf("semantic census producer %q has inconsistent bindings", observation.ProducerID)
		}
		expectedBindings[observation.ProducerID] = observation.Binding
	}
	if err := soundnessevidence.ValidateObservations(registry, observations, expectedBindings); err != nil {
		return semanticCoverageCensus{}, fmt.Errorf("validate semantic evidence observations: %w", err)
	}
	registrations := make(map[string]soundnessevidence.Registration, len(registry.Registrations))
	for _, registration := range registry.Registrations {
		registrations[semanticEvidenceKey(registration.Category, semanticEvidenceLayer(registration.Layer))] = registration
	}
	observationsByID := make(map[string]soundnessevidence.SemanticObservation, len(observations))
	for _, observation := range observations {
		observationsByID[observation.RegistrationID] = observation
	}
	owners := make(map[semanticOwnerKey]semanticOwnerRegistration)
	for _, owner := range semanticOwnerRegistry() {
		if owner.Traversal == nil && owner.PostTraversal == nil {
			return semanticCoverageCensus{}, fmt.Errorf("semantic owner %q is administrative-only", owner.Key)
		}
		owners[owner.Key] = owner
	}
	rows := make([]semanticCoverageCensusRow, 0)
	for _, category := range diagnosticCategoryRegistry() {
		if category.SemanticKind != semanticKindProtocol {
			continue
		}
		owner, exists := owners[category.Owner]
		if !exists || owner.Route == "" {
			return semanticCoverageCensus{}, fmt.Errorf("protocol category %q has no executable production owner", category.Name)
		}
		row := semanticCoverageCensusRow{
			Category: category.Name,
			Kind:     string(category.SemanticKind),
			Owner:    string(category.Owner),
			Route:    string(owner.Route),
			Layers:   make([]semanticCoverageLayerResult, 0, len(category.RequiredOracleLayers)),
		}
		for _, layer := range category.RequiredOracleLayers {
			registration := registrations[semanticEvidenceKey(category.Name, layer)]
			observation := observationsByID[registration.ID]
			caseIDs := make([]string, len(observation.Cases))
			for index, semanticCase := range observation.Cases {
				caseIDs[index] = semanticCase.ID
			}
			row.Layers = append(row.Layers, semanticCoverageLayerResult{
				Layer:              string(layer),
				RegistrationID:     registration.ID,
				FeatureID:          observation.FeatureID,
				ProducerID:         observation.ProducerID,
				TestID:             observation.TestID,
				Boundary:           string(observation.Boundary),
				ExecutedBoundaries: boundaryStrings(observation.ExecutedBoundaries),
				Outcome:            string(observation.Result.Outcome),
				CaseIDs:            caseIDs,
				CaseCount:          observation.Result.CaseCount,
				DiagnosticCount:    observation.Result.DiagnosticCount,
				Stages:             executionStageStrings(observation.Stages),
				Properties:         slices.Clone(observation.Properties),
				Dimensions:         slices.Clone(observation.Dimensions),
				CommandDigest:      observation.Binding.CommandDigest,
			})
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return semanticCoverageCensus{}, errors.New("semantic coverage census has zero protocol categories")
	}
	return semanticCoverageCensus{FormatVersion: semanticCoverageCensusVersion, Categories: rows}, nil
}

func boundaryStrings(boundaries []soundnessevidence.Boundary) []string {
	result := make([]string, len(boundaries))
	for index, boundary := range boundaries {
		result[index] = string(boundary)
	}
	return result
}

func executionStageStrings(stages []soundnessevidence.ExecutionStage) []string {
	result := make([]string, len(stages))
	for index, stage := range stages {
		result[index] = string(stage)
	}
	return result
}

func semanticEvidenceKey(category string, layer semanticEvidenceLayer) string {
	return category + "\x00" + string(layer)
}
