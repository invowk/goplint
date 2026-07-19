// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	indexPath, err := gitOutput(
		ctx,
		absoluteRoot,
		nil,
		nil,
		"rev-parse",
		"--path-format=absolute",
		"--git-path",
		"index",
	)
	if err != nil {
		return CallerSnapshot{}, err
	}
	indexDigest, err := digestFileOrMissing(indexPath)
	if err != nil {
		return CallerSnapshot{}, fmt.Errorf("snapshot caller index: %w", err)
	}
	worktreeDigest, err := snapshotWorktree(absoluteRoot, excluded)
	if err != nil {
		return CallerSnapshot{}, err
	}
	return CallerSnapshot{IndexSHA256: indexDigest, WorktreeSHA256: worktreeDigest}, nil
}

func snapshotWorktree(root string, excluded map[string]bool) (string, error) {
	var inventory bytes.Buffer
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relativize worktree path %q: %w", path, err)
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if excluded[relative] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		for excludedPath := range excluded {
			if strings.HasPrefix(relative, excludedPath+"/") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect worktree path %q: %w", relative, err)
		}
		mode := uint32(info.Mode())
		switch {
		case info.Mode().IsDir():
			fmt.Fprintf(&inventory, "D %08x %s\n", mode, relative)
		case info.Mode().IsRegular():
			digest, err := digestFile(path)
			if err != nil {
				return fmt.Errorf("digest worktree file %q: %w", relative, err)
			}
			fmt.Fprintf(&inventory, "F %08x %d %s %s\n", mode, info.Size(), digest, relative)
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read worktree symlink %q: %w", relative, err)
			}
			fmt.Fprintf(&inventory, "L %08x %s %s\n", mode, digestBytes([]byte(target)), relative)
		default:
			return fmt.Errorf("unsupported worktree file mode %s at %q", info.Mode(), relative)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("snapshot caller worktree: %w", err)
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

func digestFileOrMissing(path string) (string, error) {
	digest, err := digestFile(path)
	if err == nil {
		return digest, nil
	}
	if os.IsNotExist(err) {
		return "missing", nil
	}
	return "", err
}
