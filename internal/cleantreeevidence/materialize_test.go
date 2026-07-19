// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMaterializeUsesSelectedContentAndPreservesCaller(t *testing.T) {
	t.Parallel()

	root := initializeTestRepository(t)
	writeTestFile(t, root, "tracked.txt", "changed\n")
	writeTestFile(t, root, "untracked.txt", "new\n")
	pathsPath := writeTestFile(t, root, "paths.txt", "tracked.txt\nuntracked.txt\n")
	before, err := SnapshotCallerState(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	materialization, err := Materialize(t.Context(), root, pathsPath, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := materialization.Close(t.Context()); err != nil {
			t.Errorf("close materialization: %v", err)
		}
	})
	assertTestFile(t, materialization.Worktree, "tracked.txt", "changed\n")
	assertTestFile(t, materialization.Worktree, "untracked.txt", "new\n")
	if materialization.Identity.BaseCommit == "" || materialization.Identity.SyntheticTree == "" || materialization.Identity.DiffSHA256 == "" {
		t.Fatalf("incomplete repository identity: %+v", materialization.Identity)
	}
	after, err := SnapshotCallerState(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("caller state changed: before=%+v after=%+v", before, after)
	}
}

func TestMaterializeIdentityChangesOnlyForSelectedContent(t *testing.T) {
	t.Parallel()

	root := initializeTestRepository(t)
	pathsPath := writeTestFile(t, root, "paths.txt", "tracked.txt\n")
	first, err := Materialize(t.Context(), root, pathsPath, false)
	if err != nil {
		t.Fatal(err)
	}
	firstIdentity := first.Identity
	if err := first.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "ignored.txt", "not selected\n")
	second, err := Materialize(t.Context(), root, pathsPath, false)
	if err != nil {
		t.Fatal(err)
	}
	secondIdentity := second.Identity
	if err := second.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if firstIdentity.SyntheticTree != secondIdentity.SyntheticTree {
		t.Fatalf("unselected content changed tree: %s != %s", firstIdentity.SyntheticTree, secondIdentity.SyntheticTree)
	}
	writeTestFile(t, root, "tracked.txt", "selected drift\n")
	third, err := Materialize(t.Context(), root, pathsPath, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := third.Close(t.Context()); err != nil {
			t.Errorf("close materialization: %v", err)
		}
	})
	if firstIdentity.SyntheticTree == third.Identity.SyntheticTree || firstIdentity.DiffSHA256 == third.Identity.DiffSHA256 {
		t.Fatalf("selected drift did not change identity: before=%+v after=%+v", firstIdentity, third.Identity)
	}
}

func TestMaterializationCloseCompletesAfterCallerCancellation(t *testing.T) {
	t.Parallel()

	root := initializeTestRepository(t)
	pathsPath := writeTestFile(t, root, "paths.txt", "tracked.txt\n")
	materialization, err := Materialize(t.Context(), root, pathsPath, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := materialization.Close(t.Context()); err != nil {
			t.Errorf("close materialization: %v", err)
		}
	})
	tempRoot := materialization.tempRoot
	canceledCtx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := materialization.Close(canceledCtx); err != nil {
		t.Fatalf("Close() after cancellation: %v", err)
	}
	if _, err := os.Stat(tempRoot); !os.IsNotExist(err) {
		t.Fatalf("temporary root still exists after Close(): %v", err)
	}
}

func TestLoadPathSelectionRejectsAmbiguousScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "empty"},
		{name: "comment", content: "# all files\n"},
		{name: "absolute", content: "/tmp/file\n"},
		{name: "parent", content: "../file\n"},
		{name: "git metadata", content: ".git/index\n"},
		{name: "duplicate", content: "Makefile\nMakefile\n"},
		{name: "unsorted", content: "tools/goplint\nMakefile\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "paths")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if paths, err := LoadPathSelection(path); err == nil {
				t.Fatalf("LoadPathSelection() = %q, want error", paths)
			}
		})
	}
}

func initializeTestRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runTestGit(t, root, "init", "--quiet")
	writeTestFile(t, root, "tracked.txt", "original\n")
	runTestGit(t, root, "add", "tracked.txt")
	command := exec.CommandContext(t.Context(), "git", "commit", "--quiet", "-m", "initial")
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@invalid",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@invalid",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}
	return root
}

func runTestGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func writeTestFile(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertTestFile(t *testing.T, root, relative, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", relative, data, want)
	}
}
