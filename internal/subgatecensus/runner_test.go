// SPDX-License-Identifier: MPL-2.0

package subgatecensus

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDerivePopulationsRejectsMissingDuplicateSkippedAndExtraMembers(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		FormatVersion: FormatVersion,
		Runs: []Run{{
			ID: "tests", Packages: []string{"./goplint"}, Tests: []string{"TestRequired"}, Count: 2,
		}},
		Populations: []Population{{
			ID: "test-events",
			Selectors: []Selector{{
				Run: "tests", Scope: ScopeAllTests,
				Members: []string{"TestRequired", "TestRequired/member"},
			}},
		}},
	}
	base := runObservation{
		run:              manifest.Runs[0],
		resolvedPackages: []string{"example.test/goplint"},
		passes: map[string]int{
			"example.test/goplint\x00TestRequired":        2,
			"example.test/goplint\x00TestRequired/member": 2,
		},
		skips:         make(map[string]int),
		packagePasses: map[string]int{"example.test/goplint": 1},
	}
	populations, err := derivePopulations(manifest, map[string]runObservation{"tests": base})
	if err != nil {
		t.Fatalf("derivePopulations() error: %v", err)
	}
	if len(populations) != 1 || populations[0].Count != 4 {
		t.Fatalf("populations = %+v, want one four-member population", populations)
	}

	tests := []struct {
		name      string
		mutate    func(*runObservation)
		wantError string
	}{
		{
			name: "missing or renamed required root",
			mutate: func(observation *runObservation) {
				delete(observation.passes, "example.test/goplint\x00TestRequired")
			},
			wantError: "pass count = 0",
		},
		{
			name: "duplicate current observation",
			mutate: func(observation *runObservation) {
				observation.passes["example.test/goplint\x00TestRequired"] = 3
			},
			wantError: "pass count = 3",
		},
		{
			name: "skipped member",
			mutate: func(observation *runObservation) {
				observation.skips["example.test/goplint\x00TestRequired/member"] = 1
			},
			wantError: "was skipped",
		},
		{
			name: "undeclared extra member",
			mutate: func(observation *runObservation) {
				observation.passes["example.test/goplint\x00TestRequired/extra"] = 2
			},
			wantError: "undeclared selected test",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			observation := base
			observation.passes = cloneCounts(base.passes)
			observation.skips = cloneCounts(base.skips)
			test.mutate(&observation)
			_, err := derivePopulations(manifest, map[string]runObservation{"tests": observation})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("derivePopulations() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func cloneCounts(source map[string]int) map[string]int {
	cloned := make(map[string]int, len(source))
	maps.Copy(cloned, source)
	return cloned
}

func TestEveryLiveCensusManifestRejectsDeletedOrRenamedRequiredTest(t *testing.T) {
	t.Parallel()

	manifestDirectory := filepath.Join("..", "..", "testdata", "subgates")
	entries, err := os.ReadDir(manifestDirectory)
	if err != nil {
		t.Fatalf("os.ReadDir(%q) error: %v", manifestDirectory, err)
	}
	manifestCount := 0
	testCount := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		manifestPath := filepath.Join(manifestDirectory, entry.Name())
		isCensus, discoveryErr := hasCensusManifestShape(manifestPath)
		if discoveryErr != nil {
			t.Fatalf("hasCensusManifestShape(%q) error: %v", entry.Name(), discoveryErr)
		}
		if !isCensus {
			continue
		}
		manifest, loadErr := Load(manifestPath)
		if loadErr != nil {
			t.Fatalf("Load(%q) error: %v", entry.Name(), loadErr)
		}
		manifestCount++
		for _, run := range manifest.Runs {
			for index, required := range run.Tests {
				testCount++
				name := entry.Name() + "/" + run.ID + "/" + required
				t.Run(name+"/deleted", func(t *testing.T) {
					t.Parallel()

					enumerated := slices.Delete(slices.Clone(run.Tests), index, index+1)
					assertEnumerationError(t, run.Tests, enumerated, required)
				})
				t.Run(name+"/renamed", func(t *testing.T) {
					t.Parallel()

					enumerated := slices.Clone(run.Tests)
					enumerated[index] = required + "Renamed"
					assertEnumerationError(t, run.Tests, enumerated, required)
				})
			}
		}
	}
	if manifestCount == 0 || testCount == 0 {
		t.Fatalf("live census = %d manifests and %d required tests, want nonzero", manifestCount, testCount)
	}
}

func TestCensusManifestDiscoveryIgnoresOtherSubgateContracts(t *testing.T) {
	t.Parallel()

	manifestPath := filepath.Join(
		"..",
		"..",
		"testdata",
		"subgates",
		"mutation-kernel-coverage.v1.json",
	)
	isCensus, err := hasCensusManifestShape(manifestPath)
	if err != nil {
		t.Fatalf("hasCensusManifestShape() error: %v", err)
	}
	if isCensus {
		t.Fatal("mutation kernel coverage contract was classified as a census manifest")
	}
}

func hasCensusManifestShape(manifestPath string) (bool, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return false, fmt.Errorf("read subgate manifest: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return false, fmt.Errorf("decode subgate manifest shape: %w", err)
	}
	_, hasRuns := fields["runs"]
	_, hasPopulations := fields["populations"]
	if hasRuns != hasPopulations {
		return false, errors.New("partial census manifest shape")
	}
	return hasRuns, nil
}

func assertEnumerationError(t *testing.T, required, enumerated []string, missing string) {
	t.Helper()
	err := validateRequiredTestEnumeration("example.test/goplint", required, enumerated)
	if err == nil || !strings.Contains(err.Error(), "missing required test \""+missing+"\"") {
		t.Fatalf("validateRequiredTestEnumeration() error = %v, want missing %q", err, missing)
	}
}
