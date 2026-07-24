// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"bytes"
	"fmt"
	"strings"
)

// CompareNormalizedRunReports requires byte-identical normalized evidence from
// two aggregate run reports. Run-scoped identities are removed before the
// comparison; any divergence — a lost observation, a population mismatch, a
// different verdict — is named through the first differing normalized line.
func CompareNormalizedRunReports(reference, candidate RunReport) error {
	referenceBytes, err := NormalizedRunReportJSON(reference)
	if err != nil {
		return fmt.Errorf("normalize reference soundness run report: %w", err)
	}
	candidateBytes, err := NormalizedRunReportJSON(candidate)
	if err != nil {
		return fmt.Errorf("normalize candidate soundness run report: %w", err)
	}
	if bytes.Equal(referenceBytes, candidateBytes) {
		return nil
	}
	referenceLines := strings.Split(string(referenceBytes), "\n")
	candidateLines := strings.Split(string(candidateBytes), "\n")
	limit := min(len(referenceLines), len(candidateLines))
	for index := range limit {
		if referenceLines[index] != candidateLines[index] {
			return fmt.Errorf(
				"normalized soundness run reports diverge at line %d near %s: reference %q, candidate %q",
				index+1,
				nearestIdentifierLine(referenceLines, index),
				strings.TrimSpace(referenceLines[index]),
				strings.TrimSpace(candidateLines[index]),
			)
		}
	}
	return fmt.Errorf(
		"normalized soundness run reports diverge in length: reference %d lines, candidate %d lines",
		len(referenceLines),
		len(candidateLines),
	)
}

// nearestIdentifierLine names the closest preceding identifier so population
// and observation divergences carry their owning identity.
func nearestIdentifierLine(lines []string, index int) string {
	for cursor := index; cursor >= 0; cursor-- {
		trimmed := strings.TrimSpace(lines[cursor])
		if strings.HasPrefix(trimmed, `"id":`) || strings.HasPrefix(trimmed, `"registration_id":`) {
			return strings.TrimSuffix(trimmed, ",")
		}
	}
	return "the report header"
}
