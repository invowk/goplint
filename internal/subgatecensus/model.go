// SPDX-License-Identifier: MPL-2.0

// Package subgatecensus runs manifest-declared subgate tests and derives
// report populations exclusively from the current invocation's observations.
package subgatecensus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

const (
	FormatVersion = 1

	ScopeRoots             = "roots"
	ScopeAllTests          = "all-tests"
	ScopeImmediateSubtests = "immediate-subtests"
	ScopePackages          = "packages"
)

// Manifest declares commands and the exact observations credited to a subgate.
type Manifest struct {
	FormatVersion int          `json:"format_version"`
	Runs          []Run        `json:"runs"`
	Populations   []Population `json:"populations"`
}

// Run is one current-run go test invocation.
type Run struct {
	ID             string   `json:"id"`
	Packages       []string `json:"packages"`
	Tests          []string `json:"tests,omitempty"`
	Count          int      `json:"count"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// Population derives one report population from exact run selectors.
type Population struct {
	ID        string     `json:"id"`
	Selectors []Selector `json:"selectors"`
}

// Selector identifies which successful observations a population credits.
type Selector struct {
	Run     string   `json:"run"`
	Scope   string   `json:"scope"`
	Root    string   `json:"root,omitempty"`
	Members []string `json:"members,omitempty"`
}

// Load decodes and validates a census manifest.
func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read census manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode census manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate checks that every count is derivable from named required members.
func (manifest Manifest) Validate() error {
	if manifest.FormatVersion != FormatVersion || len(manifest.Runs) == 0 || len(manifest.Populations) == 0 {
		return errors.New("empty or unsupported subgate census manifest")
	}
	runs := make(map[string]Run, len(manifest.Runs))
	for index, run := range manifest.Runs {
		if !canonicalID(run.ID) || len(run.Packages) == 0 || run.Count <= 0 || run.TimeoutSeconds < 0 {
			return fmt.Errorf("runs[%d] is incomplete", index)
		}
		if _, duplicate := runs[run.ID]; duplicate {
			return fmt.Errorf("duplicate run %q", run.ID)
		}
		if duplicatesOrEmpty(run.Packages) || duplicatesOrEmpty(run.Tests) {
			return fmt.Errorf("run %q has empty or duplicate package/test members", run.ID)
		}
		runs[run.ID] = run
	}
	seenPopulations := make(map[string]bool, len(manifest.Populations))
	for index, population := range manifest.Populations {
		if !canonicalID(population.ID) || seenPopulations[population.ID] || len(population.Selectors) == 0 {
			return fmt.Errorf("populations[%d] is incomplete or duplicate", index)
		}
		seenPopulations[population.ID] = true
		seenSelectors := make(map[string]bool, len(population.Selectors))
		for selectorIndex, selector := range population.Selectors {
			run, ok := runs[selector.Run]
			if !ok {
				return fmt.Errorf("population %q selector %d references unknown run %q", population.ID, selectorIndex, selector.Run)
			}
			key := selector.Run + "\x00" + selector.Scope + "\x00" + selector.Root
			if seenSelectors[key] {
				return fmt.Errorf("population %q has duplicate selector for run %q", population.ID, selector.Run)
			}
			seenSelectors[key] = true
			if err := validateSelector(selector, run); err != nil {
				return fmt.Errorf("population %q selector %d: %w", population.ID, selectorIndex, err)
			}
		}
	}
	_, err := manifest.ExpectedPopulationCounts()
	return err
}

func validateSelector(selector Selector, run Run) error {
	switch selector.Scope {
	case ScopeRoots:
		if selector.Root != "" || len(selector.Members) != 0 || len(run.Tests) == 0 || len(run.Packages) != 1 {
			return errors.New("roots selector requires one test package and derives members from run tests")
		}
	case ScopeAllTests:
		if selector.Root != "" || duplicatesOrEmpty(selector.Members) || len(selector.Members) == 0 || len(run.Packages) != 1 {
			return errors.New("all-tests selector requires one package and exact members")
		}
		for _, member := range selector.Members {
			matched := slices.ContainsFunc(run.Tests, func(root string) bool {
				return member == root || strings.HasPrefix(member, root+"/")
			})
			if !matched {
				return fmt.Errorf("member %q is outside the run test roots", member)
			}
		}
	case ScopeImmediateSubtests:
		if selector.Root == "" || !slices.Contains(run.Tests, selector.Root) ||
			duplicatesOrEmpty(selector.Members) || len(selector.Members) == 0 || len(run.Packages) != 1 {
			return errors.New("immediate-subtests selector requires one listed root and exact members")
		}
		for _, member := range selector.Members {
			if strings.Contains(member, "/") {
				return fmt.Errorf("immediate subtest member %q contains a nested separator", member)
			}
		}
	case ScopePackages:
		if selector.Root != "" || len(selector.Members) != 0 || len(run.Tests) != 0 || run.Count != 1 {
			return errors.New("packages selector requires a one-count package-only run")
		}
	default:
		return fmt.Errorf("unsupported scope %q", selector.Scope)
	}
	return nil
}

// ExpectedPopulationCounts derives manifest minima without numeric mirrors.
func (manifest Manifest) ExpectedPopulationCounts() (map[string]int, error) {
	runs := make(map[string]Run, len(manifest.Runs))
	for _, run := range manifest.Runs {
		runs[run.ID] = run
	}
	counts := make(map[string]int, len(manifest.Populations))
	for _, population := range manifest.Populations {
		count := 0
		for _, selector := range population.Selectors {
			run, ok := runs[selector.Run]
			if !ok {
				return nil, fmt.Errorf("population %q references unknown run %q", population.ID, selector.Run)
			}
			switch selector.Scope {
			case ScopeRoots:
				count += len(run.Tests) * run.Count
			case ScopeAllTests, ScopeImmediateSubtests:
				count += len(selector.Members) * run.Count
			case ScopePackages:
				count += len(run.Packages)
			default:
				return nil, fmt.Errorf("population %q has unsupported scope %q", population.ID, selector.Scope)
			}
		}
		if count <= 0 {
			return nil, fmt.Errorf("population %q derives a zero population", population.ID)
		}
		counts[population.ID] = count
	}
	return counts, nil
}

func canonicalID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
			continue
		}
		return false
	}
	return true
}

func duplicatesOrEmpty(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || value != strings.TrimSpace(value) || seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}
