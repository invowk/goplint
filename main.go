// SPDX-License-Identifier: MPL-2.0

// goplint reports bare primitive types (string, int, float64, etc.)
// in struct fields, function parameters, and return types where DDD Value
// Types should be used instead.
//
// Usage:
//
//	goplint [-config=exceptions.toml] [-json] ./...
//	goplint -audit-exceptions -config=exceptions.toml ./...
//	goplint -update-baseline=baseline.toml -check-all -config=exceptions.toml ./...
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/invowk/invowk/tools/goplint/goplint"
)

func main() {
	// Detect --update-baseline before singlechecker takes over flag parsing.
	// singlechecker.Main() calls os.Exit(), so we must intercept first.
	if outputPath := extractUpdateBaselinePath(os.Args[1:]); outputPath != "" {
		if err := generateBaseline(outputPath, os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "goplint: update-baseline: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Detect --global (only meaningful with --audit-exceptions).
	// Runs self as subprocess to aggregate stale exceptions across all packages.
	if hasFlag(os.Args[1:], "global") {
		if err := auditExceptionsGlobal(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "goplint: audit-exceptions-global: %v\n", err)
			os.Exit(1)
		}
		return
	}

	singlechecker.Main(goplint.Analyzer)
}

// extractUpdateBaselinePath scans CLI args for -update-baseline=PATH or
// --update-baseline=PATH and returns the path. Returns "" if not found.
func extractUpdateBaselinePath(args []string) string {
	for _, arg := range args {
		trimmed := strings.TrimLeft(arg, "-")
		if after, ok := strings.CutPrefix(trimmed, "update-baseline="); ok {
			return after
		}
	}
	return ""
}

// generateBaseline runs the analyzer as a subprocess with -json output,
// parses the diagnostics, and writes a sorted baseline TOML file.
//
// The subprocess approach is necessary because singlechecker.Main() calls
// os.Exit() after analysis — there is no post-analysis hook for cross-package
// aggregation within the framework.
func generateBaseline(outputPath string, originalArgs []string) error {
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	// Build subprocess args: remove -update-baseline, ensure -json is present.
	subArgs := buildSubprocessArgs(originalArgs)

	cmd := exec.Command(selfPath, subArgs...)
	cmd.Stderr = os.Stderr // let warnings/errors pass through

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// singlechecker exits non-zero when diagnostics are found.
	// We need the JSON output regardless, so ignore exit errors.
	_ = cmd.Run()

	// Parse the go/analysis JSON output.
	findings, err := parseAnalysisJSON(stdout.Bytes())
	if err != nil {
		return fmt.Errorf("parsing analysis output: %w", err)
	}

	if err := goplint.WriteBaseline(outputPath, findings); err != nil {
		return fmt.Errorf("writing baseline: %w", err)
	}

	total := 0
	for _, entries := range findings {
		total += len(entries)
	}
	fmt.Fprintf(os.Stderr, "Baseline written: %s (%d findings)\n", outputPath, total)

	return nil
}

// buildSubprocessArgs constructs args for the subprocess invocation by
// removing -update-baseline and ensuring -json is present.
func buildSubprocessArgs(args []string) []string {
	return filterAndEnsureFlags(args, func(trimmed string) bool {
		return strings.HasPrefix(trimmed, "update-baseline")
	}, []string{"-json"})
}

// hasFlag checks if any CLI arg matches the given flag name (with or without leading dashes).
func hasFlag(args []string, flagName string) bool {
	for _, arg := range args {
		trimmed := strings.TrimLeft(arg, "-")
		if trimmed == flagName {
			return true
		}
	}
	return false
}

// auditExceptionsGlobal runs --audit-exceptions as a subprocess with -json output,
// aggregates stale exception patterns across all packages, and reports patterns
// that were stale in every package (globally stale — truly unreachable patterns).
func auditExceptionsGlobal(originalArgs []string) error {
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	// Build subprocess args: remove --global, ensure -json and -audit-exceptions.
	subArgs := buildGlobalAuditArgs(originalArgs)

	cmd := exec.Command(selfPath, subArgs...)
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// singlechecker exits non-zero when diagnostics are found.
	_ = cmd.Run()

	// Parse the JSON stream — track stale patterns per package.
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))

	// pattern → count of packages where it was reported as stale
	stalePerPattern := make(map[string]int)
	totalPackages := 0

	for decoder.More() {
		var result analysisResult
		if err := decoder.Decode(&result); err != nil {
			return fmt.Errorf("decoding JSON: %w", err)
		}

		for _, analyzers := range result {
			totalPackages++
			diags, ok := analyzers["goplint"]
			if !ok {
				continue
			}
			for _, d := range diags {
				if d.Category != goplint.CategoryStaleException {
					continue
				}
				pattern := extractPatternFromStaleMessage(d.Message)
				if pattern != "" {
					stalePerPattern[pattern]++
				}
			}
		}
	}

	if totalPackages == 0 {
		fmt.Fprintf(os.Stderr, "Global audit: no packages analyzed\n")
		return nil
	}

	// Report patterns stale in ALL packages (truly globally stale).
	var stalePatterns []string
	for pattern, count := range stalePerPattern {
		if count >= totalPackages {
			stalePatterns = append(stalePatterns, pattern)
		}
	}
	slices.Sort(stalePatterns)

	for _, pattern := range stalePatterns {
		fmt.Printf("globally stale exception: pattern %q matched no diagnostics in any package\n", pattern)
	}

	fmt.Fprintf(os.Stderr, "Global audit: %d/%d stale exception patterns are globally stale (%d packages analyzed)\n",
		len(stalePatterns), len(stalePerPattern), totalPackages)

	return nil
}

// buildGlobalAuditArgs constructs args for the subprocess invocation by
// removing --global and ensuring -json and -audit-exceptions are present.
func buildGlobalAuditArgs(args []string) []string {
	return filterAndEnsureFlags(args, func(trimmed string) bool {
		return trimmed == "global"
	}, []string{"-json", "-audit-exceptions"})
}

// filterAndEnsureFlags is a shared helper for building subprocess arg lists.
// It removes args where skipFn(trimmedArg) returns true and ensures each flag
// in requiredFlags is present (prepended if missing). The trimmed form strips
// leading dashes for comparison.
func filterAndEnsureFlags(args []string, skipFn func(trimmed string) bool, requiredFlags []string) []string {
	present := make(map[string]bool, len(requiredFlags))
	var result []string

	for _, arg := range args {
		trimmed := strings.TrimLeft(arg, "-")
		if skipFn(trimmed) {
			continue
		}
		for _, rf := range requiredFlags {
			if trimmed == strings.TrimLeft(rf, "-") {
				present[rf] = true
			}
		}
		result = append(result, arg)
	}

	for _, rf := range requiredFlags {
		if !present[rf] {
			result = slices.Insert(result, 0, rf)
		}
	}

	return result
}

// extractPatternFromStaleMessage extracts the exception pattern from a
// stale-exception diagnostic message. The message format is:
// 'stale exception: pattern "X" matched no diagnostics (reason: Y)'
func extractPatternFromStaleMessage(message string) string {
	const prefix = `stale exception: pattern "`
	_, after, found := strings.Cut(message, prefix)
	if !found {
		return ""
	}
	pattern, _, found := strings.Cut(after, `"`)
	if !found {
		return ""
	}
	return pattern
}

// analysisResult represents the go/analysis -json output structure.
// The JSON is a map from package path to per-analyzer results.
type analysisResult map[string]map[string][]analysisDiagnostic

// analysisDiagnostic is a single diagnostic in the -json output.
type analysisDiagnostic struct {
	Posn     string `json:"posn"`
	Message  string `json:"message"`
	Category string `json:"category"`
	URL      string `json:"url"`
}

// parseAnalysisJSON parses the go/analysis -json output (one JSON object
// per package, concatenated) and returns findings grouped by category.
// Filters out stale-exception diagnostics and deduplicates.
func parseAnalysisJSON(data []byte) (map[string][]goplint.BaselineFinding, error) {
	// The -json output is a stream of JSON objects (one per package).
	// Each object maps package path → analyzer name → diagnostics array.
	decoder := json.NewDecoder(bytes.NewReader(data))

	// Deduplicate across packages (test variants can produce duplicates).
	// Keyed by category and stable finding ID.
	seen := make(map[string]map[string]goplint.BaselineFinding) // category → findingID → finding

	for decoder.More() {
		var result analysisResult
		if err := decoder.Decode(&result); err != nil {
			return nil, fmt.Errorf("decoding JSON object: %w", err)
		}

		for _, analyzers := range result {
			diags, ok := analyzers["goplint"]
			if !ok {
				continue
			}
			for _, d := range diags {
				// Skip stale-exception — config maintenance, not codebase findings.
				if d.Category == goplint.CategoryStaleException {
					continue
				}
				if d.Category == "" || d.Message == "" {
					continue
				}
				findingID := goplint.FindingIDFromDiagnosticURL(d.URL)
				if findingID == "" {
					// Legacy compatibility for diagnostics emitted without URL.
					findingID = goplint.FallbackFindingID(d.Category, d.Message)
				}

				if seen[d.Category] == nil {
					seen[d.Category] = make(map[string]goplint.BaselineFinding)
				}
				seen[d.Category][findingID] = goplint.BaselineFinding{
					ID:      findingID,
					Message: d.Message,
				}
			}
		}
	}

	// Convert sets to slices. WriteBaseline handles sorting.
	findings := make(map[string][]goplint.BaselineFinding, len(seen))
	for cat, entries := range seen {
		slice := make([]goplint.BaselineFinding, 0, len(entries))
		for _, entry := range entries {
			slice = append(slice, entry)
		}
		findings[cat] = slice
	}

	return findings, nil
}
