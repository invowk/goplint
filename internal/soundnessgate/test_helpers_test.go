// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

func validGateRegistry() soundnessevidence.Registry {
	maximumDiagnostics := 1
	registrations := []soundnessevidence.Registration{
		{
			ID:         "cast-validation.production",
			Category:   "invalid-cast-validation",
			Layer:      soundnessevidence.LayerProduction,
			FeatureID:  "cast-validation",
			ProducerID: "semantic-production-a",
			TestID:     "TestSemanticProduction/cast-validation",
			Boundary:   soundnessevidence.BoundaryProductionAnalyzer,
			Expected: soundnessevidence.Expectation{
				Outcome:            soundnessevidence.OutcomeMustReport,
				MinimumCases:       1,
				Diagnostics:        soundnessevidence.CountRange{Minimum: 1, Maximum: &maximumDiagnostics},
				RequiredStages:     []soundnessevidence.ExecutionStage{soundnessevidence.StageSourceExtraction, soundnessevidence.StageReporting},
				RequiredProperties: []string{"diagnostic-emitted"},
				RequiredDimensions: []string{"casts"},
			},
		},
		{
			ID:         "use-before-validation.production",
			Category:   "unvalidated-use",
			Layer:      soundnessevidence.LayerProduction,
			FeatureID:  "use-before-validation",
			ProducerID: "semantic-production-b",
			TestID:     "TestSemanticProduction/use-before-validation",
			Boundary:   soundnessevidence.BoundaryProductionAnalyzer,
			Expected: soundnessevidence.Expectation{
				Outcome:            soundnessevidence.OutcomeMustReport,
				MinimumCases:       1,
				Diagnostics:        soundnessevidence.CountRange{Minimum: 1, Maximum: &maximumDiagnostics},
				RequiredStages:     []soundnessevidence.ExecutionStage{soundnessevidence.StageSourceExtraction, soundnessevidence.StageReporting},
				RequiredProperties: []string{"diagnostic-emitted"},
				RequiredDimensions: []string{"uses"},
			},
		},
	}
	return soundnessevidence.Registry{
		FormatVersion: soundnessevidence.RegistryFormatVersion,
		Registrations: registrations,
	}
}

func validGateManifest() Manifest {
	return Manifest{
		FormatVersion: ManifestFormatVersion,
		RegistryPath:  "spec/semantic-evidence.v2.json",
		Profiles: []Profile{
			{
				ID:         ProfileCore,
				SubgateIDs: []string{"semantic-production-a", "semantic-production-b", "targeted-mutation"},
			},
			{
				ID:         ProfileComplete,
				SubgateIDs: []string{"clean-tree-freshness", "semantic-production-a", "semantic-production-b", "targeted-mutation"},
			},
		},
		Subgates: []Subgate{
			{
				ID:                      "clean-tree-freshness",
				WorkingDirectory:        ".",
				Command:                 []string{"producer", "clean-tree-freshness"},
				TimeoutSeconds:          10,
				ReportFile:              "report.json",
				RequiredRegistrationIDs: []string{},
				RequiredPopulations:     []PopulationRequirement{{ID: "verified-clean-tree-records", Minimum: 1}},
			},
			{
				ID:                      "semantic-production-a",
				WorkingDirectory:        ".",
				Command:                 []string{"producer", "semantic-production-a"},
				TimeoutSeconds:          10,
				ReportFile:              "report.json",
				RequiredRegistrationIDs: []string{"cast-validation.production"},
				RequiredPopulations:     []PopulationRequirement{{ID: "cases", Minimum: 1}},
			},
			{
				ID:                      "semantic-production-b",
				WorkingDirectory:        ".",
				Command:                 []string{"producer", "semantic-production-b"},
				TimeoutSeconds:          10,
				ReportFile:              "report.json",
				RequiredRegistrationIDs: []string{"use-before-validation.production"},
				RequiredPopulations:     []PopulationRequirement{{ID: "cases", Minimum: 1}},
			},
			{
				ID:                      "targeted-mutation",
				WorkingDirectory:        ".",
				Command:                 []string{"producer", "targeted-mutation"},
				TimeoutSeconds:          10,
				ReportFile:              "report.json",
				RequiredRegistrationIDs: []string{},
				RequiredPopulations:     []PopulationRequirement{{ID: "cases", Minimum: 1}},
			},
		},
	}
}

func validGateBinding(subgate Subgate) soundnessevidence.ObservationBinding {
	commandDigest, err := CommandDigest(subgate)
	if err != nil {
		panic(err)
	}
	return soundnessevidence.ObservationBinding{
		RunID:           "run-test",
		WorkspaceDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestDigest:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		CommandDigest:   commandDigest,
		SubgateID:       subgate.ID,
	}
}

func validGateObservation(
	registration soundnessevidence.Registration,
	binding soundnessevidence.ObservationBinding,
) soundnessevidence.SemanticObservation {
	cases := []soundnessevidence.SemanticCase{
		{
			ID:        registration.ID + "/case-001",
			Category:  registration.Category,
			Layer:     registration.Layer,
			FeatureID: registration.FeatureID,
			Boundary:  registration.Boundary,
			ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
				registration.Boundary,
			),
			Outcome:         registration.Expected.Outcome,
			DiagnosticCount: registration.Expected.Diagnostics.Minimum,
			Stages:          append([]soundnessevidence.ExecutionStage(nil), registration.Expected.RequiredStages...),
			Properties:      append([]string(nil), registration.Expected.RequiredProperties...),
			Dimensions:      append([]string(nil), registration.Expected.RequiredDimensions...),
		},
	}
	observation, err := soundnessevidence.ObservationFromCases(
		registration.ID,
		registration.ProducerID,
		registration.TestID,
		cases,
	)
	if err != nil {
		panic("ObservationFromCases(): " + err.Error())
	}
	observation.Binding = binding
	return observation
}

func writeGateJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
