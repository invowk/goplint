// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// CallerSnapshot is a byte-sensitive identity for the caller's real index and
// worktree inventory. Git administrative state is deliberately excluded.
type CallerSnapshot struct {
	IndexSHA256    string
	WorktreeSHA256 string
}

// SnapshotCallerState captures the caller state while excluding authorized
// recorder output paths. Exclusions must be repository-relative slash paths.
func SnapshotCallerState(ctx context.Context, root string, excludedPaths ...string) (CallerSnapshot, error) {
	absoluteRoot, err := repositoryRoot(ctx, root)
	if err != nil {
		return CallerSnapshot{}, err
	}
	excluded := make(map[string]bool, len(excludedPaths))
	for _, path := range excludedPaths {
		if err := validateRepoPath(path); err != nil {
			return CallerSnapshot{}, fmt.Errorf("snapshot exclusion: %w", err)
		}
		excluded[path] = true
	}
	// The index identity is its staged content — paths, modes, blob objects,
	// and stages — not the raw index file bytes: concurrent read-only git
	// commands legitimately refresh the stat cache without changing what is
	// staged, while any real staging change alters this listing.
	staged, err := runCommand(ctx, absoluteRoot, nil, nil, "git", "ls-files", "--stage", "-z")
	if err != nil {
		return CallerSnapshot{}, fmt.Errorf("snapshot caller index: %w", err)
	}
	indexDigest := digestBytes(staged)
	worktreeDigest, err := snapshotWorktree(ctx, absoluteRoot, excluded)
	if err != nil {
		return CallerSnapshot{}, err
	}
	return CallerSnapshot{IndexSHA256: indexDigest, WorktreeSHA256: worktreeDigest}, nil
}

// snapshotWorktree inventories the caller's git-visible worktree: every
// tracked path and every untracked, non-ignored path. Ignored artifacts —
// build outputs, caches, editor and agent scratch state — are outside the
// proof identity by construction (the diff census and synthetic tree never
// admit them), and unrelated processes may legitimately write them while
// evidence is generated, so they must not poison the preservation guarantee.
func snapshotWorktree(ctx context.Context, root string, excluded map[string]bool) (string, error) {
	listing, err := runCommand(
		ctx,
		root,
		nil,
		nil,
		"git",
		"ls-files",
		"-z",
		"--cached",
		"--others",
		"--exclude-standard",
	)
	if err != nil {
		return "", fmt.Errorf("enumerate caller worktree files: %w", err)
	}
	paths := make([]string, 0, 256)
	seen := make(map[string]bool, 256)
	for rawPath := range bytes.SplitSeq(listing, []byte{0}) {
		if len(rawPath) == 0 {
			continue
		}
		path := string(rawPath)
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	var inventory bytes.Buffer
	for _, relative := range paths {
		if excluded[relative] {
			continue
		}
		skip := false
		for excludedPath := range excluded {
			if strings.HasPrefix(relative, excludedPath+"/") {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		absolute := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(absolute)
		switch {
		case os.IsNotExist(err):
			// Tracked but deleted from the worktree: record the deletion.
			fmt.Fprintf(&inventory, "M %s\n", relative)
		case err != nil:
			return "", fmt.Errorf("inspect worktree path %q: %w", relative, err)
		case info.Mode().IsRegular():
			digest, digestErr := digestFile(absolute)
			if digestErr != nil {
				return "", fmt.Errorf("digest worktree file %q: %w", relative, digestErr)
			}
			fmt.Fprintf(&inventory, "F %08x %d %s %s\n", uint32(info.Mode()), info.Size(), digest, relative)
		case info.Mode()&fs.ModeSymlink != 0:
			target, linkErr := os.Readlink(absolute)
			if linkErr != nil {
				return "", fmt.Errorf("read worktree symlink %q: %w", relative, linkErr)
			}
			fmt.Fprintf(&inventory, "L %08x %s %s\n", uint32(info.Mode()), digestBytes([]byte(target)), relative)
		default:
			return "", fmt.Errorf("unsupported worktree file mode %s at %q", info.Mode(), relative)
		}
	}
	return digestBytes(inventory.Bytes()), nil
}

func digestFile(path string) (string, error) {
	if err := requireRegularFile(path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", path, err)
	}
	return digestBytes(data), nil
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect path %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path %s is not a regular file", path)
	}
	return nil
}
