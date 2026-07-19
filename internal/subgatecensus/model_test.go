// SPDX-License-Identifier: MPL-2.0

package subgatecensus

import (
	"strings"
	"testing"
)

func TestManifestExpectedPopulationCountsAreMemberDerived(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		FormatVersion: FormatVersion,
		Runs: []Run{
			{
				ID:       "category",
				Packages: []string{"./goplint"},
				Tests:    []string{"TestCategory"},
				Count:    1,
			},
			{
				ID:       "repeated",
				Packages: []string{"./goplint"},
				Tests:    []string{"TestReal", "TestProtocol", "TestRefinement"},
				Count:    5,
			},
		},
		Populations: []Population{
			{
				ID: "determinism-runs",
				Selectors: []Selector{
					{
						Run: "category", Scope: ScopeAllTests,
						Members: []string{"TestCategory", "TestCategory/a", "TestCategory/b"},
					},
					{
						Run: "repeated", Scope: ScopeAllTests,
						Members: []string{
							"TestReal",
							"TestProtocol",
							"TestProtocol/first",
							"TestProtocol/second",
							"TestRefinement",
						},
					},
				},
			},
			{
				ID: "protocol-categories",
				Selectors: []Selector{{
					Run: "category", Scope: ScopeImmediateSubtests, Root: "TestCategory", Members: []string{"a", "b"},
				}},
			},
		},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	counts, err := manifest.ExpectedPopulationCounts()
	if err != nil {
		t.Fatalf("ExpectedPopulationCounts() error: %v", err)
	}
	if counts["determinism-runs"] != 28 || counts["protocol-categories"] != 2 {
		t.Fatalf("population counts = %v, want determinism-runs=28 protocol-categories=2", counts)
	}
}

func TestManifestRejectsHardCodedOrAmbiguousSelectors(t *testing.T) {
	t.Parallel()

	base := Manifest{
		FormatVersion: FormatVersion,
		Runs:          []Run{{ID: "tests", Packages: []string{"./goplint"}, Tests: []string{"TestRequired"}, Count: 1}},
		Populations:   []Population{{ID: "tests", Selectors: []Selector{{Run: "tests", Scope: ScopeAllTests}}}},
	}
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "exact members") {
		t.Fatalf("Validate() error = %v, want exact-member rejection", err)
	}

	base.Populations[0].Selectors[0] = Selector{
		Run: "tests", Scope: ScopeAllTests, Members: []string{"TestRenamed"},
	}
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "outside the run test roots") {
		t.Fatalf("Validate() error = %v, want disconnected-member rejection", err)
	}
}
