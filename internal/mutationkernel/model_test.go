// SPDX-License-Identifier: MPL-2.0

package mutationkernel

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadRepositoryContract(t *testing.T) {
	t.Parallel()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error: %v", err)
	}
	root := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	result, err := Load(
		t.Context(),
		root,
		"testdata/subgates/mutation-kernel-coverage.v1.json",
	)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if result.CategoryCount != 8 || result.SelectedMutantCount != 27 || result.BindingCount != 110 {
		t.Fatalf("Load() counts = categories %d, mutants %d, bindings %d", result.CategoryCount,
			result.SelectedMutantCount, result.BindingCount)
	}
	wantCategories := []string{
		"missing-constructor-validate",
		"missing-constructor-validate-inconclusive",
		"unvalidated-boundary-request",
		"unvalidated-cast",
		"unvalidated-cast-inconclusive",
		"use-before-validate-cross-block",
		"use-before-validate-inconclusive",
		"use-before-validate-same-block",
	}
	gotCategories := make([]string, 0, len(result.Observations))
	for _, observation := range result.Observations {
		gotCategories = append(gotCategories, observation.Category)
		if len(observation.MutantIDs) == 0 || len(observation.ChangedStages) == 0 ||
			len(observation.ExpectedMismatchAssertionIDs) == 0 {
			t.Errorf("observation for %q has incomplete causal metadata: %+v", observation.Category, observation)
		}
	}
	if !reflect.DeepEqual(gotCategories, wantCategories) {
		t.Errorf("Load() categories = %v, want %v", gotCategories, wantCategories)
	}
	populations, err := result.Populations()
	if err != nil {
		t.Fatalf("Populations() error: %v", err)
	}
	wantPopulations := map[string]int{
		PopulationBindings:   110,
		PopulationCategories: 8,
		PopulationMutants:    27,
	}
	for _, population := range populations {
		if wantPopulations[population.ID] != population.Count {
			t.Errorf("population %q count = %d, want %d", population.ID, population.Count,
				wantPopulations[population.ID])
		}
		delete(wantPopulations, population.ID)
	}
	if len(wantPopulations) != 0 {
		t.Errorf("missing populations: %v", wantPopulations)
	}
}

func TestEvaluateListsEveryUncoveredRequiredCategory(t *testing.T) {
	t.Parallel()

	definition, rules, profile, catalog := validFixture()
	rules.CategoryCatalog = append(rules.CategoryCatalog,
		semanticCategory{Category: "category-b", RequiredLayers: []string{mutationLayer}},
		semanticCategory{Category: "category-c", RequiredLayers: []string{mutationLayer}},
	)
	catalog.Mutations[0].Categories = []string{"category-c"}
	_, err := evaluate(definition, rules, profile, catalog)
	if err == nil || !strings.Contains(err.Error(), "category-a, category-b") {
		t.Fatalf("evaluate() error = %v, want both uncovered categories", err)
	}
}

func TestEvaluateRejectsUnknownCategoryMetadata(t *testing.T) {
	t.Parallel()

	definition, rules, profile, catalog := validFixture()
	catalog.Mutations[0].Categories = append(catalog.Mutations[0].Categories, "unknown-category")
	_, err := evaluate(definition, rules, profile, catalog)
	if err == nil || !strings.Contains(err.Error(), "unknown semantic category") {
		t.Fatalf("evaluate() error = %v, want unknown category failure", err)
	}
}

func TestEvaluateRejectsMissingCausalMetadata(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		mutate func(*mutationRecord)
		want   string
	}{
		{
			name: "changed stages",
			mutate: func(mutation *mutationRecord) {
				mutation.ChangedStages = nil
			},
			want: "changed_stages is empty",
		},
		{
			name: "expected mismatches",
			mutate: func(mutation *mutationRecord) {
				mutation.Guard.ExpectedMismatches = nil
			},
			want: "has no expected mismatches",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			definition, rules, profile, catalog := validFixture()
			testCase.mutate(&catalog.Mutations[0])
			_, err := evaluate(definition, rules, profile, catalog)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("evaluate() error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestLoadRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "unknown field",
			content: `{
  "format_version": 1,
  "semantic_rules": "rules.json",
  "blocking_profile": "profile.json",
  "mutant_catalog": "mutants.json",
  "unexpected": true
}`,
			want: "unknown field",
		},
		{
			name: "trailing value",
			content: `{
  "format_version": 1,
  "semantic_rules": "rules.json",
  "blocking_profile": "profile.json",
  "mutant_catalog": "mutants.json"
}
{}`,
			want: "trailing JSON value",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(testCase.content), 0o600); err != nil {
				t.Fatalf("os.WriteFile() error: %v", err)
			}
			_, err := Load(t.Context(), root, "manifest.json")
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("Load() error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestPopulationsRejectsInconsistentCensusCounts(t *testing.T) {
	t.Parallel()

	result := Result{
		CategoryCount:       1,
		SelectedMutantCount: 1,
		BindingCount:        2,
		Observations: []Observation{{
			Category:  "category-a",
			MutantIDs: []string{"mutant-a"},
		}},
	}
	_, err := result.Populations()
	if err == nil || !strings.Contains(err.Error(), "binding_count") {
		t.Fatalf("Populations() error = %v, want binding count failure", err)
	}
}

func validFixture() (manifest, semanticRulesManifest, mutationProfile, mutationCatalog) {
	definition := manifest{
		FormatVersion:   manifestFormatVersion,
		SemanticRules:   "rules.json",
		BlockingProfile: "profile.json",
		MutantCatalog:   "mutants.json",
	}
	rules := semanticRulesManifest{
		Version: semanticRulesVersion,
		CategoryCatalog: []semanticCategory{{
			Category:       "category-a",
			RequiredLayers: []string{mutationLayer},
		}},
	}
	profile := mutationProfile{
		FormatVersion: mutationDataFormatVersion,
		Manifest:      definition.MutantCatalog,
		Count:         2,
		MutationIDs:   []string{"mutant-a"},
	}
	catalog := mutationCatalog{
		FormatVersion: mutationDataFormatVersion,
		Profile:       "test-profile",
		Mutations: []mutationRecord{{
			ID:            "mutant-a",
			Concern:       "exercise category-a",
			Categories:    []string{"category-a"},
			ChangedStages: []string{"propagation"},
			File:          "goplint/example.go",
			Before:        "before",
			BeforeSHA256:  "before-digest",
			After:         "after",
			AfterSHA256:   "after-digest",
			Guard: mutationGuard{ExpectedMismatches: []expectedMismatch{{
				Assertion: assertionIdentity{Test: "TestCategoryA", ID: "category-a"},
				Expected:  semanticState{Subject: "category-a", State: "safe"},
				Actual:    semanticState{Subject: "category-a", State: "unsafe"},
			}}},
		}},
	}
	return definition, rules, profile, catalog
}
