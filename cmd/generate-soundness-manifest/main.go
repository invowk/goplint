// SPDX-License-Identifier: MPL-2.0

// Command generate-soundness-manifest materializes the reviewed aggregate
// subgate command and profile contract.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/mutationkernel"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
	"github.com/invowk/invowk/tools/goplint/internal/subgatecensus"
)

func main() {
	registryPath := flag.String("registry", "spec/semantic-evidence.v2.json", "executable evidence registry")
	output := flag.String("output", "spec/soundness-gate.v1.json", "generated aggregate manifest")
	flag.Parse()
	if err := generate(*registryPath, *output); err != nil {
		fmt.Fprintln(os.Stderr, "generate goplint soundness manifest:", err)
		os.Exit(1)
	}
}

func generate(registryPath, outputPath string) error {
	registry, err := soundnessevidence.LoadRegistry(context.Background(), registryPath)
	if err != nil {
		return fmt.Errorf("load semantic evidence registry: %w", err)
	}
	manifest, err := buildManifest(registry)
	if err != nil {
		return err
	}
	if err := manifest.Validate(registry); err != nil {
		return fmt.Errorf("validate generated soundness manifest: %w", err)
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode soundness manifest: %w", err)
	}
	encoded = append(encoded, '\n')
	temporaryPath := outputPath + ".tmp"
	if err := os.WriteFile(temporaryPath, encoded, 0o600); err != nil {
		return fmt.Errorf("write temporary soundness manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish soundness manifest: %w", err)
	}
	return nil
}

func buildManifest(registry soundnessevidence.Registry) (soundnessgate.Manifest, error) {
	mutationKernelRequirements, err := mutationKernelPopulationRequirements()
	if err != nil {
		return soundnessgate.Manifest{}, err
	}
	censusRequirements := make(map[string]soundnessgate.PopulationRequirement)
	for _, entry := range []struct {
		manifest   string
		population string
	}{
		{manifest: "aggregate-contract.v1.json", population: "contract-packages"},
		{manifest: "architecture.v1.json", population: "architecture-guards"},
		{manifest: "cfg-refinement.v1.json", population: "refinement-tests"},
		{manifest: "counterexamples.v1.json", population: "counterexamples"},
		{manifest: "determinism.v1.json", population: "determinism-runs"},
		{manifest: "determinism.v1.json", population: "protocol-categories"},
		{manifest: "production-integration.v1.json", population: "production-tests"},
		{manifest: "production-integration.v1.json", population: "protocol-categories"},
	} {
		requirement, err := censusPopulationRequirement(entry.manifest, entry.population)
		if err != nil {
			return soundnessgate.Manifest{}, err
		}
		censusRequirements[entry.manifest+"\x00"+entry.population] = requirement
	}
	subgates := []soundnessgate.Subgate{
		newSubgate(
			"aggregate-contract",
			"tools/goplint",
			[]string{"./scripts/check-aggregate-contract.sh"},
			120,
			"report.json",
			nil,
			censusRequirements["aggregate-contract.v1.json\x00contract-packages"],
		),
		newSubgate(
			"architecture",
			"tools/goplint",
			[]string{"./scripts/check-production-architecture.sh"},
			120,
			"report.json",
			nil,
			censusRequirements["architecture.v1.json\x00architecture-guards"],
		),
		newSubgate(
			"baseline",
			".",
			[]string{"make", "check-baseline"},
			900,
			"report.json",
			nil,
			population("repository-scans", 1),
		),
		newSubgate(
			"benchmarks",
			"tools/goplint",
			[]string{"./scripts/check-cfg-bench-thresholds.sh"},
			3600,
			"report.json",
			nil,
			population("analyzer-benchmarks", 1),
			population("generated-programs", 42),
		),
		newSubgate(
			"catalog",
			"tools/goplint",
			[]string{"./scripts/check-semantic-spec.sh"},
			300,
			"report.json",
			registrationIDs(registry, "catalog"),
			population("protocol-categories", 8),
			population("semantic-registrations", 88),
		),
		newSubgate(
			"cfg-refinement",
			"tools/goplint",
			[]string{"./scripts/check-cfg-refinement.sh"},
			300,
			"report.json",
			nil,
			censusRequirements["cfg-refinement.v1.json\x00refinement-tests"],
		),
		newSubgate(
			"clean-tree-freshness",
			"tools/goplint",
			[]string{
				"go",
				"run",
				"./cmd/check-clean-tree-evidence",
				"-root",
				"../..",
				"-paths",
				"tools/goplint/testdata/gates/clean-tree-v3.paths",
				"-plan",
				"tools/goplint/testdata/gates/clean-tree-v3.json",
				"-evidence",
				"tools/goplint/testdata/gates/clean-tree-run.v3.json",
			},
			3600,
			"clean-tree-freshness.json",
			nil,
			population("verified-clean-tree-records", 1),
		),
		newSubgate(
			"counterexamples",
			"tools/goplint",
			[]string{"./scripts/check-counterexamples.sh"},
			300,
			"report.json",
			nil,
			censusRequirements["counterexamples.v1.json\x00counterexamples"],
		),
		newSubgate(
			"determinism",
			"tools/goplint",
			[]string{"./scripts/check-protocol-determinism.sh"},
			900,
			"report.json",
			registrationIDs(registry, "determinism"),
			censusRequirements["determinism.v1.json\x00determinism-runs"],
			censusRequirements["determinism.v1.json\x00protocol-categories"],
		),
		newSubgate(
			"end-to-end-oracle",
			"tools/goplint",
			[]string{"./scripts/check-protocol-oracle-e2e.sh"},
			600,
			"report.json",
			registrationIDs(registry, "end-to-end-oracle"),
			population("generated-programs", 42),
			population("perturbations", 7),
		),
		newSubgate(
			"full-scan",
			".",
			[]string{"make", "check-goplint-full-scan"},
			900,
			"report.json",
			nil,
			population("repository-scans", 1),
		),
		newSubgate(
			"fuzz-seeds",
			"tools/goplint",
			[]string{"./scripts/check-fuzz-seeds.sh"},
			600,
			"report.json",
			registrationIDs(registry, "fuzz-seeds"),
			population("decoded-seeds", 35),
			population("historical-counterexamples", 24),
			population("protocol-categories", 8),
		),
		newSubgate(
			"inconclusive-suppression",
			"tools/goplint",
			[]string{"./scripts/check-inconclusive-suppression.sh"},
			180,
			"report.json",
			nil,
			population("suppression-surfaces", 5),
		),
		newSubgate(
			"mutation-kernel-coverage",
			"tools/goplint",
			[]string{
				"go",
				"run",
				"./cmd/mutation-kernel-coverage",
				"-root",
				".",
				"-manifest",
				"testdata/subgates/mutation-kernel-coverage.v1.json",
			},
			120,
			"report.json",
			nil,
			mutationKernelRequirements...,
		),
		newSubgate(
			"production-integration",
			"tools/goplint",
			[]string{"./scripts/check-production-integration.sh"},
			600,
			"report.json",
			registrationIDs(registry, "production-integration"),
			censusRequirements["production-integration.v1.json\x00production-tests"],
			censusRequirements["production-integration.v1.json\x00protocol-categories"],
		),
		newSubgate(
			"protocol-oracle",
			"tools/goplint",
			[]string{"./scripts/check-protocol-oracle.sh"},
			300,
			"report.json",
			registrationIDs(registry, "protocol-oracle"),
			population("independent-cases", 42),
			population("metamorphic-relations", 8),
		),
		newSubgate(
			"race-repeat",
			"tools/goplint",
			[]string{"./scripts/check-race-repeat.sh"},
			3600,
			"report.json",
			nil,
			population("race-runs", 1),
			population("repeat-runs", 3),
		),
		newSubgate(
			"scheduled-oracle",
			"tools/goplint",
			[]string{"./scripts/check-protocol-oracle-scheduled.sh"},
			3600,
			"report.json",
			nil,
			population("generated-programs", 2058),
			population("protocol-categories", 2),
		),
		newSubgate(
			"targeted-mutation",
			"tools/goplint",
			[]string{"go", "run", "./cmd/targeted-mutation", "-profile", "testdata/mutation/profiles/blocking-v2.json"},
			1800,
			"report.json",
			registrationIDs(registry, "targeted-mutation"),
			population("causal-mutants", 27),
			population("clean-controls", 54),
			population("intended-mismatches", 27),
			population("restorations", 27),
			population("selected-guards", 27),
		),
	}
	slices.SortFunc(subgates, func(left, right soundnessgate.Subgate) int {
		return strings.Compare(left.ID, right.ID)
	})
	allIDs := make([]string, 0, len(subgates))
	for _, subgate := range subgates {
		allIDs = append(allIDs, subgate.ID)
	}
	coreIDs := slices.DeleteFunc(slices.Clone(allIDs), func(subgateID string) bool {
		return subgateID == "clean-tree-freshness"
	})
	return soundnessgate.Manifest{
		FormatVersion: soundnessgate.ManifestFormatVersion,
		RegistryPath:  "tools/goplint/spec/semantic-evidence.v2.json",
		Profiles: []soundnessgate.Profile{
			{ID: soundnessgate.ProfileCore, SubgateIDs: coreIDs},
			{ID: soundnessgate.ProfileComplete, SubgateIDs: allIDs},
		},
		Subgates: subgates,
	}, nil
}

func mutationKernelPopulationRequirements() ([]soundnessgate.PopulationRequirement, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return nil, errors.New("resolve generator source path")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	result, err := mutationkernel.Load(
		context.Background(),
		moduleRoot,
		"testdata/subgates/mutation-kernel-coverage.v1.json",
	)
	if err != nil {
		return nil, fmt.Errorf("load mutation kernel coverage: %w", err)
	}
	requirements, err := result.PopulationRequirements()
	if err != nil {
		return nil, fmt.Errorf("derive mutation kernel populations: %w", err)
	}
	return requirements, nil
}

func censusPopulationRequirement(manifestName, populationID string) (soundnessgate.PopulationRequirement, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return soundnessgate.PopulationRequirement{}, errors.New("resolve generator source path")
	}
	manifest, err := subgatecensus.Load(filepath.Join(
		filepath.Dir(filename),
		"..",
		"..",
		"testdata",
		"subgates",
		manifestName,
	))
	if err != nil {
		return soundnessgate.PopulationRequirement{}, fmt.Errorf("load census %s: %w", manifestName, err)
	}
	counts, err := manifest.ExpectedPopulationCounts()
	if err != nil {
		return soundnessgate.PopulationRequirement{}, fmt.Errorf("derive census %s: %w", manifestName, err)
	}
	count, ok := counts[populationID]
	if !ok {
		return soundnessgate.PopulationRequirement{}, fmt.Errorf("census %s has no population %q", manifestName, populationID)
	}
	return population(populationID, count), nil
}

func newSubgate(
	id string,
	workingDirectory string,
	command []string,
	timeoutSeconds int,
	reportFile string,
	registrationIDs []string,
	populations ...soundnessgate.PopulationRequirement,
) soundnessgate.Subgate {
	if registrationIDs == nil {
		registrationIDs = []string{}
	}
	slices.SortFunc(populations, func(left, right soundnessgate.PopulationRequirement) int {
		return strings.Compare(left.ID, right.ID)
	})
	return soundnessgate.Subgate{
		ID:                      id,
		WorkingDirectory:        workingDirectory,
		Command:                 command,
		TimeoutSeconds:          timeoutSeconds,
		ReportFile:              reportFile,
		RequiredRegistrationIDs: registrationIDs,
		RequiredPopulations:     populations,
	}
}

func population(id string, minimum int) soundnessgate.PopulationRequirement {
	return soundnessgate.PopulationRequirement{ID: id, Minimum: minimum}
}

func registrationIDs(registry soundnessevidence.Registry, producerID string) []string {
	result := make([]string, 0)
	for _, registration := range registry.Registrations {
		if registration.ProducerID == producerID {
			result = append(result, registration.ID)
		}
	}
	slices.Sort(result)
	return result
}
