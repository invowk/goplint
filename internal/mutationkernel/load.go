// SPDX-License-Identifier: MPL-2.0

package mutationkernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Load reads the bound manifests beneath root and evaluates the current
// mutation-kernel coverage contract.
func Load(ctx context.Context, root, manifestPath string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("load mutation kernel: %w", err)
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve mutation kernel root: %w", err)
	}
	if err := validateRepositoryPath("manifest", manifestPath); err != nil {
		return Result{}, err
	}
	var definition manifest
	if err := decodeStrictJSON(resolvePath(absoluteRoot, manifestPath), &definition); err != nil {
		return Result{}, fmt.Errorf("load mutation kernel manifest: %w", err)
	}
	if err := definition.validate(); err != nil {
		return Result{}, err
	}
	var rules semanticRulesManifest
	if err := decodeStrictJSON(resolvePath(absoluteRoot, definition.SemanticRules), &rules); err != nil {
		return Result{}, fmt.Errorf("load semantic rules: %w", err)
	}
	var profile mutationProfile
	if err := decodeStrictJSON(resolvePath(absoluteRoot, definition.BlockingProfile), &profile); err != nil {
		return Result{}, fmt.Errorf("load blocking mutation profile: %w", err)
	}
	var catalog mutationCatalog
	if err := decodeStrictJSON(resolvePath(absoluteRoot, definition.MutantCatalog), &catalog); err != nil {
		return Result{}, fmt.Errorf("load mutant catalog: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("load mutation kernel: %w", err)
	}
	return evaluate(definition, rules, profile, catalog)
}

func decodeStrictJSON(filePath string, target any) (returnErr error) {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %q: %w", filePath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close %q: %w", filePath, closeErr))
		}
	}()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %q: %w", filePath, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode %q: trailing JSON value", filePath)
		}
		return fmt.Errorf("decode %q trailing data: %w", filePath, err)
	}
	return nil
}

func validateRepositoryPath(label, value string) error {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || strings.Contains(value, "\\") ||
		path.IsAbs(value) || value == "." || value == ".." || strings.HasPrefix(value, "../") || path.Clean(value) != value {
		return fmt.Errorf("mutation kernel %s path %q is not canonical repository-relative", label, value)
	}
	return nil
}

func resolvePath(root, repositoryPath string) string {
	return filepath.Join(root, filepath.FromSlash(repositoryPath))
}
