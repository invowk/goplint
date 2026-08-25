// SPDX-License-Identifier: MPL-2.0

package profileownership

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDecodeRoundTripUnderStrictJSONV2 verifies that a Manifest written by
// json.Marshal (v1) decodes successfully under the json/v2-backed strict
// reader. Field-name matching is case-sensitive; the writer emits snake_case
// tags matching the struct tags read here.
func TestDecodeRoundTripUnderStrictJSONV2(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		FormatVersion: FormatVersion,
		// Rules are checked in lexical order; keep the sample canonical so
		// the round-trip decode also passes Validate().
		Rules: []Rule{
			{Pattern: "docs/goplint/README.md", Class: ClassDocumentation},
			{Pattern: "tools/goplint/", Class: ClassAnalyzerSemantics},
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.FormatVersion != manifest.FormatVersion || len(got.Rules) != len(manifest.Rules) {
		t.Fatalf("Decode() format=%d rules=%d, want format=%d rules=%d",
			got.FormatVersion, len(got.Rules), manifest.FormatVersion, len(manifest.Rules))
	}
}

// TestDecodeRejectsDuplicateKeys verifies the json/v2 duplicate-object member
// policy for the ownership-manifest reader.
func TestDecodeRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"format_version":2,"format_version":1,"rules":[]}`)
	_, err := Decode(payload)
	if err == nil {
		t.Fatal("Decode() accepted a duplicate object member; want a rejection error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Decode() error = %v, want message naming duplicate members", err)
	}
}
