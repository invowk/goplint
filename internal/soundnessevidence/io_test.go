// SPDX-License-Identifier: MPL-2.0

package soundnessevidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRegistryRejectsUnknownAndTrailingJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantError string
	}{
		{
			name:      "unknown field",
			content:   `{"format_version":2,"registrations":[],"marker_only":true}`,
			wantError: "unknown field",
		},
		{
			name:      "trailing object",
			content:   `{"format_version":2,"registrations":[]} {}`,
			wantError: "multiple JSON values",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "registry.json")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			_, err := LoadRegistry(t.Context(), path)
			assertErrorContains(t, err, test.wantError)
		})
	}
}

func TestLoadObservationsRejectsNonEvidenceArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("PASS\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := LoadObservations(t.Context(), root)
	assertErrorContains(t, err, "not a JSON output")
}

func TestLoadObservationsRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "forged.json"), []byte(`{"marker_only":"PASS"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := LoadObservations(t.Context(), root)
	assertErrorContains(t, err, "unknown field")
}

func TestEmitObservationDisabledWithoutAggregateEnvironment(t *testing.T) {
	t.Parallel()

	path, err := emitObservation(t.Context(), SemanticObservation{}, func(string) (string, bool) {
		return "", false
	})
	if err != nil {
		t.Fatalf("emitObservation() error = %v", err)
	}
	if path != "" {
		t.Fatalf("emitObservation() path = %q, want empty", path)
	}
}

func TestEmitObservationRoundTrip(t *testing.T) {
	directory := t.TempDir()
	binding := validTestBinding()
	t.Setenv(EnvEvidenceDir, directory)
	t.Setenv(EnvRunID, binding.RunID)
	t.Setenv(EnvWorkspaceDigest, binding.WorkspaceDigest)
	t.Setenv(EnvManifestDigest, binding.ManifestDigest)
	t.Setenv(EnvCommandDigest, binding.CommandDigest)
	t.Setenv(EnvSubgateID, binding.SubgateID)

	observation := validTestObservation()
	observation.FormatVersion = 0
	observation.Binding = ObservationBinding{}
	path, err := EmitObservationFromEnvironment(t.Context(), observation)
	if err != nil {
		t.Fatalf("EmitObservationFromEnvironment() error = %v", err)
	}
	if filepath.Dir(path) != directory {
		t.Fatalf("EmitObservationFromEnvironment() path = %q, want directory %q", path, directory)
	}
	loaded, err := LoadObservations(t.Context(), directory)
	if err != nil {
		t.Fatalf("LoadObservations() error = %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("LoadObservations() count = %d, want 1", len(loaded))
	}
	encoded, err := json.Marshal(loaded[0])
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want, err := json.Marshal(validTestObservation())
	if err != nil {
		t.Fatalf("Marshal() want error = %v", err)
	}
	if string(encoded) != string(want) {
		t.Fatalf("round-trip observation = %s, want %s", encoded, want)
	}
}

func TestEmitObservationRejectsMissingBindingVariable(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	binding := validTestBinding()
	values := map[string]string{
		EnvEvidenceDir:     directory,
		EnvRunID:           binding.RunID,
		EnvWorkspaceDigest: binding.WorkspaceDigest,
		EnvManifestDigest:  binding.ManifestDigest,
		EnvCommandDigest:   binding.CommandDigest,
		EnvSubgateID:       binding.SubgateID,
	}
	delete(values, EnvCommandDigest)
	observation := validTestObservation()
	observation.Binding = ObservationBinding{}
	_, err := emitObservation(t.Context(), observation, func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	})
	assertErrorContains(t, err, EnvCommandDigest+" is unset")
}
