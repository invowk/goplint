// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestReadTaskLedgerRecordsExactPendingIDs(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tasks.md")
	content := "## Tasks\n\n- [x] 1.1 Complete\n- [ ] 1.2 Pending\n- [X] 2.1 Complete too\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := readTaskLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Total != 3 || identity.Completed != 2 || !slices.Equal(identity.PendingIDs, []string{"1.2"}) || identity.SHA256 == "" {
		t.Fatalf("readTaskLedger() = %+v", identity)
	}
}

func TestCollectorsRejectUnexpectedPendingTaskAndWrongToolVersion(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, root, "tasks.md", "- [ ] 10.7 Proof\n- [ ] 10.8 Archive\n")
	plan := Plan{
		TaskLedgers: []TaskLedgerPlan{
			{Name: "follow-up", Path: "tasks.md", ExpectedPending: []string{"10.8"}},
		},
		Toolchain: []ToolPlan{
			{Name: "go", Command: []string{"go", "version"}, RequiredVersionRE: `^definitely-not-the-current-version$`},
		},
	}
	if ledgers, err := collectTaskLedgers(root, plan); err == nil {
		t.Fatalf("collectTaskLedgers() = %+v, want unexpected-pending error", ledgers)
	}
	if tools, err := collectToolchain(t.Context(), root, plan); err == nil {
		t.Fatalf("collectToolchain() = %+v, want version error", tools)
	}
}

func TestReadTaskLedgerRejectsMalformedOrDuplicateTasks(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"- [maybe] 1.1 Invalid\n",
		"- [ ] 1.1 First\n- [x] 1.1 Duplicate\n",
	} {
		path := filepath.Join(t.TempDir(), "tasks.md")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if identity, err := readTaskLedger(path); err == nil {
			t.Fatalf("readTaskLedger() = %+v, want error", identity)
		}
	}
}
