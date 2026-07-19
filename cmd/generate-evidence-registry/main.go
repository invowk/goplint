// SPDX-License-Identifier: MPL-2.0

// Command generate-evidence-registry materializes the reviewed category/layer
// evidence contract from the semantic category catalog.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const protocolSemanticKind = "protocol"

type (
	semanticCatalog struct {
		Categories []semanticCategory `json:"category_catalog"`
	}

	semanticCategory struct {
		Category       string   `json:"category"`
		Kind           string   `json:"kind"`
		RequiredLayers []string `json:"required_layers"`
	}

	mutationManifest struct {
		FormatVersion int                `json:"format_version"`
		Mutations     []mutationEvidence `json:"mutations"`
	}

	mutationEvidence struct {
		ID            string                             `json:"id"`
		Categories    []string                           `json:"categories"`
		ChangedStages []soundnessevidence.ExecutionStage `json:"changed_stages"`
	}
)

func main() {
	input := flag.String("input", "spec/semantic-rules.v1.json", "semantic rule catalog")
	mutationManifest := flag.String(
		"mutation-manifest",
		"testdata/mutation/soundness-mutants-v2.json",
		"reviewed causal mutation manifest",
	)
	output := flag.String("output", "spec/semantic-evidence.v2.json", "generated evidence registry")
	flag.Parse()
	if err := generate(*input, *mutationManifest, *output); err != nil {
		fmt.Fprintln(os.Stderr, "generate goplint evidence registry:", err)
		os.Exit(1)
	}
}

func generate(inputPath, mutationManifestPath, outputPath string) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read semantic catalog: %w", err)
	}
	var catalog semanticCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return fmt.Errorf("decode semantic catalog: %w", err)
	}
	mutationStages, err := loadMutationStages(mutationManifestPath)
	if err != nil {
		return err
	}
	if err := validateMutationStageCoverage(catalog, mutationStages); err != nil {
		return err
	}
	registry, err := buildRegistry(catalog, mutationStages)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode evidence registry: %w", err)
	}
	encoded = append(encoded, '\n')
	temporaryPath := outputPath + ".tmp"
	if err := os.WriteFile(temporaryPath, encoded, 0o600); err != nil {
		return fmt.Errorf("write temporary evidence registry: %w", err)
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish evidence registry: %w", err)
	}
	return nil
}

func loadMutationStages(path string) (map[string][]soundnessevidence.ExecutionStage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read causal mutation manifest: %w", err)
	}
	var manifest mutationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode causal mutation manifest: %w", err)
	}
	if manifest.FormatVersion != 2 {
		return nil, fmt.Errorf("causal mutation manifest format_version = %d, want 2", manifest.FormatVersion)
	}
	if len(manifest.Mutations) == 0 {
		return nil, errors.New("causal mutation manifest has no mutations")
	}

	stageSets := make(map[string]map[soundnessevidence.ExecutionStage]bool)
	seenMutations := make(map[string]bool, len(manifest.Mutations))
	for index, mutation := range manifest.Mutations {
		if mutation.ID == "" {
			return nil, fmt.Errorf("causal mutation manifest mutations[%d].id is empty", index)
		}
		if seenMutations[mutation.ID] {
			return nil, fmt.Errorf("causal mutation manifest repeats mutation %q", mutation.ID)
		}
		seenMutations[mutation.ID] = true
		if err := validateMutationStages(mutation.ID, mutation.ChangedStages); err != nil {
			return nil, err
		}
		if len(mutation.Categories) == 0 {
			continue
		}
		seenCategories := make(map[string]bool, len(mutation.Categories))
		for _, category := range mutation.Categories {
			if category == "" {
				return nil, fmt.Errorf("causal mutation %q has an empty category", mutation.ID)
			}
			if seenCategories[category] {
				return nil, fmt.Errorf("causal mutation %q repeats category %q", mutation.ID, category)
			}
			seenCategories[category] = true
			if stageSets[category] == nil {
				stageSets[category] = make(map[soundnessevidence.ExecutionStage]bool)
			}
			for _, stage := range mutation.ChangedStages {
				stageSets[category][stage] = true
			}
		}
	}

	result := make(map[string][]soundnessevidence.ExecutionStage, len(stageSets))
	for category, stages := range stageSets {
		result[category] = canonicalMutationStages(stages)
	}
	return result, nil
}

func validateMutationStageCoverage(
	catalog semanticCatalog,
	mutationStages map[string][]soundnessevidence.ExecutionStage,
) error {
	required := make(map[string]bool)
	for _, category := range catalog.Categories {
		if category.Kind == protocolSemanticKind &&
			slices.Contains(category.RequiredLayers, string(soundnessevidence.LayerMutation)) {
			required[category.Category] = true
		}
	}
	for category := range mutationStages {
		if !required[category] {
			return fmt.Errorf("causal mutation manifest names unregistered mutation category %q", category)
		}
	}
	for category := range required {
		if len(mutationStages[category]) == 0 {
			return fmt.Errorf("protocol category %q has no reviewed causal mutation stages", category)
		}
	}
	return nil
}

func validateMutationStages(id string, stages []soundnessevidence.ExecutionStage) error {
	if len(stages) == 0 {
		return fmt.Errorf("causal mutation %q changed_stages is empty", id)
	}
	set := make(map[soundnessevidence.ExecutionStage]bool, len(stages))
	for _, stage := range stages {
		set[stage] = true
	}
	canonical := canonicalMutationStages(set)
	if len(canonical) != len(stages) || !slices.Equal(canonical, stages) {
		return fmt.Errorf("causal mutation %q changed_stages must be unique and canonical", id)
	}
	return nil
}

func canonicalMutationStages(
	set map[soundnessevidence.ExecutionStage]bool,
) []soundnessevidence.ExecutionStage {
	order := []soundnessevidence.ExecutionStage{
		soundnessevidence.StageSourceExtraction,
		soundnessevidence.StageIdentity,
		soundnessevidence.StageGraphConstruction,
		soundnessevidence.StagePropagation,
		soundnessevidence.StageRefinement,
		soundnessevidence.StageAggregation,
		soundnessevidence.StageReporting,
	}
	result := make([]soundnessevidence.ExecutionStage, 0, len(set))
	for _, stage := range order {
		if set[stage] {
			result = append(result, stage)
		}
	}
	return result
}

func buildRegistry(
	catalog semanticCatalog,
	mutationStages map[string][]soundnessevidence.ExecutionStage,
) (soundnessevidence.Registry, error) {
	registrations := make([]soundnessevidence.Registration, 0)
	for _, category := range catalog.Categories {
		if category.Kind != protocolSemanticKind {
			continue
		}
		featureID, err := categoryFeatureID(category.Category)
		if err != nil {
			return soundnessevidence.Registry{}, err
		}
		for _, layerName := range category.RequiredLayers {
			registration, err := buildRegistration(
				category.Category,
				featureID,
				soundnessevidence.Layer(layerName),
				mutationStages,
			)
			if err != nil {
				return soundnessevidence.Registry{}, err
			}
			registrations = append(registrations, registration)
		}
	}
	slices.SortFunc(registrations, func(left, right soundnessevidence.Registration) int {
		return compareStrings(left.ID, right.ID)
	})
	registry := soundnessevidence.Registry{
		FormatVersion: soundnessevidence.RegistryFormatVersion,
		Registrations: registrations,
	}
	if err := registry.Validate(); err != nil {
		return soundnessevidence.Registry{}, fmt.Errorf("validate generated evidence registry: %w", err)
	}
	return registry, nil
}

func buildRegistration(
	category string,
	featureID string,
	layer soundnessevidence.Layer,
	mutationStages map[string][]soundnessevidence.ExecutionStage,
) (soundnessevidence.Registration, error) {
	registration := soundnessevidence.Registration{
		ID:        category + "." + string(layer),
		Category:  category,
		Layer:     layer,
		FeatureID: featureID,
	}
	fullStages := []soundnessevidence.ExecutionStage{
		soundnessevidence.StageSourceExtraction,
		soundnessevidence.StageIdentity,
		soundnessevidence.StageGraphConstruction,
		soundnessevidence.StagePropagation,
		soundnessevidence.StageRefinement,
		soundnessevidence.StageAggregation,
		soundnessevidence.StageReporting,
	}
	productionRouteStages := []soundnessevidence.ExecutionStage{
		soundnessevidence.StageSourceExtraction,
		soundnessevidence.StageReporting,
	}
	switch layer {
	case soundnessevidence.LayerRuleContract:
		registration.ProducerID = "catalog"
		registration.TestID = "TestSemanticEvidenceContract/" + category + "/rule-contract"
		registration.Boundary = soundnessevidence.BoundaryCatalogValidation
		registration.Expected = expectation(
			soundnessevidence.OutcomeModelAgrees,
			0,
			nil,
			[]string{"catalog-rule-matched", "exact-category-bound"},
			[]string{featureID},
		)
	case soundnessevidence.LayerOwnerRoute:
		registration.ProducerID = "production-integration"
		registration.TestID = "TestProtocolProductionRoutingEvidence/" + category
		registration.Boundary = soundnessevidence.BoundaryProductionAnalyzer
		registration.Expected = expectation(
			soundnessevidence.OutcomeRouteExecuted,
			0,
			productionRouteStages,
			[]string{"category-owner-executed", "production-route-executed"},
			[]string{featureID},
		)
	case soundnessevidence.LayerMustReport:
		registration.ProducerID = "catalog"
		registration.TestID = "TestSemanticEvidenceProduction/" + category + "/must-report"
		registration.Boundary = soundnessevidence.BoundaryProductionAnalyzer
		registration.Expected = expectation(
			soundnessevidence.OutcomeMustReport,
			1,
			productionRouteStages,
			[]string{"diagnostic-emitted", "fixture-executed"},
			[]string{featureID},
		)
	case soundnessevidence.LayerMustNotReport:
		maximum := 0
		registration.ProducerID = "catalog"
		registration.TestID = "TestSemanticEvidenceProduction/" + category + "/must-not-report"
		registration.Boundary = soundnessevidence.BoundaryProductionAnalyzer
		registration.Expected = expectationWithMaximum(
			soundnessevidence.OutcomeMustNotReport,
			0,
			&maximum,
			productionRouteStages,
			[]string{"fixture-executed", "no-diagnostic-emitted"},
			[]string{featureID},
		)
	case soundnessevidence.LayerMustBeInconclusive:
		registration.ProducerID = "catalog"
		registration.TestID = "TestSemanticEvidenceProduction/" + category + "/must-be-inconclusive"
		registration.Boundary = soundnessevidence.BoundaryProductionAnalyzer
		registration.Expected = expectation(
			soundnessevidence.OutcomeMustBeInconclusive,
			1,
			productionRouteStages,
			[]string{"blocking-inconclusive-emitted", "fixture-executed"},
			[]string{featureID},
		)
	case soundnessevidence.LayerProduction:
		registration.ProducerID = "production-integration"
		registration.TestID = "TestSemanticEvidenceProduction/" + category + "/production-integration"
		registration.Boundary = soundnessevidence.BoundaryProductionAnalyzer
		registration.Expected = expectation(
			soundnessevidence.OutcomeModelAgrees,
			0,
			productionRouteStages,
			[]string{"negative-path-executed", "positive-path-executed", "uncertainty-path-executed"},
			[]string{featureID},
		)
	case soundnessevidence.LayerGenerated:
		registration.ProducerID = "end-to-end-oracle"
		registration.TestID = "TestCategoryGeneratedEvidence/" + category
		if featureID == "cast-validation" {
			registration.Boundary = soundnessevidence.BoundaryProductionAnalyzer
			registration.Expected = expectation(
				soundnessevidence.OutcomeModelAgrees,
				0,
				fullStages,
				[]string{"independent-model-compared", "production-analyzer-executed"},
				generatedDimensions(featureID),
			)
		} else {
			registration.Boundary = soundnessevidence.BoundaryIndependentOracle
			registration.Expected = expectation(
				soundnessevidence.OutcomeModelAgrees,
				0,
				productionRouteStages,
				[]string{"boundary-oracle-compared", "production-analyzer-executed"},
				sortedStrings(featureID, "oracle-outcomes"),
			)
		}
	case soundnessevidence.LayerMetamorphic:
		registration.ProducerID = "protocol-oracle"
		registration.TestID = "TestCategoryMetamorphicEvidence/" + category
		registration.Boundary = soundnessevidence.BoundaryMetamorphicAnalyzer
		stages := fullStages
		if featureID != "cast-validation" {
			stages = productionRouteStages
		}
		registration.Expected = expectation(
			soundnessevidence.OutcomeRelationPreserved,
			0,
			stages,
			[]string{"production-analyzer-executed", "relation-checked"},
			sortedStrings(featureID, "relations"),
		)
	case soundnessevidence.LayerFuzz:
		registration.ProducerID = "fuzz-seeds"
		registration.TestID = "TestCategoryFuzzSeedEvidence/" + category
		registration.Boundary = soundnessevidence.BoundaryFuzzDecoder
		if featureID == "cast-validation" {
			registration.Expected = expectation(
				soundnessevidence.OutcomePropertyDetected,
				0,
				fullStages,
				[]string{"decoded-seed-matched-category", "independent-property-checked"},
				sortedStrings(featureID, "historical-counterexamples", "seed-structures"),
			)
		} else {
			registration.Expected = expectation(
				soundnessevidence.OutcomePropertyDetected,
				0,
				productionRouteStages,
				[]string{"decoded-structure-matched-category", "independent-property-checked"},
				sortedStrings(featureID, "historical-counterexamples", "seed-structures"),
			)
		}
	case soundnessevidence.LayerMutation:
		stages, ok := mutationStages[category]
		if !ok || len(stages) == 0 {
			return soundnessevidence.Registration{}, fmt.Errorf(
				"protocol category %q has no reviewed causal mutation stages",
				category,
			)
		}
		registration.ProducerID = "targeted-mutation"
		registration.TestID = "cmd/targeted-mutation/" + category
		registration.Boundary = soundnessevidence.BoundaryMutationRunner
		registration.Expected = expectation(
			soundnessevidence.OutcomeMutantKilled,
			0,
			slices.Clone(stages),
			[]string{
				"clean-control-passed",
				"declared-guard-selected",
				"exact-anchor-selected",
				"exact-transformation-applied",
				"intended-mismatch-observed",
				"mismatch-repeatable",
				"mutant-compiled",
				"post-control-passed",
				"source-restored",
			},
			sortedStrings(featureID, "guards", "mutants"),
		)
	case soundnessevidence.LayerDeterminism:
		registration.ProducerID = "determinism"
		registration.TestID = "TestCategoryDeterminismEvidence/" + category
		registration.Boundary = soundnessevidence.BoundaryDeterminismAnalyzer
		registration.Expected = expectation(
			soundnessevidence.OutcomeDeterministic,
			0,
			productionRouteStages,
			[]string{"equivalent-schedules-compared", "production-analyzer-executed"},
			sortedStrings("equivalent-schedules", featureID),
		)
	default:
		return soundnessevidence.Registration{}, fmt.Errorf("category %q has unsupported evidence layer %q", category, layer)
	}
	return registration, nil
}

func generatedDimensions(featureID string) []string {
	dimensions := []string{"call-sites", "constraints", "facts", featureID}
	if featureID != "boundary-request-validation" {
		dimensions = append(dimensions, "aliases", "return-edges")
	}
	return sortedStrings(dimensions...)
}

func expectation(
	outcome soundnessevidence.Outcome,
	minimumDiagnostics int,
	stages []soundnessevidence.ExecutionStage,
	properties []string,
	dimensions []string,
) soundnessevidence.Expectation {
	return expectationWithMaximum(outcome, minimumDiagnostics, nil, stages, properties, dimensions)
}

func expectationWithMaximum(
	outcome soundnessevidence.Outcome,
	minimumDiagnostics int,
	maximumDiagnostics *int,
	stages []soundnessevidence.ExecutionStage,
	properties []string,
	dimensions []string,
) soundnessevidence.Expectation {
	return soundnessevidence.Expectation{
		Outcome:      outcome,
		MinimumCases: 1,
		Diagnostics: soundnessevidence.CountRange{
			Minimum: minimumDiagnostics,
			Maximum: maximumDiagnostics,
		},
		RequiredStages:     stages,
		RequiredProperties: properties,
		RequiredDimensions: dimensions,
	}
}

func categoryFeatureID(category string) (string, error) {
	switch category {
	case "unvalidated-cast", "unvalidated-cast-inconclusive":
		return "cast-validation", nil
	case "missing-constructor-validate", "missing-constructor-validate-inconclusive":
		return "constructor-validation", nil
	case "use-before-validate-same-block", "use-before-validate-cross-block", "use-before-validate-inconclusive":
		return "use-before-validation", nil
	case "unvalidated-boundary-request":
		return "boundary-request-validation", nil
	default:
		return "", fmt.Errorf("protocol category %q has no reviewed semantic feature", category)
	}
}

func sortedStrings(values ...string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return result
}

func compareStrings(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
