// SPDX-License-Identifier: MPL-2.0

package mutationkernel

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
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

// decodeStrictJSON decodes an integrity-sensitive manifest with json/v2
// semantics: duplicate object members and invalid UTF-8 are rejected by
// default, unknown members are rejected, and trailing content after the
// top-level value is rejected. Field-name matching is case-sensitive;
// manifests read here are produced by writers in this repository with
// snake_case tags, so casing is controlled.
func decodeStrictJSON(filePath string, target any) (returnErr error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("open %q: %w", filePath, err)
	}
	if err := jsonv2.Unmarshal(data, target, jsonv2.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("decode %q: %w", filePath, err)
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
