// SPDX-License-Identifier: MPL-2.0

package main

import (
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func TestCompareNormalizedReportsIgnoresRunScopedIdentity(t *testing.T) {
	t.Parallel()

	reference := soundnessgate.RunReport{
		FormatVersion: soundnessgate.RunReportFormatVersion,
		Profile:       soundnessgate.ProfileSemantic,
		RunID:         "serial-reference",
	}
	candidate := reference
	candidate.RunID = "optimized-aggregate"

	referenceDigest, candidateDigest, identical, err := compareNormalizedReports(reference, candidate)
	if err != nil {
		t.Fatalf("compare normalized reports: %v", err)
	}
	if !identical {
		t.Fatalf("normalized reports unexpectedly differ: reference=%s candidate=%s", referenceDigest, candidateDigest)
	}
	if referenceDigest != candidateDigest {
		t.Fatalf("normalized digest mismatch: reference=%s candidate=%s", referenceDigest, candidateDigest)
	}
}

func TestCompareNormalizedReportsRejectsSemanticDrift(t *testing.T) {
	t.Parallel()

	reference := soundnessgate.RunReport{
		FormatVersion: soundnessgate.RunReportFormatVersion,
		Profile:       soundnessgate.ProfileSemantic,
	}
	candidate := reference
	candidate.WorkspaceDigest = "changed"

	referenceDigest, candidateDigest, identical, err := compareNormalizedReports(reference, candidate)
	if err != nil {
		t.Fatalf("compare normalized reports: %v", err)
	}
	if identical {
		t.Fatal("normalized reports unexpectedly match")
	}
	if referenceDigest == candidateDigest {
		t.Fatalf("different reports have identical digest %s", referenceDigest)
	}
}
