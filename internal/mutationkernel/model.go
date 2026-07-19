// SPDX-License-Identifier: MPL-2.0

// Package mutationkernel validates that the blocking causal mutation profile
// covers every semantic category whose catalog contract requires mutation.
package mutationkernel

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

const (
	manifestFormatVersion     = 1
	mutationDataFormatVersion = 2
	semanticRulesVersion      = "v1"
	mutationLayer             = "mutation"

	// PopulationBindings identifies exact category-to-mutant relationships.
	PopulationBindings = "mutation-kernel-bindings"
	// PopulationCategories identifies covered mutation-required categories.
	PopulationCategories = "mutation-kernel-categories"
	// PopulationMutants identifies selected causal mutants credited by coverage.
	PopulationMutants = "mutation-kernel-mutants"
)

type (
	manifest struct {
		FormatVersion   int    `json:"format_version"`
		SemanticRules   string `json:"semantic_rules"`
		BlockingProfile string `json:"blocking_profile"`
		MutantCatalog   string `json:"mutant_catalog"`
	}

	semanticRulesManifest struct {
		Version                string             `json:"version"`
		CategoryCatalog        []semanticCategory `json:"category_catalog"`
		Rules                  json.RawMessage    `json:"rules"`
		OracleMatrix           json.RawMessage    `json:"oracle_matrix"`
		HistoricalMissFixtures json.RawMessage    `json:"historical_miss_fixtures"`
		HistoricalMissOracles  json.RawMessage    `json:"historical_miss_oracles"`
	}

	semanticCategory struct {
		Category       string   `json:"category"`
		Kind           string   `json:"kind"`
		OwnerKey       string   `json:"owner_key"`
		OracleStrategy string   `json:"oracle_strategy"`
		RequiredLayers []string `json:"required_layers"`
	}

	mutationProfile struct {
		FormatVersion int      `json:"format_version"`
		Manifest      string   `json:"manifest"`
		Count         int      `json:"count"`
		MutationIDs   []string `json:"mutation_ids"`
	}

	mutationCatalog struct {
		FormatVersion int              `json:"format_version"`
		Profile       string           `json:"profile"`
		Policy        mutationPolicy   `json:"policy"`
		Mutations     []mutationRecord `json:"mutations"`
	}

	mutationPolicy struct {
		MaxSurvivors      int  `json:"max_survivors"`
		AllowCompileKills bool `json:"allow_compile_kills"`
		AllowBaseline     bool `json:"allow_baseline"`
	}

	mutationRecord struct {
		ID            string        `json:"id"`
		Concern       string        `json:"concern"`
		Categories    []string      `json:"categories"`
		ChangedStages []string      `json:"changed_stages"`
		File          string        `json:"file"`
		Before        string        `json:"before"`
		BeforeSHA256  string        `json:"before_sha256"`
		After         string        `json:"after"`
		AfterSHA256   string        `json:"after_sha256"`
		Guard         mutationGuard `json:"guard"`
	}

	mutationGuard struct {
		TestRegex          string             `json:"test_regex"`
		SelectedTests      []string           `json:"selected_tests"`
		ExpectedMismatches []expectedMismatch `json:"expected_mismatches"`
	}

	expectedMismatch struct {
		Assertion assertionIdentity `json:"assertion"`
		Expected  semanticState     `json:"expected"`
		Actual    semanticState     `json:"actual"`
	}

	assertionIdentity struct {
		Test string `json:"test"`
		ID   string `json:"id"`
	}

	semanticState struct {
		Subject string `json:"subject"`
		State   string `json:"state"`
	}

	coverageAccumulator struct {
		MutantIDs     map[string]bool
		ChangedStages map[string]bool
		AssertionIDs  map[string]bool
	}

	// Observation binds one mutation-required category to its selected causal
	// mutants and the metadata that makes their kills semantically attributable.
	Observation struct {
		Category                     string   `json:"category"`
		MutantIDs                    []string `json:"mutant_ids"`
		ChangedStages                []string `json:"changed_stages"`
		ExpectedMismatchAssertionIDs []string `json:"expected_mismatch_assertion_ids"`
	}

	// Result is the deterministic structured mutation-kernel coverage census.
	Result struct {
		FormatVersion       int           `json:"format_version"`
		SemanticRules       string        `json:"semantic_rules"`
		BlockingProfile     string        `json:"blocking_profile"`
		MutantCatalog       string        `json:"mutant_catalog"`
		CategoryCount       int           `json:"category_count"`
		SelectedMutantCount int           `json:"selected_mutant_count"`
		BindingCount        int           `json:"binding_count"`
		Observations        []Observation `json:"observations"`
	}
)

// Populations derives the exact non-vacuous aggregate populations from the
// structured category-to-mutant observations.
func (result Result) Populations() ([]soundnessgate.Population, error) {
	if len(result.Observations) == 0 {
		return nil, errors.New("mutation kernel has no category observations")
	}
	if result.CategoryCount != len(result.Observations) {
		return nil, fmt.Errorf(
			"mutation kernel category_count = %d, want %d observations",
			result.CategoryCount,
			len(result.Observations),
		)
	}
	members := make([]soundnessgate.ObservedMember, 0, result.BindingCount+len(result.Observations))
	mutants := make(map[string]bool, result.SelectedMutantCount)
	bindingCount := 0
	for _, observation := range result.Observations {
		members = append(members, soundnessgate.ObservedMember{
			PopulationID: PopulationCategories,
			MemberID:     observation.Category,
		})
		for _, mutantID := range observation.MutantIDs {
			mutants[mutantID] = true
			bindingCount++
			members = append(members, soundnessgate.ObservedMember{
				PopulationID: PopulationBindings,
				MemberID:     observation.Category + "=" + mutantID,
			})
		}
	}
	if result.BindingCount != bindingCount {
		return nil, fmt.Errorf(
			"mutation kernel binding_count = %d, want %d observed bindings",
			result.BindingCount,
			bindingCount,
		)
	}
	mutantIDs := sortedSet(mutants)
	if result.SelectedMutantCount != len(mutantIDs) {
		return nil, fmt.Errorf(
			"mutation kernel selected_mutant_count = %d, want %d observed mutants",
			result.SelectedMutantCount,
			len(mutantIDs),
		)
	}
	for _, mutantID := range mutantIDs {
		members = append(members, soundnessgate.ObservedMember{
			PopulationID: PopulationMutants,
			MemberID:     mutantID,
		})
	}
	populations, err := soundnessgate.PopulationsFromObservedMembers(members)
	if err != nil {
		return nil, fmt.Errorf("derive mutation kernel populations: %w", err)
	}
	return populations, nil
}

// PopulationRequirements returns exact current minima for manifest generation.
func (result Result) PopulationRequirements() ([]soundnessgate.PopulationRequirement, error) {
	populations, err := result.Populations()
	if err != nil {
		return nil, err
	}
	requirements := make([]soundnessgate.PopulationRequirement, 0, len(populations))
	for _, population := range populations {
		requirements = append(requirements, soundnessgate.PopulationRequirement{
			ID:      population.ID,
			Minimum: population.Count,
		})
	}
	return requirements, nil
}

func evaluate(
	definition manifest,
	rules semanticRulesManifest,
	profile mutationProfile,
	catalog mutationCatalog,
) (Result, error) {
	if err := definition.validate(); err != nil {
		return Result{}, err
	}
	allCategories, requiredCategories, err := rules.categories()
	if err != nil {
		return Result{}, err
	}
	if err := profile.validate(definition.MutantCatalog); err != nil {
		return Result{}, err
	}
	mutations, err := catalog.selectedMutations(profile.MutationIDs, allCategories, requiredCategories)
	if err != nil {
		return Result{}, err
	}
	coverage := make(map[string]*coverageAccumulator, len(requiredCategories))
	for category := range requiredCategories {
		coverage[category] = &coverageAccumulator{
			MutantIDs:     make(map[string]bool),
			ChangedStages: make(map[string]bool),
			AssertionIDs:  make(map[string]bool),
		}
	}
	usedMutants := make(map[string]bool, len(mutations))
	for _, mutation := range mutations {
		coveredRequiredCategory := false
		for _, category := range mutation.Categories {
			accumulator, required := coverage[category]
			if !required {
				continue
			}
			coveredRequiredCategory = true
			usedMutants[mutation.ID] = true
			accumulator.MutantIDs[mutation.ID] = true
			for _, stage := range mutation.ChangedStages {
				accumulator.ChangedStages[stage] = true
			}
			for _, mismatch := range mutation.Guard.ExpectedMismatches {
				assertionID := mismatch.Assertion.Test + "/" + mismatch.Assertion.ID
				accumulator.AssertionIDs[mutation.ID+":"+assertionID] = true
			}
		}
		if !coveredRequiredCategory {
			return Result{}, fmt.Errorf("selected mutant %q covers no mutation-required semantic category", mutation.ID)
		}
	}
	categories := sortedSet(requiredCategories)
	observations := make([]Observation, 0, len(categories))
	var uncovered []string
	bindingCount := 0
	for _, category := range categories {
		accumulator := coverage[category]
		mutantIDs := sortedSet(accumulator.MutantIDs)
		if len(mutantIDs) == 0 {
			uncovered = append(uncovered, category)
			continue
		}
		bindingCount += len(mutantIDs)
		observations = append(observations, Observation{
			Category:                     category,
			MutantIDs:                    mutantIDs,
			ChangedStages:                sortedSet(accumulator.ChangedStages),
			ExpectedMismatchAssertionIDs: sortedSet(accumulator.AssertionIDs),
		})
	}
	if len(uncovered) != 0 {
		return Result{}, fmt.Errorf(
			"uncovered mutation-required semantic categories: %s",
			strings.Join(uncovered, ", "),
		)
	}
	if len(usedMutants) != len(profile.MutationIDs) {
		return Result{}, errors.New("one or more selected mutants were not credited to the mutation kernel")
	}
	return Result{
		FormatVersion:       manifestFormatVersion,
		SemanticRules:       definition.SemanticRules,
		BlockingProfile:     definition.BlockingProfile,
		MutantCatalog:       definition.MutantCatalog,
		CategoryCount:       len(observations),
		SelectedMutantCount: len(usedMutants),
		BindingCount:        bindingCount,
		Observations:        observations,
	}, nil
}

func (definition manifest) validate() error {
	if definition.FormatVersion != manifestFormatVersion {
		return fmt.Errorf(
			"mutation kernel manifest format_version = %d, want %d",
			definition.FormatVersion,
			manifestFormatVersion,
		)
	}
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "semantic_rules", value: definition.SemanticRules},
		{label: "blocking_profile", value: definition.BlockingProfile},
		{label: "mutant_catalog", value: definition.MutantCatalog},
	} {
		if err := validateRepositoryPath(field.label, field.value); err != nil {
			return err
		}
	}
	return nil
}

func (rules semanticRulesManifest) categories() (map[string]bool, map[string]bool, error) {
	if rules.Version != semanticRulesVersion || len(rules.CategoryCatalog) == 0 {
		return nil, nil, errors.New("empty or unsupported semantic rules manifest")
	}
	allCategories := make(map[string]bool, len(rules.CategoryCatalog))
	required := make(map[string]bool)
	for index, category := range rules.CategoryCatalog {
		if err := validateToken(fmt.Sprintf("category_catalog[%d].category", index), category.Category); err != nil {
			return nil, nil, err
		}
		if allCategories[category.Category] {
			return nil, nil, fmt.Errorf("semantic rules contain duplicate category %q", category.Category)
		}
		allCategories[category.Category] = true
		if err := validateUniqueTokens(
			fmt.Sprintf("category %q required_layers", category.Category),
			category.RequiredLayers,
		); err != nil {
			return nil, nil, err
		}
		if slices.Contains(category.RequiredLayers, mutationLayer) {
			required[category.Category] = true
		}
	}
	if len(required) == 0 {
		return nil, nil, errors.New("semantic rules contain no mutation-required categories")
	}
	return allCategories, required, nil
}

func (profile mutationProfile) validate(catalogPath string) error {
	if profile.FormatVersion != mutationDataFormatVersion || profile.Count < 2 || len(profile.MutationIDs) == 0 {
		return errors.New("empty or unsupported blocking mutation profile")
	}
	if profile.Manifest != catalogPath {
		return fmt.Errorf(
			"blocking profile manifest = %q, want bound mutant catalog %q",
			profile.Manifest,
			catalogPath,
		)
	}
	return validateUniqueTokens("blocking profile mutation_ids", profile.MutationIDs)
}

func (catalog mutationCatalog) selectedMutations(
	selectedIDs []string,
	allCategories map[string]bool,
	requiredCategories map[string]bool,
) ([]mutationRecord, error) {
	if catalog.FormatVersion != mutationDataFormatVersion || strings.TrimSpace(catalog.Profile) == "" ||
		catalog.Policy.MaxSurvivors != 0 || catalog.Policy.AllowCompileKills || catalog.Policy.AllowBaseline ||
		len(catalog.Mutations) == 0 {
		return nil, errors.New("empty, unsupported, or weakened mutant catalog")
	}
	byID := make(map[string]mutationRecord, len(catalog.Mutations))
	for _, mutation := range catalog.Mutations {
		if err := validateToken("mutant id", mutation.ID); err != nil {
			return nil, err
		}
		if _, duplicate := byID[mutation.ID]; duplicate {
			return nil, fmt.Errorf("mutant catalog contains duplicate id %q", mutation.ID)
		}
		byID[mutation.ID] = mutation
	}
	mutations := make([]mutationRecord, 0, len(selectedIDs))
	for _, mutantID := range selectedIDs {
		mutation, found := byID[mutantID]
		if !found {
			return nil, fmt.Errorf("blocking profile selects unknown mutant %q", mutantID)
		}
		if err := mutation.validate(allCategories, requiredCategories); err != nil {
			return nil, err
		}
		mutations = append(mutations, mutation)
	}
	return mutations, nil
}

func (mutation mutationRecord) validate(allCategories, requiredCategories map[string]bool) error {
	if strings.TrimSpace(mutation.Concern) == "" || strings.TrimSpace(mutation.File) == "" ||
		strings.TrimSpace(mutation.Before) == "" || strings.TrimSpace(mutation.BeforeSHA256) == "" ||
		strings.TrimSpace(mutation.After) == "" || strings.TrimSpace(mutation.AfterSHA256) == "" {
		return fmt.Errorf("selected mutant %q is incomplete", mutation.ID)
	}
	if err := validateUniqueTokens("selected mutant "+mutation.ID+" categories", mutation.Categories); err != nil {
		return err
	}
	if err := validateUniqueTokens("selected mutant "+mutation.ID+" changed_stages", mutation.ChangedStages); err != nil {
		return err
	}
	if len(mutation.Guard.ExpectedMismatches) == 0 {
		return fmt.Errorf("selected mutant %q has no expected mismatches", mutation.ID)
	}
	coveredRequiredCategory := false
	for _, category := range mutation.Categories {
		if !allCategories[category] {
			return fmt.Errorf("selected mutant %q names unknown semantic category %q", mutation.ID, category)
		}
		if requiredCategories[category] {
			coveredRequiredCategory = true
		}
	}
	if !coveredRequiredCategory {
		return fmt.Errorf("selected mutant %q has no mutation-required category metadata", mutation.ID)
	}
	seenAssertions := make(map[string]bool, len(mutation.Guard.ExpectedMismatches))
	for _, mismatch := range mutation.Guard.ExpectedMismatches {
		key := mismatch.Assertion.Test + "/" + mismatch.Assertion.ID
		if err := validateToken("selected mutant "+mutation.ID+" mismatch assertion", key); err != nil {
			return err
		}
		if seenAssertions[key] {
			return fmt.Errorf("selected mutant %q has duplicate mismatch assertion %q", mutation.ID, key)
		}
		seenAssertions[key] = true
		if err := mismatch.Expected.validate("expected"); err != nil {
			return fmt.Errorf("selected mutant %q: %w", mutation.ID, err)
		}
		if err := mismatch.Actual.validate("actual"); err != nil {
			return fmt.Errorf("selected mutant %q: %w", mutation.ID, err)
		}
		if mismatch.Expected == mismatch.Actual {
			return fmt.Errorf("selected mutant %q expected and actual mismatch states are identical", mutation.ID)
		}
	}
	return nil
}

func (state semanticState) validate(label string) error {
	if err := validateToken(label+" mismatch subject", state.Subject); err != nil {
		return err
	}
	return validateToken(label+" mismatch state", state.State)
}

func validateUniqueTokens(label string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s is empty", label)
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if err := validateToken(label, value); err != nil {
			return err
		}
		if seen[value] {
			return fmt.Errorf("%s contains duplicate %q", label, value)
		}
		seen[value] = true
	}
	return nil
}

func validateToken(label, value string) error {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
		return fmt.Errorf("%s %q is empty or has surrounding whitespace", label, value)
	}
	return nil
}

func sortedSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}
