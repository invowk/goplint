// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	diffDispositionSelected = "selected"
	diffDispositionExcluded = "reviewed-exclusion"
)

// collectDiffCensus compares the caller's complete tracked and non-ignored
// untracked repository state with the retained base. Every changed path must be
// selected, explicitly reviewed as unrelated, or be an authorized recorder
// output.
func collectDiffCensus(
	ctx context.Context,
	root string,
	baseRevision string,
	selected []string,
	review DiffReviewPlan,
	authorizedOutputs []string,
) (DiffCensusIdentity, error) {
	baseCommit, err := gitOutput(ctx, root, nil, nil, "rev-parse", baseRevision+"^{commit}")
	if err != nil {
		return DiffCensusIdentity{}, fmt.Errorf("resolve complete-diff base %q: %w", baseRevision, err)
	}
	if err := validateReviewedExclusions(review.ReviewedExclusions); err != nil {
		return DiffCensusIdentity{}, err
	}
	authorized, err := canonicalAuthorizedOutputs(authorizedOutputs)
	if err != nil {
		return DiffCensusIdentity{}, err
	}
	authorizedSet := make(map[string]bool, len(authorized))
	for _, path := range authorized {
		authorizedSet[path] = true
	}
	exclusionByPath := make(map[string]ReviewedExclusion, len(review.ReviewedExclusions))
	for _, exclusion := range review.ReviewedExclusions {
		if authorizedSet[exclusion.Path] {
			return DiffCensusIdentity{}, fmt.Errorf("reviewed exclusion %q overlaps an authorized recorder output", exclusion.Path)
		}
		covered, coverErr := pathCoveredBySelectionAtBase(ctx, root, baseCommit, selected, exclusion.Path)
		if coverErr != nil {
			return DiffCensusIdentity{}, coverErr
		}
		if covered {
			return DiffCensusIdentity{}, fmt.Errorf("reviewed exclusion %q is also covered by the proof path selection", exclusion.Path)
		}
		exclusionByPath[exclusion.Path] = exclusion
	}

	changedStatuses, err := changedPathStatuses(ctx, root, baseCommit)
	if err != nil {
		return DiffCensusIdentity{}, err
	}
	changes := make([]ChangedPathIdentity, 0, len(changedStatuses))
	seenExclusions := make(map[string]bool, len(exclusionByPath))
	for _, changed := range changedStatuses {
		if authorizedSet[changed.Path] {
			continue
		}
		var disposition string
		covered, coverErr := pathCoveredBySelectionAtBase(ctx, root, baseCommit, selected, changed.Path)
		if coverErr != nil {
			return DiffCensusIdentity{}, coverErr
		}
		if covered {
			disposition = diffDispositionSelected
		} else if _, ok := exclusionByPath[changed.Path]; ok {
			disposition = diffDispositionExcluded
			seenExclusions[changed.Path] = true
		} else {
			return DiffCensusIdentity{}, fmt.Errorf("changed path %q is silently omitted from the proof selection and reviewed exclusions", changed.Path)
		}
		identity, identityErr := changedPathIdentity(root, changed, disposition)
		if identityErr != nil {
			return DiffCensusIdentity{}, identityErr
		}
		changes = append(changes, identity)
	}
	for _, exclusion := range review.ReviewedExclusions {
		if !seenExclusions[exclusion.Path] {
			return DiffCensusIdentity{}, fmt.Errorf("reviewed exclusion %q is stale because the path is not changed", exclusion.Path)
		}
	}
	identity := DiffCensusIdentity{
		BaseCommit:         baseCommit,
		Changes:            changes,
		ReviewedExclusions: slices.Clone(review.ReviewedExclusions),
		AuthorizedOutputs:  authorized,
	}
	payload, err := json.Marshal(struct {
		BaseCommit         string                `json:"base_commit"`
		Changes            []ChangedPathIdentity `json:"changes"`
		ReviewedExclusions []ReviewedExclusion   `json:"reviewed_exclusions"`
		AuthorizedOutputs  []string              `json:"authorized_outputs"`
	}{
		BaseCommit:         identity.BaseCommit,
		Changes:            identity.Changes,
		ReviewedExclusions: identity.ReviewedExclusions,
		AuthorizedOutputs:  identity.AuthorizedOutputs,
	})
	if err != nil {
		return DiffCensusIdentity{}, fmt.Errorf("encode complete-diff census: %w", err)
	}
	identity.CanonicalSHA256 = digestBytes(payload)
	return identity, nil
}

type changedPathStatus struct {
	Path   string
	Status string
}

func changedPathStatuses(ctx context.Context, root, baseCommit string) ([]changedPathStatus, error) {
	trackedOutput, err := runCommand(
		ctx,
		root,
		nil,
		nil,
		"git",
		"diff",
		"--name-status",
		"-z",
		"--no-renames",
		baseCommit,
		"--",
	)
	if err != nil {
		return nil, fmt.Errorf("enumerate tracked complete diff: %w", err)
	}
	statuses, err := parseNameStatusZ(trackedOutput)
	if err != nil {
		return nil, err
	}
	untrackedOutput, err := runCommand(
		ctx,
		root,
		nil,
		nil,
		"git",
		"ls-files",
		"--others",
		"--exclude-standard",
		"-z",
		"--",
	)
	if err != nil {
		return nil, fmt.Errorf("enumerate untracked complete diff: %w", err)
	}
	seen := make(map[string]bool, len(statuses))
	for _, status := range statuses {
		seen[status.Path] = true
	}
	for _, field := range splitNULTerminated(untrackedOutput) {
		path := filepath.ToSlash(field)
		if err := validateRepoPath(path); err != nil {
			return nil, fmt.Errorf("invalid untracked complete-diff path %q: %w", path, err)
		}
		if seen[path] {
			return nil, fmt.Errorf("duplicate tracked and untracked complete-diff path %q", path)
		}
		seen[path] = true
		// Normalize an untracked path to the same base-relative addition status
		// it has after the proven tree is committed. The separate enumeration
		// above still ensures untracked content participates in the census.
		statuses = append(statuses, changedPathStatus{Path: path, Status: "A"})
	}
	slices.SortFunc(statuses, func(left, right changedPathStatus) int {
		return strings.Compare(left.Path, right.Path)
	})
	return statuses, nil
}

func parseNameStatusZ(data []byte) ([]changedPathStatus, error) {
	fields := splitNULTerminated(data)
	statuses := make([]changedPathStatus, 0, len(fields)/2)
	for index := 0; index < len(fields); {
		status := fields[index]
		index++
		var path string
		if prefix, remainder, ok := strings.Cut(status, "\t"); ok {
			status = prefix
			path = remainder
		} else {
			if index >= len(fields) {
				return nil, errors.New("tracked complete-diff status is missing its path")
			}
			path = fields[index]
			index++
		}
		if len(status) != 1 || !strings.ContainsRune("ACDMTUXB", rune(status[0])) {
			return nil, fmt.Errorf("unsupported tracked complete-diff status %q", status)
		}
		path = filepath.ToSlash(path)
		if err := validateRepoPath(path); err != nil {
			return nil, fmt.Errorf("invalid tracked complete-diff path %q: %w", path, err)
		}
		statuses = append(statuses, changedPathStatus{Path: path, Status: status})
	}
	return statuses, nil
}

func splitNULTerminated(data []byte) []string {
	raw := bytes.Split(data, []byte{0})
	if len(raw) > 0 && len(raw[len(raw)-1]) == 0 {
		raw = raw[:len(raw)-1]
	}
	fields := make([]string, 0, len(raw))
	for _, field := range raw {
		fields = append(fields, string(field))
	}
	return fields
}

func changedPathIdentity(root string, changed changedPathStatus, disposition string) (ChangedPathIdentity, error) {
	identity := ChangedPathIdentity{
		Path:        changed.Path,
		GitStatus:   changed.Status,
		Disposition: disposition,
	}
	absolutePath := resolveFromRoot(root, changed.Path)
	info, err := os.Lstat(absolutePath)
	if err != nil {
		if os.IsNotExist(err) && changed.Status == "D" {
			identity.Kind = "deleted"
			return identity, nil
		}
		return ChangedPathIdentity{}, fmt.Errorf("inspect changed path %q: %w", changed.Path, err)
	}
	var content []byte
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		identity.Kind = "symlink"
		target, readErr := os.Readlink(absolutePath)
		if readErr != nil {
			return ChangedPathIdentity{}, fmt.Errorf("read changed symlink %q: %w", changed.Path, readErr)
		}
		content = []byte(target)
	case info.Mode().IsRegular():
		identity.Kind = "regular"
		if info.Mode().Perm()&0o111 != 0 {
			identity.Kind = "executable"
		}
		content, err = os.ReadFile(absolutePath)
		if err != nil {
			return ChangedPathIdentity{}, fmt.Errorf("read changed path %q: %w", changed.Path, err)
		}
	default:
		return ChangedPathIdentity{}, fmt.Errorf("changed path %q has unsupported mode %s", changed.Path, info.Mode())
	}
	identity.ContentSHA256 = digestBytes(content)
	return identity, nil
}

func canonicalAuthorizedOutputs(paths []string) ([]string, error) {
	result := slices.Clone(paths)
	slices.Sort(result)
	for index, path := range result {
		if err := validateRepoPath(path); err != nil {
			return nil, fmt.Errorf("authorized recorder output: %w", err)
		}
		if index > 0 && path == result[index-1] {
			return nil, fmt.Errorf("duplicate authorized recorder output %q", path)
		}
	}
	return result, nil
}

func pathCoveredBySelectionAtBase(
	ctx context.Context,
	root string,
	baseCommit string,
	selected []string,
	path string,
) (bool, error) {
	for _, selection := range selected {
		if path == selection {
			return true, nil
		}
		if !strings.HasPrefix(path, selection+"/") {
			continue
		}
		info, err := os.Stat(resolveFromRoot(root, selection))
		if err == nil {
			if info.IsDir() {
				return true, nil
			}
			continue
		}
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("inspect selected path %q: %w", selection, err)
		}
		objectType, typeErr := gitOutput(ctx, root, nil, nil, "git", "cat-file", "-t", baseCommit+":"+selection)
		if typeErr == nil && objectType == "tree" {
			return true, nil
		}
	}
	return false, nil
}
