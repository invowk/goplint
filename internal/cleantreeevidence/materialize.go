// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	syntheticCommitDate    = "2000-01-01T00:00:00Z"
	detachedCleanupTimeout = 30 * time.Second
)

// Materialization owns the temporary index and optional detached worktree used
// to evaluate an exact selected tree.
type Materialization struct {
	Root       string
	Worktree   string
	Identity   RepositoryIdentity
	tempRoot   string
	worktreeOK bool
}

// Materialize builds HEAD plus the explicit path selection through an isolated
// temporary index. When withWorktree is true it also creates a clean detached
// worktree at the deterministic synthetic commit.
func Materialize(ctx context.Context, root, pathsPath string, withWorktree bool) (*Materialization, error) {
	return materializeFromBase(ctx, root, pathsPath, "HEAD", withWorktree)
}

// materializeFromBase builds the selected content on top of one exact base
// revision. Capture uses HEAD; verification replays the retained base so a
// later commit containing the proven tree does not invalidate its own proof.
func materializeFromBase(
	ctx context.Context,
	root string,
	pathsPath string,
	baseRevision string,
	withWorktree bool,
) (_ *Materialization, resultErr error) {
	absoluteRoot, err := repositoryRoot(ctx, root)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(baseRevision) == "" {
		return nil, errors.New("clean-tree base revision is empty")
	}
	absolutePathsPath := resolveFromRoot(absoluteRoot, pathsPath)
	paths, err := LoadPathSelection(absolutePathsPath)
	if err != nil {
		return nil, err
	}
	tempRoot, err := os.MkdirTemp("", "goplint-clean-tree-*")
	if err != nil {
		return nil, fmt.Errorf("create clean-tree temporary directory: %w", err)
	}
	materialization := &Materialization{Root: absoluteRoot, tempRoot: tempRoot}
	failed := true
	defer func() {
		if failed {
			resultErr = errors.Join(resultErr, materialization.Close(ctx))
		}
	}()

	baseCommit, err := gitOutput(ctx, absoluteRoot, nil, nil, "rev-parse", baseRevision+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve clean-tree base revision %q: %w", baseRevision, err)
	}
	indexPath := filepath.Join(tempRoot, "index")
	indexEnv := replaceEnvironmentVariable(os.Environ(), "GIT_INDEX_FILE", indexPath)
	if _, err := runCommand(ctx, absoluteRoot, indexEnv, nil, "git", "read-tree", baseCommit); err != nil {
		return nil, fmt.Errorf("initialize temporary index: %w", err)
	}
	if _, err := runCommand(
		ctx,
		absoluteRoot,
		indexEnv,
		nil,
		"git",
		"add",
		"-A",
		"--pathspec-from-file="+absolutePathsPath,
	); err != nil {
		return nil, fmt.Errorf("stage reviewed paths in temporary index: %w", err)
	}
	syntheticTree, err := gitOutput(ctx, absoluteRoot, indexEnv, nil, "write-tree")
	if err != nil {
		return nil, err
	}
	diff, err := runCommand(ctx, absoluteRoot, nil, nil, "git", "diff", "--binary", baseCommit, syntheticTree)
	if err != nil {
		return nil, fmt.Errorf("compute synthetic diff: %w", err)
	}
	commitEnv := os.Environ()
	for key, value := range map[string]string{
		"GIT_AUTHOR_NAME":     "goplint evidence",
		"GIT_AUTHOR_EMAIL":    "goplint-evidence@invalid",
		"GIT_AUTHOR_DATE":     syntheticCommitDate,
		"GIT_COMMITTER_NAME":  "goplint evidence",
		"GIT_COMMITTER_EMAIL": "goplint-evidence@invalid",
		"GIT_COMMITTER_DATE":  syntheticCommitDate,
	} {
		commitEnv = replaceEnvironmentVariable(commitEnv, key, value)
	}
	syntheticCommit, err := gitOutput(
		ctx,
		absoluteRoot,
		commitEnv,
		[]byte("goplint clean-tree evidence v3\n"),
		"commit-tree",
		syntheticTree,
		"-p",
		baseCommit,
	)
	if err != nil {
		return nil, err
	}
	materialization.Identity = RepositoryIdentity{
		BaseCommit:          baseCommit,
		SyntheticTree:       syntheticTree,
		SyntheticCommit:     syntheticCommit,
		DiffSHA256:          digestBytes(diff),
		PathSelectionSHA256: digestBytes([]byte(strings.Join(paths, "\n") + "\n")),
		PathSelection:       paths,
	}
	if withWorktree {
		materialization.Worktree = filepath.Join(tempRoot, "worktree")
		if _, err := runCommand(
			ctx,
			absoluteRoot,
			nil,
			nil,
			"git",
			"worktree",
			"add",
			"--detach",
			materialization.Worktree,
			syntheticCommit,
		); err != nil {
			return nil, fmt.Errorf("materialize detached worktree: %w", err)
		}
		materialization.worktreeOK = true
		status, err := gitOutput(ctx, materialization.Worktree, nil, nil, "status", "--porcelain=v1")
		if err != nil {
			return nil, err
		}
		if status != "" {
			return nil, fmt.Errorf("synthetic worktree is not clean: %s", status)
		}
	}
	failed = false
	return materialization, nil
}

// Close removes the detached worktree and all isolated temporary state.
func (m *Materialization) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	cleanupCtx, cancel := detachedCleanupContext(ctx)
	defer cancel()
	var closeErrors []error
	if m.worktreeOK {
		if _, err := runCommand(
			cleanupCtx,
			m.Root,
			nil,
			nil,
			"git",
			"worktree",
			"remove",
			"--force",
			m.Worktree,
		); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("remove detached worktree: %w", err))
		}
		m.worktreeOK = false
	}
	if m.tempRoot != "" {
		if err := os.RemoveAll(m.tempRoot); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("remove temporary directory: %w", err))
		}
		m.tempRoot = ""
	}
	return errors.Join(closeErrors...)
}

// LoadPathSelection loads a sorted, explicit, repository-relative path list.
func LoadPathSelection(path string) ([]string, error) {
	if err := requireRegularFile(path); err != nil {
		return nil, fmt.Errorf("inspect path selection: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read path selection: %w", err)
	}
	var paths []string
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(string(data), "\n") {
		pathValue := strings.TrimSpace(line)
		if pathValue == "" {
			continue
		}
		if strings.HasPrefix(pathValue, "#") {
			return nil, fmt.Errorf("comments are forbidden in path selection: %q", pathValue)
		}
		if err := validateRepoPath(pathValue); err != nil {
			return nil, fmt.Errorf("invalid selected path %q: %w", pathValue, err)
		}
		if seen[pathValue] {
			return nil, fmt.Errorf("duplicate selected path %q", pathValue)
		}
		seen[pathValue] = true
		paths = append(paths, pathValue)
	}
	if len(paths) == 0 {
		return nil, errors.New("path selection is empty")
	}
	if !slices.IsSorted(paths) {
		return nil, errors.New("path selection must be sorted")
	}
	return paths, nil
}

func repositoryRoot(ctx context.Context, root string) (string, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	resolved, err := gitOutput(ctx, absoluteRoot, nil, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve Git repository root: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func detachedCleanupContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), detachedCleanupTimeout)
}

func resolveFromRoot(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.FromSlash(path))
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func gitOutput(ctx context.Context, directory string, env []string, input []byte, args ...string) (string, error) {
	output, err := runCommand(ctx, directory, env, input, "git", args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func runCommand(
	ctx context.Context,
	directory string,
	env []string,
	input []byte,
	name string,
	args ...string,
) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.WaitDelay = 10 * time.Second
	command.Dir = directory
	if env != nil {
		command.Env = env
	}
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("run %s: %w", name, err)
	}
	return output, nil
}

func replaceEnvironmentVariable(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, prefix+value)
}
