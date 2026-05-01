// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"slices"
	"strings"
)

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

// StaleExceptionPatternFromDiagnostic extracts the exception pattern from a
// stale-exception diagnostic.
func StaleExceptionPatternFromDiagnostic(diag AnalysisDiagnostic) string {
	if pattern := FindingMetaFromDiagnosticURL(diag.URL, "pattern"); pattern != "" {
		return pattern
	}
	return staleExceptionPatternFromMessage(diag.Message)
}

func staleExceptionPatternFromMessage(message string) string {
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
