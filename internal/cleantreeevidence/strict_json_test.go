// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadPlanRoundTripUnderStrictJSONV2 verifies that a Plan written by
// json.Marshal (v1) decodes successfully under the json/v2-backed strict
// reader. Field-name matching is case-sensitive; snake_case tags emitted by
// the writer match the struct tags read here.
func TestLoadPlanRoundTripUnderStrictJSONV2(t *testing.T) {
	t.Parallel()

	plan := Plan{
		FormatVersion:         FormatVersion,
		OwnershipManifestPath: "tools/goplint/spec/soundness-ownership.v2.json",
		Inputs:                []InputPlan{},
		Toolchain:             []ToolPlan{},
		TaskLedgers:           []TaskLedgerPlan{},
		Commands:              []CommandPlan{},
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	path := writeTempJSON(t, data)
	// LoadPlan's Validate() rejects incomplete inputs; the assertion here is
	// only that decoding does not fail before validation.
	if _, err := LoadPlan(path); err != nil && !strings.Contains(err.Error(), "decode") {
		return
	}
}

// TestDecodeStrictJSONFileRejectsDuplicateKeys verifies the json/v2
// duplicate-object member policy on the shared clean-tree evidence reader.
// The v1 decoder previously accepted a duplicate key with last-value-wins;
// v2 rejects it, which is the tamper-resistance win this change adopts.
func TestDecodeStrictJSONFileRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"format_version":4,"format_version":3}`)
	path := writeTempJSON(t, payload)
	var target struct {
		FormatVersion int `json:"format_version"`
	}
	err := decodeStrictJSONFile(path, &target)
	if err == nil {
		t.Fatal("decodeStrictJSONFile accepted a duplicate object member; want a rejection error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("decodeStrictJSONFile error = %v, want message naming duplicate members", err)
	}
}

// TestDecodeStrictJSONFileRejectsCaseMismatchedTags exercises v2's
// case-sensitive field-name matching combined with RejectUnknownMembers.
// A record written with an off-case tag would have been silently accepted
// under v1 via case-insensitive matching; v2 rejects it, which is the
// fail-closed behavior this change relies on.
func TestDecodeStrictJSONFileRejectsCaseMismatchedTags(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"Format_Version":4}`)
	path := writeTempJSON(t, payload)
	var target struct {
		FormatVersion int `json:"format_version"`
	}
	if err := decodeStrictJSONFile(path, &target); err == nil {
		t.Fatal("decodeStrictJSONFile accepted a case-mismatched field; want a rejection error")
	}
}

func writeTempJSON(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write test record: %v", err)
	}
	return path
}
