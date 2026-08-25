// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoadTimingManifestRoundTripUnderStrictJSONV2 verifies that a manifest
// written by json.Marshal (v1) decodes successfully under the json/v2-backed
// strict reader. Field-name matching is case-sensitive under v2; the writer
// emits snake_case tags that match the struct tags read here.
func TestLoadTimingManifestRoundTripUnderStrictJSONV2(t *testing.T) {
	t.Parallel()

	manifest := TimingManifest{
		FormatVersion:            TimingFormatVersion,
		Package:                  "./goplint",
		Toolchain:                "go1.27.0",
		GeneratedAt:              time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
		DefaultWeightNanoseconds: 20,
		ReviewedInternalShardIDs: []string{},
		Environment:              []string{ScheduledOracleEnvironment},
		Entries: []TimingEntry{
			{ID: "TestA", Kind: KindTest, DurationWeightNanoseconds: 10, SampleCount: 1},
			{ID: "TestB", Kind: KindTest, DurationWeightNanoseconds: 20, SampleCount: 1},
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "timings.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got, err := LoadTimingManifest(path)
	if err != nil {
		t.Fatalf("LoadTimingManifest() error = %v", err)
	}
	if got.Toolchain != manifest.Toolchain || len(got.Entries) != len(manifest.Entries) {
		t.Fatalf("LoadTimingManifest() toolchain=%q entries=%d, want %q entries=%d",
			got.Toolchain, len(got.Entries), manifest.Toolchain, len(manifest.Entries))
	}
}

// TestLoadTimingManifestRejectsDuplicateKeys verifies the json/v2
// duplicate-object member policy for the timing manifest reader.
func TestLoadTimingManifestRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"format_version":1,"format_version":2,"package":"./goplint","toolchain":"go1.27.0"}`)
	path := filepath.Join(t.TempDir(), "timings.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	_, err := LoadTimingManifest(path)
	if err == nil {
		t.Fatal("LoadTimingManifest() accepted a duplicate object member; want a rejection error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("LoadTimingManifest() error = %v, want message naming duplicate members", err)
	}
}
