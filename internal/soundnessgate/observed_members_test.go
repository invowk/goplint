// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPopulationsFromObservedMembersDerivesCanonicalCounts(t *testing.T) {
	t.Parallel()

	populations, err := PopulationsFromObservedMembers([]ObservedMember{
		{PopulationID: "zeta", MemberID: "case-b"},
		{PopulationID: "alpha", MemberID: "case-a"},
		{PopulationID: "zeta", MemberID: "case-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(populations) != 2 || populations[0] != (Population{ID: "alpha", Count: 1}) ||
		populations[1] != (Population{ID: "zeta", Count: 2}) {
		t.Fatalf("PopulationsFromObservedMembers() = %+v", populations)
	}
}

func TestPopulationsFromObservedMembersRejectsMissingDuplicateOrInvalidMembers(t *testing.T) {
	t.Parallel()

	tests := [][]ObservedMember{
		nil,
		{{PopulationID: "", MemberID: "case"}},
		{{PopulationID: "cases", MemberID: ""}},
		{
			{PopulationID: "cases", MemberID: "duplicate"},
			{PopulationID: "cases", MemberID: "duplicate"},
		},
	}
	for _, observations := range tests {
		if populations, err := PopulationsFromObservedMembers(observations); err == nil {
			t.Errorf("PopulationsFromObservedMembers(%+v) = %+v, want error", observations, populations)
		}
	}
}

func TestProductionReportersCannotReintroduceLiteralPopulationCounts(t *testing.T) {
	t.Parallel()

	moduleRoot := filepath.Clean(filepath.Join("..", ".."))
	moduleFS, err := os.OpenRoot(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := moduleFS.Close(); err != nil {
			t.Errorf("close module root: %v", err)
		}
	})
	err = filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == "internal/subgatecensus/runner.go" {
			return nil
		}
		data, err := moduleFS.ReadFile(relative)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "soundnessgate.Population{") {
			t.Errorf("production reporter %s constructs a literal population instead of observed members", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean(filepath.Join(moduleRoot, "..", ".."))
	paths := []string{filepath.Join(repositoryRoot, "Makefile")}
	scripts, err := filepath.Glob(filepath.Join(moduleRoot, "scripts", "*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, scripts...)
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(data), "-population") {
			t.Errorf("runtime reporter %s retains the literal -population escape hatch", path)
		}
	}
}
