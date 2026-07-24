// SPDX-License-Identifier: MPL-2.0

// Command repository-audit produces or validates the single-pass canonical
// goplint repository audit and performs configuration-only review-date checks.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/goplint"
	"github.com/invowk/invowk/tools/goplint/internal/repositoryaudit"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func main() {
	mode := flag.String("mode", "produce", "operation: produce, full-scan, baseline, exceptions, or review-dates")
	root := flag.String("root", "../..", "repository root")
	analyzer := flag.String("analyzer", "bin/goplint", "root-relative analyzer binary")
	baseline := flag.String("baseline", "tools/goplint/baseline.toml", "root-relative baseline")
	exceptions := flag.String("exceptions", "tools/goplint/exceptions.toml", "root-relative exceptions config")
	manifest := flag.String("semantic-manifest", "tools/goplint/spec/soundness-gate.v1.json", "root-relative governing semantic manifest")
	packages := flag.String("packages", "./cmd/...,./internal/...,./pkg/...", "comma-separated canonical package patterns")
	cachePolicy := flag.String("cache-policy", "inherited", "reviewed analyzer cache policy label")
	output := flag.String("output", os.Getenv("GOPLINT_REPOSITORY_AUDIT_PATH"), "absolute immutable repository-audit path")
	flag.Parse()
	if err := execute(context.Background(), commandOptions{
		mode: *mode, root: *root, analyzer: *analyzer, baseline: *baseline,
		exceptions: *exceptions, manifest: *manifest, packagePatterns: splitPatterns(*packages),
		cachePolicy: *cachePolicy, output: *output,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "goplint repository audit:", err)
		os.Exit(1)
	}
}

type commandOptions struct {
	mode, root, analyzer, baseline, exceptions, manifest, cachePolicy, output string
	packagePatterns                                                           []string
}

func execute(ctx context.Context, options commandOptions) error {
	if options.mode == "review-dates" {
		findings, err := goplint.AuditExceptionReviewDates(resolveRootPath(options.root, options.exceptions), time.Now())
		if err != nil {
			return fmt.Errorf("audit exception review dates: %w", err)
		}
		for _, finding := range findings {
			fmt.Fprintln(os.Stderr, finding.Message)
		}
		if len(findings) != 0 {
			return fmt.Errorf("%d malformed or overdue exception review entries", len(findings))
		}
		return nil
	}
	workspaceDigest, err := soundnessgate.WorkspaceDigest(ctx, options.root)
	if err != nil {
		return fmt.Errorf("calculate repository workspace digest: %w", err)
	}
	runOptions := repositoryaudit.RunOptions{
		Root: options.root, AnalyzerPath: options.analyzer, BaselinePath: options.baseline,
		ExceptionsPath: options.exceptions, SemanticManifestPath: options.manifest,
		PackagePatterns: options.packagePatterns, WorkspaceDigest: workspaceDigest,
		CachePolicy: options.cachePolicy, OutputPath: options.output,
	}
	if options.mode == "produce" {
		outputPath, cleanup, err := outputPath(options.output)
		if err != nil {
			return err
		}
		defer cleanup()
		runOptions.OutputPath = outputPath
		result, err := repositoryaudit.Run(ctx, runOptions)
		if err != nil {
			return fmt.Errorf("produce repository audit: %w", err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			ResultID     string `json:"result_id"`
			FindingCount int    `json:"finding_count"`
			PackageCount int    `json:"package_count"`
		}{ResultID: result.ResultID, FindingCount: len(result.Findings), PackageCount: len(result.Packages.PackageIDs)}); err != nil {
			return fmt.Errorf("encode repository audit summary: %w", err)
		}
		return nil
	}
	if options.mode != "full-scan" && options.mode != "baseline" && options.mode != "exceptions" {
		return fmt.Errorf("mode %q is invalid", options.mode)
	}
	outputPath, cleanup, err := outputPath(options.output)
	if err != nil {
		return err
	}
	defer cleanup()
	runOptions.OutputPath = outputPath
	if _, statErr := os.Stat(outputPath); os.IsNotExist(statErr) {
		if _, err := repositoryaudit.Run(ctx, runOptions); err != nil {
			return fmt.Errorf("produce repository audit for %s consumer: %w", options.mode, err)
		}
	} else if statErr != nil {
		return fmt.Errorf("inspect repository audit output: %w", statErr)
	}
	result, err := repositoryaudit.Load(ctx, outputPath)
	if err != nil {
		return fmt.Errorf("load repository audit: %w", err)
	}
	expected, err := repositoryaudit.CurrentInputBinding(runOptions)
	if err != nil {
		return fmt.Errorf("calculate current repository audit input binding: %w", err)
	}
	if err := repositoryaudit.ValidateConsumer(result, expected, result.Packages.PackageIDs, options.mode); err != nil {
		return fmt.Errorf("validate repository audit for %s consumer: %w", options.mode, err)
	}
	return nil
}

func splitPatterns(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func resolveRootPath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, filepath.FromSlash(path))
}

func outputPath(explicit string) (string, func(), error) {
	if explicit != "" {
		return explicit, func() {}, nil
	}
	file, err := os.CreateTemp("", "goplint-repository-audit-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary repository audit path: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", nil, fmt.Errorf("close temporary repository audit file: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", nil, fmt.Errorf("remove temporary repository audit placeholder: %w", err)
	}
	return path, func() {
		os.Remove(path) //nolint:errcheck // Best-effort cleanup of a private temporary audit artifact.
	}, nil
}
