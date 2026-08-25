// SPDX-License-Identifier: MPL-2.0

package subgatecensus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadRoundTripUnderStrictJSONV2 verifies that a Manifest written by
// json.Marshal (v1) decodes successfully under the json/v2-backed strict
// reader. Field-name matching is case-sensitive; the writer emits snake_case
// tags matching the struct tags read here.
func TestLoadRoundTripUnderStrictJSONV2(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		FormatVersion: FormatVersion,
		Runs: []Run{
			{
				ID:       "run-a",
				Packages: []string{"github.com/invowk/invowk/tools/goplint/internal/racerepeat"},
				Tests:    []string{"TestA"},
				Count:    1,
			},
		},
		Populations: []Population{
			{
				ID: "cases",
				Selectors: []Selector{
					{
						Run:     "run-a",
						Scope:   ScopeAllTests,
						Members: []string{"TestA"},
					},
				},
			},
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "census.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.FormatVersion != manifest.FormatVersion || len(got.Runs) != len(manifest.Runs) {
		t.Fatalf("Load() format=%d runs=%d, want format=%d runs=%d",
			got.FormatVersion, len(got.Runs), manifest.FormatVersion, len(manifest.Runs))
	}
}

// TestLoadRejectsDuplicateKeys verifies the json/v2 duplicate-object member
// policy for the subgate-census manifest reader.
func TestLoadRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"format_version":1,"format_version":2,"runs":[],"populations":[]}`)
	path := filepath.Join(t.TempDir(), "census.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() accepted a duplicate object member; want a rejection error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Load() error = %v, want message naming duplicate members", err)
	}
}
