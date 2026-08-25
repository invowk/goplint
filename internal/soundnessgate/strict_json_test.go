// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadManifestRoundTripUnderStrictJSONV2 verifies that a manifest written
// by json.Marshal (v1) decodes successfully under the json/v2-backed strict
// reader. Field-name casing is case-sensitive under v2; this exercises the
// self-produced-record casing contract for LoadManifest.
func TestLoadManifestRoundTripUnderStrictJSONV2(t *testing.T) {
	t.Parallel()

	manifest := validGateManifest()
	path := writeTempJSON(t, manifest)
	got, _, err := LoadManifest(t.Context(), path)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if got.FormatVersion != manifest.FormatVersion || len(got.Profiles) != len(manifest.Profiles) {
		t.Fatalf("LoadManifest() format=%d profiles=%d, want format=%d profiles=%d",
			got.FormatVersion, len(got.Profiles), manifest.FormatVersion, len(manifest.Profiles))
	}
}

// TestLoadManifestRejectsDuplicateKeys verifies the json/v2 duplicate-object
// member policy for the aggregate soundness manifest reader.
func TestLoadManifestRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"format_version":1,"format_version":2,"registry_path":"r","profiles":[],"subgates":[]}`)
	path := writeTempBytes(t, payload)
	_, _, err := LoadManifest(t.Context(), path)
	if err == nil {
		t.Fatal("LoadManifest() accepted a duplicate object member; want a rejection error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("LoadManifest() error = %v, want message naming duplicate members", err)
	}
}

// TestLoadReportRejectsDuplicateKeys verifies the json/v2 duplicate-object
// member policy for the subgate-report reader.
func TestLoadReportRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"format_version":1,"format_version":2,"binding":{},"status":"passed","populations":[]}`)
	path := writeTempBytes(t, payload)
	_, err := LoadReport(t.Context(), path)
	if err == nil {
		t.Fatal("LoadReport() accepted a duplicate object member; want a rejection error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("LoadReport() error = %v, want message naming duplicate members", err)
	}
}

// TestLoadManifestRejectsCaseMismatchedTags exercises v2's case-sensitive
// field-name matching. A field emitted with an uppercase tag by an external
// producer would silently drop rather than populate the struct field. This
// test pins fail-closed behavior: an unknown-cased key must be rejected by
// RejectUnknownMembers even though case-insensitive matching would have
// mapped it in v1.
func TestLoadManifestRejectsCaseMismatchedTags(t *testing.T) {
	t.Parallel()

	// Same shape as writeTempJSON output but with an uppercased field name;
	// v1 would silently accept via case-insensitive matching and read the
	// value, v2's case-sensitive default plus RejectUnknownMembers rejects
	// the record.
	payload := []byte(`{"Format_Version":1,"registry_path":"r","profiles":[],"subgates":[]}`)
	path := writeTempBytes(t, payload)
	if _, _, err := LoadManifest(t.Context(), path); err == nil {
		t.Fatal("LoadManifest() accepted a case-mismatched field; want a rejection error")
	}
}

func writeTempJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal test manifest: %v", err)
	}
	return writeTempBytes(t, data)
}

func writeTempBytes(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write test record: %v", err)
	}
	return path
}
