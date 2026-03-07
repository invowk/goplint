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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/invowk/invowk/tools/goplint/goplint"
)

type commandRunner func(cmd *exec.Cmd) error

type baselineGenerator func(outputPath string, originalArgs []string) error

type globalAuditRunner func(originalArgs []string) error

type dispatchDeps struct {
	generateBaseline      baselineGenerator
	auditExceptionsGlobal globalAuditRunner
}

func defaultRunCommand(cmd *exec.Cmd) error {
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running subprocess: %w", err)
	}
	return nil
}

func main() {
	nextArgs, exitCode, handled := dispatch(os.Args[1:], os.Stderr)
	if handled {
		os.Exit(exitCode)
		return
	}

	os.Args = append(os.Args[:1], nextArgs...)
	singlechecker.Main(goplint.NewAnalyzer())
}

func dispatch(args []string, stderr io.Writer) (nextArgs []string, exitCode int, handled bool) {
	return dispatchWithDeps(args, stderr, dispatchDeps{
		generateBaseline:      generateBaseline,
		auditExceptionsGlobal: auditExceptionsGlobal,
	})
}

func dispatchWithDeps(args []string, stderr io.Writer, deps dispatchDeps) (nextArgs []string, exitCode int, handled bool) {
	if deps.generateBaseline == nil {
		deps.generateBaseline = generateBaseline
	}
	if deps.auditExceptionsGlobal == nil {
		deps.auditExceptionsGlobal = auditExceptionsGlobal
	}

	// Detect --update-baseline before singlechecker takes over flag parsing.
	// singlechecker.Main() calls os.Exit(), so we must intercept first.
	if hasFlagToken(args, "update-baseline") {
		outputPath := extractUpdateBaselinePath(args)
		if outputPath == "" {
			if err := writeStderrf(stderr, "goplint: update-baseline: missing required path value\n"); err != nil {
				return nil, 1, true
			}
			return nil, 2, true
		}
		if err := deps.generateBaseline(outputPath, args); err != nil {
			if writeErr := writeStderrf(stderr, "goplint: update-baseline: %v\n", err); writeErr != nil {
				return nil, 1, true
			}
			return nil, 1, true
		}
		return nil, 0, true
	}

	// Detect --global (only meaningful with --audit-exceptions).
	// Runs self as subprocess to aggregate stale exceptions across all packages.
	if hasFlagToken(args, "global") {
		if hasFlag(args, "global") {
			if err := deps.auditExceptionsGlobal(args); err != nil {
				if writeErr := writeStderrf(stderr, "goplint: audit-exceptions-global: %v\n", err); writeErr != nil {
					return nil, 1, true
				}
				return nil, 1, true
			}
			return nil, 0, true
		}
		// Explicitly disabled (--global=false): strip the meta-flag before
		// delegating to singlechecker, which does not recognize it.
		return removeFlagWithOptionalValue(args, "global", true), 0, false
	}

	return args, 0, false
}

func writeStderrf(stderr io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(stderr, format, args...)
	if err != nil {
		return fmt.Errorf("writing stderr output: %w", err)
	}
	return nil
}

// extractUpdateBaselinePath scans CLI args for:
//   - -update-baseline=PATH / --update-baseline=PATH
//   - -update-baseline PATH / --update-baseline PATH
//
// Returns "" if not found or if the flag is present without a value.
func extractUpdateBaselinePath(args []string) string {
	for i := range len(args) {
		arg := args[i]
		matched, value, hasInlineValue := parseFlagToken(arg, "update-baseline")
		if !matched {
			continue
		}
		if hasInlineValue {
			return value
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			return args[i+1]
		}
		return ""
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
	return generateBaselineWithRunner(outputPath, originalArgs, defaultRunCommand, os.Stderr)
}

func generateBaselineWithRunner(
	outputPath string,
	originalArgs []string,
	runCommand commandRunner,
	stderr io.Writer,
) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	findingsFile, err := os.CreateTemp("", "goplint-findings-*.jsonl")
	if err != nil {
		return fmt.Errorf("creating findings stream temp file: %w", err)
	}
	findingsPath := findingsFile.Name()
	if err := findingsFile.Close(); err != nil {
		return fmt.Errorf("closing findings stream temp file: %w", err)
	}
	defer func() {
		if removeErr := os.Remove(findingsPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			if writeErr := writeStderrf(stderr, "goplint: warning: removing findings stream temp file: %v\n", removeErr); writeErr != nil {
				return
			}
		}
	}()

	// Build subprocess args: remove -update-baseline, ensure -json is present.
	subArgs := buildSubprocessArgs(originalArgs)
	subArgs = slices.Insert(subArgs, 0, "-emit-findings-jsonl="+findingsPath)

	stdout, err := runAnalyzerSubprocess(runCommand, stderr, subArgs)
	if err != nil {
		return fmt.Errorf("running analyzer subprocess: %w", err)
	}

	// Parse the machine findings stream emitted by the analyzer.
	findingsData, err := os.ReadFile(findingsPath)
	if err != nil {
		return fmt.Errorf("reading findings stream: %w", err)
	}
	findings, err := parseFindingsJSONL(findingsData)
	if err != nil {
		return fmt.Errorf("parsing analysis output: %w", err)
	}
	analysisFindings, err := parseAnalysisJSON(stdout)
	if err != nil {
		return fmt.Errorf("parsing analyzer json output: %w", err)
	}
	streamCount := countBaselineFindings(findings)
	analysisCount := countBaselineFindings(analysisFindings)
	if analysisCount > 0 && streamCount == 0 {
		return fmt.Errorf("findings stream is empty but analyzer output contains %d suppressible findings", analysisCount)
	}
	if streamCount < analysisCount {
		return fmt.Errorf("findings stream is incomplete (%d findings) versus analyzer output (%d findings)", streamCount, analysisCount)
	}

	if err := goplint.WriteBaseline(outputPath, findings); err != nil {
		return fmt.Errorf("writing baseline: %w", err)
	}

	total := 0
	for _, entries := range findings {
		total += len(entries)
	}
	if _, err := fmt.Fprintf(stderr, "Baseline written: %s (%d findings)\n", outputPath, total); err != nil {
		return fmt.Errorf("writing baseline summary: %w", err)
	}

	return nil
}

// buildSubprocessArgs constructs args for the subprocess invocation by
// removing -update-baseline and ensuring -json is present.
func buildSubprocessArgs(args []string) []string {
	filtered := removeFlagWithOptionalValue(args, "update-baseline", false)
	return filterAndEnsureFlags(filtered, func(string) bool { return false }, []string{"-json"})
}

// hasFlag checks if any CLI arg matches the given flag name (with or without leading dashes).
func hasFlag(args []string, flagName string) bool {
	for i := range len(args) {
		matched, value, hasInlineValue := parseFlagToken(args[i], flagName)
		if !matched {
			continue
		}
		if hasInlineValue {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return true
			}
			return parsed
		}
		if i+1 < len(args) {
			if parsed, err := strconv.ParseBool(args[i+1]); err == nil {
				return parsed
			}
		}
		return true
	}
	return false
}

// hasFlagToken reports whether flagName appears in args in any supported form:
// -flag, --flag, -flag=value, --flag=value.
func hasFlagToken(args []string, flagName string) bool {
	for _, arg := range args {
		matched, _, _ := parseFlagToken(arg, flagName)
		if matched {
			return true
		}
	}
	return false
}

// auditExceptionsGlobal runs --audit-exceptions as a subprocess with -json output,
// aggregates stale exception patterns across all packages, and reports patterns
// that were stale in every package (globally stale — truly unreachable patterns).
func auditExceptionsGlobal(originalArgs []string) error {
	return auditExceptionsGlobalWithRunner(originalArgs, defaultRunCommand, os.Stderr)
}

func auditExceptionsGlobalWithRunner(originalArgs []string, runCommand commandRunner, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}

	// Build subprocess args: remove --global, ensure -json and -audit-exceptions.
	subArgs := buildGlobalAuditArgs(originalArgs)

	stdout, err := runAnalyzerSubprocess(runCommand, stderr, subArgs)
	if err != nil {
		return fmt.Errorf("running global audit subprocess: %w", err)
	}

	stalePatterns, totalPatterns, totalPackages, err := aggregateGlobalStalePatterns(stdout)
	if err != nil {
		return fmt.Errorf("aggregating global stale exceptions: %w", err)
	}
	if totalPackages == 0 {
		if _, err := fmt.Fprintf(stderr, "Global audit: no packages analyzed\n"); err != nil {
			return fmt.Errorf("writing global audit summary: %w", err)
		}
		return nil
	}

	for _, pattern := range stalePatterns {
		fmt.Printf("globally stale exception: pattern %q matched no diagnostics in any package\n", pattern)
	}

	if _, err := fmt.Fprintf(stderr, "Global audit: %d/%d stale exception patterns are globally stale (%d packages analyzed)\n",
		len(stalePatterns), totalPatterns, totalPackages); err != nil {
		return fmt.Errorf("writing global audit summary: %w", err)
	}

	if len(stalePatterns) > 0 {
		return fmt.Errorf("%d globally stale exception patterns found", len(stalePatterns))
	}
	return nil
}

// aggregateGlobalStalePatterns parses go/analysis JSON output and returns
// stale exception patterns that were reported as stale in all analyzed
// packages. Aggregation is package-based (not diagnostic-count based), so
// duplicate stale diagnostics in one package do not inflate global coverage.
func aggregateGlobalStalePatterns(data []byte) (stalePatterns []string, totalPatterns, totalPackages int, err error) {
	// pattern -> set(packagePath)
	patternPackages := make(map[string]map[string]bool)
	seenPackages := make(map[string]bool)

	if err := forEachAnalysisResult(data, func(result analysisResult) error {
		for pkgPath, analyzers := range result {
			seenPackages[pkgPath] = true
			diags, ok := analyzers["goplint"]
			if !ok {
				continue
			}
			for _, d := range diags {
				if d.Category != goplint.CategoryStaleException {
					continue
				}
				pattern := extractPatternFromStaleDiagnostic(d)
				if pattern == "" {
					continue
				}
				if patternPackages[pattern] == nil {
					patternPackages[pattern] = make(map[string]bool)
				}
				patternPackages[pattern][pkgPath] = true
			}
		}
		return nil
	}); err != nil {
		return nil, 0, 0, fmt.Errorf("decoding JSON: %w", err)
	}

	totalPackages = len(seenPackages)
	totalPatterns = len(patternPackages)
	if totalPackages == 0 || totalPatterns == 0 {
		return nil, totalPatterns, totalPackages, nil
	}

	for pattern, pkgSet := range patternPackages {
		if len(pkgSet) == totalPackages {
			stalePatterns = append(stalePatterns, pattern)
		}
	}
	slices.Sort(stalePatterns)
	return stalePatterns, totalPatterns, totalPackages, nil
}

// buildGlobalAuditArgs constructs args for the subprocess invocation by
// removing --global and ensuring -json and -audit-exceptions are present.
func buildGlobalAuditArgs(args []string) []string {
	filtered := removeFlagWithOptionalValue(args, "global", true)
	return filterAndEnsureFlags(filtered, func(string) bool { return false }, []string{"-json", "-audit-exceptions"})
}

// parseFlagToken matches one flag token against flagName and returns whether it
// matched, optional inline value (for --flag=value), and whether a value was
// present inline.
func parseFlagToken(arg, flagName string) (matched bool, value string, hasInlineValue bool) {
	trimmed := strings.TrimLeft(arg, "-")
	if trimmed == flagName {
		return true, "", false
	}
	prefix := flagName + "="
	if after, ok := strings.CutPrefix(trimmed, prefix); ok {
		return true, after, true
	}
	return false, "", false
}

// removeFlagWithOptionalValue strips flagName from args. For value-style flags
// (consumeOptionalBoolValue=false), a following non-flag token is also removed.
// For bool-style flags (consumeOptionalBoolValue=true), a following token is
// removed only when it parses as a bool.
func removeFlagWithOptionalValue(args []string, flagName string, consumeOptionalBoolValue bool) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		matched, _, hasInlineValue := parseFlagToken(args[i], flagName)
		if !matched {
			out = append(out, args[i])
			continue
		}
		if hasInlineValue || i+1 >= len(args) {
			continue
		}
		next := args[i+1]
		if consumeOptionalBoolValue {
			if _, err := strconv.ParseBool(next); err == nil {
				i++
			}
			continue
		}
		if !strings.HasPrefix(next, "-") {
			i++
		}
	}
	return out
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
			if matched, _, _ := parseFlagToken(arg, strings.TrimLeft(rf, "-")); matched {
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

// tolerateAnalyzerExit accepts a non-zero subprocess exit only when it is an
// ExitError and JSON output exists on stdout. singlechecker exits non-zero
// when diagnostics are reported, which is expected for baseline generation.
func tolerateAnalyzerExit(runErr error, stdout []byte) error {
	if runErr == nil {
		return nil
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); !ok || exitErr == nil {
		return runErr
	}
	if len(bytes.TrimSpace(stdout)) == 0 {
		return runErr
	}
	if _, err := parseAnalysisJSON(stdout); err != nil {
		return fmt.Errorf("invalid analyzer JSON output: %w", err)
	}
	return nil
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

func extractPatternFromStaleDiagnostic(diag analysisDiagnostic) string {
	if pattern := goplint.FindingMetaFromDiagnosticURL(diag.URL, "pattern"); pattern != "" {
		return pattern
	}
	return extractPatternFromStaleMessage(diag.Message)
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

func parseFindingsJSONL(data []byte) (map[string][]goplint.BaselineFinding, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string][]goplint.BaselineFinding{}, nil
	}

	seen := make(map[string]map[string]goplint.BaselineFinding)
	if err := forEachFindingsRecord(data, func(record goplint.FindingStreamRecord) error {
		if record.Kind != "" && record.Kind != "finding" {
			return nil
		}
		if record.Category == "" || record.Message == "" || record.ID == "" {
			return errors.New("decoding findings record: missing required fields")
		}
		if !goplint.IsKnownDiagnosticCategory(record.Category) {
			return fmt.Errorf("unknown goplint category %q", record.Category)
		}
		if !goplint.IsBaselineSuppressibleCategory(record.Category) {
			return nil
		}
		if seen[record.Category] == nil {
			seen[record.Category] = make(map[string]goplint.BaselineFinding)
		}
		seen[record.Category][record.ID] = goplint.BaselineFinding{
			ID:      record.ID,
			Message: record.Message,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	findings := make(map[string][]goplint.BaselineFinding, len(seen))
	for category, entries := range seen {
		out := make([]goplint.BaselineFinding, 0, len(entries))
		for _, entry := range entries {
			out = append(out, entry)
		}
		findings[category] = out
	}
	return findings, nil
}

func forEachFindingsRecord(data []byte, fn func(record goplint.FindingStreamRecord) error) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}

	scanner := bytes.NewBuffer(data)
	for {
		line, err := scanner.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("reading findings stream: %w", err)
		}
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			var record goplint.FindingStreamRecord
			if unmarshalErr := json.Unmarshal(line, &record); unmarshalErr != nil {
				return fmt.Errorf("decoding findings record: %w", unmarshalErr)
			}
			if fn != nil {
				if callbackErr := fn(record); callbackErr != nil {
					return callbackErr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func countBaselineFindings(findings map[string][]goplint.BaselineFinding) int {
	total := 0
	for _, entries := range findings {
		total += len(entries)
	}
	return total
}

// parseAnalysisJSON parses the go/analysis -json output (one JSON object
// per package, concatenated) and returns findings grouped by category.
// Filters out stale-exception diagnostics and deduplicates.
func parseAnalysisJSON(data []byte) (map[string][]goplint.BaselineFinding, error) {
	// Deduplicate across packages (test variants can produce duplicates).
	// Keyed by category and stable finding ID.
	seen := make(map[string]map[string]goplint.BaselineFinding) // category → findingID → finding

	if err := forEachAnalysisResult(data, func(result analysisResult) error {
		for _, analyzers := range result {
			diags, ok := analyzers["goplint"]
			if !ok {
				continue
			}
			for _, d := range diags {
				if d.Category == "" || d.Message == "" {
					continue
				}
				if !goplint.IsKnownDiagnosticCategory(d.Category) {
					return fmt.Errorf("unknown goplint category %q", d.Category)
				}
				if !goplint.IsBaselineSuppressibleCategory(d.Category) {
					continue
				}
				findingID := goplint.FindingIDFromDiagnosticURL(d.URL)
				if findingID == "" {
					// go/analysis JSON output does not always include diagnostic URL
					// fields. Use a deterministic fallback identity for stream-count
					// parity checks when URL metadata is unavailable.
					findingID = goplint.StableFindingID(d.Category, d.Posn, d.Message)
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
		return nil
	}); err != nil {
		return nil, fmt.Errorf("decoding JSON object: %w", err)
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

func runAnalyzerSubprocess(runCommand commandRunner, stderr io.Writer, subArgs []string) ([]byte, error) {
	if runCommand == nil {
		runCommand = defaultRunCommand
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	selfPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolving executable path: %w", err)
	}

	cmd := exec.CommandContext(context.Background(), selfPath, subArgs...)
	cmd.Stderr = stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	runErr := runCommand(cmd)
	if err := tolerateAnalyzerExit(runErr, stdout.Bytes()); err != nil {
		return nil, err
	}

	return stdout.Bytes(), nil
}

func forEachAnalysisResult(data []byte, fn func(result analysisResult) error) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var result analysisResult
		if err := decoder.Decode(&result); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decoding analysis stream: %w", err)
		}
		if fn == nil {
			continue
		}
		if err := fn(result); err != nil {
			return err
		}
	}
}
