// SPDX-License-Identifier: MPL-2.0

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

func TestLoadMutationStagesDerivesCanonicalCategoryUnion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "mutations.json")
	data := []byte(`{
  "format_version": 2,
  "mutations": [
    {
      "id": "cast/propagation",
      "categories": ["unvalidated-cast"],
      "changed_stages": ["propagation"]
    },
    {
      "id": "cast/reporting",
      "categories": ["unvalidated-cast"],
      "changed_stages": ["reporting"]
    }
  ]
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write mutation manifest: %v", err)
	}

	got, err := loadMutationStages(path)
	if err != nil {
		t.Fatalf("loadMutationStages() error = %v", err)
	}
	want := []soundnessevidence.ExecutionStage{
		soundnessevidence.StagePropagation,
		soundnessevidence.StageReporting,
	}
	if !slices.Equal(got["unvalidated-cast"], want) {
		t.Fatalf("unvalidated-cast stages = %v, want %v", got["unvalidated-cast"], want)
	}
}

func TestValidateMutationStageCoverageRejectsMissingCategory(t *testing.T) {
	t.Parallel()

	catalog := semanticCatalog{Categories: []semanticCategory{{
		Category:       "unvalidated-cast",
		Kind:           protocolSemanticKind,
		RequiredLayers: []string{string(soundnessevidence.LayerMutation)},
	}}}
	err := validateMutationStageCoverage(catalog, nil)
	if err == nil || !strings.Contains(err.Error(), "unvalidated-cast") {
		t.Fatalf("validateMutationStageCoverage() error = %v, want missing category", err)
	}
}

func TestBuildMutationRegistrationUsesManifestStages(t *testing.T) {
	t.Parallel()

	want := []soundnessevidence.ExecutionStage{
		soundnessevidence.StageIdentity,
		soundnessevidence.StageReporting,
	}
	registration, err := buildRegistration(
		"unvalidated-cast",
		"cast-validation",
		soundnessevidence.LayerMutation,
		map[string][]soundnessevidence.ExecutionStage{"unvalidated-cast": want},
	)
	if err != nil {
		t.Fatalf("buildRegistration() error = %v", err)
	}
	if !slices.Equal(registration.Expected.RequiredStages, want) {
		t.Fatalf("required stages = %v, want %v", registration.Expected.RequiredStages, want)
	}
}
