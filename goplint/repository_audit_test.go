// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestValidateFindingStreamCoverageIncludesAlwaysVisibleCategories(t *testing.T) {
	t.Parallel()

	analysis := []byte(`{"example.com/a":{"goplint":[{"posn":"a.go:1:1","message":"inconclusive","category":"unvalidated-cast-inconclusive"}]}}`)
	findings := []byte(`{"package":"example.com/a","category":"unvalidated-cast-inconclusive","id":"gpl3_test","message":"inconclusive","posn":"a.go:1:1"}` + "\n")
	if err := ValidateFindingStreamCoverage(findings, analysis); err != nil {
		t.Fatalf("ValidateFindingStreamCoverage() error = %v", err)
	}
	if err := ValidateFindingStreamCoverage(nil, analysis); err == nil {
		t.Fatal("ValidateFindingStreamCoverage() accepted missing always-visible finding")
	}
}

func TestAnalysisPackageCensusIncludesPackagesWithoutDiagnostics(t *testing.T) {
	t.Parallel()

	packages, err := AnalysisPackageCensus([]byte(`{"example.com/b":{},"example.com/a":{"goplint":[]}}`))
	if err != nil {
		t.Fatalf("AnalysisPackageCensus() error = %v", err)
	}
	if len(packages) != 2 || packages[0] != "example.com/a" || packages[1] != "example.com/b" {
		t.Fatalf("AnalysisPackageCensus() = %q", packages)
	}
}
