// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"errors"
	"fmt"
	"slices"
)

// CollectGlobalStaleExceptionPatternsFromStreams aggregates stale exceptions
// using go/analysis JSON for the complete package set and the internal findings
// stream for canonical pattern metadata. The analysis driver does not preserve
// analysis.Diagnostic.URL, so its JSON cannot be the metadata authority.
func CollectGlobalStaleExceptionPatternsFromStreams(
	analysisData, findingsData []byte,
) (stalePatterns []string, totalPatterns, totalPackages int, err error) {
	type diagnosticKey struct {
		pkg     string
		message string
		posn    string
	}

	seenPackages := make(map[string]bool)
	analysisCounts := make(map[diagnosticKey]int)
	if err := ForEachAnalysisResult(analysisData, func(result AnalysisResult) error {
		for pkgPath, analyzers := range result {
			seenPackages[pkgPath] = true
			for _, diag := range analyzers["goplint"] {
				if diag.Category != CategoryStaleException {
					continue
				}
				analysisCounts[diagnosticKey{pkg: pkgPath, message: diag.Message, posn: diag.Posn}]++
			}
		}
		return nil
	}); err != nil {
		return nil, 0, 0, fmt.Errorf("decoding analysis JSON: %w", err)
	}

	streamCounts := make(map[diagnosticKey]int)
	patternPackages := make(map[string]map[string]bool)
	if err := forEachFindingsRecord(findingsData, func(record FindingStreamRecord) error {
		if record.Kind != "" && record.Kind != "finding" {
			return nil
		}
		if record.Category != CategoryStaleException {
			return nil
		}
		pattern := record.Meta["pattern"]
		if record.Package == "" || record.ID == "" || record.Message == "" || pattern == "" {
			return errors.New("stale-exception findings record is missing canonical package, ID, message, or pattern metadata")
		}
		if !seenPackages[record.Package] {
			return fmt.Errorf("stale-exception findings record references unanalyzed package %q", record.Package)
		}
		streamCounts[diagnosticKey{pkg: record.Package, message: record.Message, posn: record.Posn}]++
		if patternPackages[pattern] == nil {
			patternPackages[pattern] = make(map[string]bool)
		}
		patternPackages[pattern][record.Package] = true
		return nil
	}); err != nil {
		return nil, 0, 0, fmt.Errorf("decoding findings stream: %w", err)
	}

	for key, count := range analysisCounts {
		if streamCounts[key] < count {
			return nil, 0, 0, fmt.Errorf(
				"findings stream is missing %d stale-exception occurrence(s) for package %q at %q",
				count-streamCounts[key], key.pkg, key.posn,
			)
		}
	}

	totalPackages = len(seenPackages)
	totalPatterns = len(patternPackages)
	for pattern, pkgSet := range patternPackages {
		if len(pkgSet) == totalPackages {
			stalePatterns = append(stalePatterns, pattern)
		}
	}
	slices.Sort(stalePatterns)
	return stalePatterns, totalPatterns, totalPackages, nil
}

// CollectGlobalStaleExceptionPatterns parses go/analysis JSON output and
// returns stale exception patterns that were reported as stale in all analyzed
// packages. Aggregation is package-based, so duplicate stale diagnostics in one
// package do not inflate global coverage.
func CollectGlobalStaleExceptionPatterns(data []byte) (stalePatterns []string, totalPatterns, totalPackages int, err error) {
	patternPackages := make(map[string]map[string]bool)
	seenPackages := make(map[string]bool)

	if err := ForEachAnalysisResult(data, func(result AnalysisResult) error {
		for pkgPath, analyzers := range result {
			seenPackages[pkgPath] = true
			diags, ok := analyzers["goplint"]
			if !ok {
				continue
			}
			for _, diag := range diags {
				if diag.Category != CategoryStaleException {
					continue
				}
				pattern := StaleExceptionPatternFromDiagnostic(diag)
				if pattern == "" {
					return fmt.Errorf("package %q stale-exception diagnostic is missing canonical pattern metadata", pkgPath)
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

// StaleExceptionPatternFromDiagnostic extracts the exception pattern from a
// stale-exception diagnostic.
func StaleExceptionPatternFromDiagnostic(diag AnalysisDiagnostic) string {
	if pattern := FindingMetaFromDiagnosticURL(diag.URL, "pattern"); pattern != "" {
		return pattern
	}
	return ""
}
