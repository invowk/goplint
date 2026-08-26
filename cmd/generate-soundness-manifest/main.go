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

	"github.com/invowk/goplint/internal/mutationkernel"
	"github.com/invowk/goplint/internal/soundnessevidence"
	"github.com/invowk/goplint/internal/soundnessgate"
	"github.com/invowk/goplint/internal/subgatecensus"
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
			[]string{"./scripts/check-cfg-bench-thresholds.sh", "--phase", "algorithms"},
			3600,
			"report.json",
			nil,
			population("analyzer-benchmarks", 1),
			population("generated-programs", 42),
		),
		newSubgate(
			"benchmark-full-scan",
			"tools/goplint",
			[]string{"./scripts/check-cfg-bench-thresholds.sh", "--phase", "full-scan"},
			3600,
			"report.json",
			nil,
			population("certification-full-scans", 5),
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
				"tools/goplint/testdata/gates/clean-tree-v4.paths",
				"-plan",
				"tools/goplint/testdata/gates/clean-tree-v4.json",
				"-evidence",
				"tools/goplint/testdata/gates/clean-tree-run.v4.json",
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
			"exceptions",
			".",
			[]string{"make", "check-goplint-exceptions"},
			300,
			"report.json",
			nil,
			population("repository-scans", 1),
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
			"harness-parity",
			"tools/goplint",
			[]string{"go", "run", "./cmd/harness-parity"},
			600,
			"report.json",
			nil,
			population("parity-fixture-comparisons", 1),
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
			"module-tests",
			"tools/goplint",
			[]string{"./scripts/check-module-tests.sh"},
			1800,
			"report.json",
			nil,
			population("module-test-suites", 1),
		),
		newSubgate(
			"performance-smoke",
			"tools/goplint",
			[]string{"./scripts/check-performance-smoke.sh"},
			900,
			"report.json",
			nil,
			population("consumer-performance-smoke", 1),
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
		raceRepeatGroupSubgate(1),
		raceRepeatGroupSubgate(2),
		raceRepeatGroupSubgate(3),
		raceRepeatGroupSubgate(4),
		raceRepeatGroupSubgate(5),
		raceRepeatGroupSubgate(6),
		newSubgate(
			"race-repeat-supporting",
			"tools/goplint",
			[]string{"./scripts/check-race-repeat.sh", "--phase", "supporting"},
			1800,
			"report.json",
			nil,
			population("supporting-race-runs", 1),
			population("supporting-repeat-runs", 3),
		),
		newSubgate(
			"repository-audit",
			".",
			[]string{"make", "check-goplint-repository-audit"},
			900,
			"report.json",
			nil,
			population("repository-scans", 1),
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
	for index := range subgates {
		if err := applyExecutionPolicy(&subgates[index]); err != nil {
			return soundnessgate.Manifest{}, err
		}
	}
	slices.SortFunc(subgates, func(left, right soundnessgate.Subgate) int {
		return strings.Compare(left.ID, right.ID)
	})
	allIDs := make([]string, 0, len(subgates))
	for _, subgate := range subgates {
		allIDs = append(allIDs, subgate.ID)
	}
	semanticIDs := slices.DeleteFunc(slices.Clone(allIDs), func(subgateID string) bool {
		return subgateID == "clean-tree-freshness" || subgateID == "performance-smoke"
	})
	completeIDs := append(slices.Clone(semanticIDs), "clean-tree-freshness")
	slices.Sort(completeIDs)
	consumerIDs := []string{"baseline", "exceptions", "full-scan", "performance-smoke", "repository-audit"}
	harnessIDs := append(slices.Clone(consumerIDs), "harness-parity", "module-tests")
	slices.Sort(harnessIDs)
	return soundnessgate.Manifest{
		FormatVersion: soundnessgate.ManifestFormatVersion,
		RegistryPath:  "tools/goplint/spec/semantic-evidence.v2.json",
		Profiles: []soundnessgate.Profile{
			{ID: soundnessgate.ProfileComplete, SubgateIDs: completeIDs},
			{ID: soundnessgate.ProfileConsumer, SubgateIDs: consumerIDs},
			{ID: soundnessgate.ProfileHarness, SubgateIDs: harnessIDs},
			{ID: soundnessgate.ProfileSemantic, SubgateIDs: semanticIDs},
		},
		Subgates: subgates,
	}, nil
}

func applyExecutionPolicy(subgate *soundnessgate.Subgate) error {
	type policy struct {
		cpu       int
		memory    int64
		dependsOn []string
		exclusive []string
	}
	policies := map[string]policy{
		"aggregate-contract":       {cpu: 1, memory: 512 * 1024 * 1024},
		"architecture":             {cpu: 2, memory: 2 * 1024 * 1024 * 1024},
		"baseline":                 {cpu: 1, memory: 512 * 1024 * 1024, dependsOn: []string{"repository-audit"}},
		"benchmark-full-scan":      {cpu: 4, memory: 6 * 1024 * 1024 * 1024},
		"benchmarks":               {cpu: 4, memory: 6 * 1024 * 1024 * 1024},
		"catalog":                  {cpu: 2, memory: 4 * 1024 * 1024 * 1024},
		"cfg-refinement":           {cpu: 2, memory: 4 * 1024 * 1024 * 1024},
		"clean-tree-freshness":     {cpu: 1, memory: 1024 * 1024 * 1024},
		"counterexamples":          {cpu: 2, memory: 4 * 1024 * 1024 * 1024},
		"determinism":              {cpu: 4, memory: 8 * 1024 * 1024 * 1024},
		"end-to-end-oracle":        {cpu: 4, memory: 8 * 1024 * 1024 * 1024},
		"exceptions":               {cpu: 1, memory: 512 * 1024 * 1024, dependsOn: []string{"repository-audit"}},
		"full-scan":                {cpu: 1, memory: 512 * 1024 * 1024, dependsOn: []string{"repository-audit"}},
		"fuzz-seeds":               {cpu: 4, memory: 8 * 1024 * 1024 * 1024},
		"harness-parity":           {cpu: 2, memory: 2 * 1024 * 1024 * 1024},
		"inconclusive-suppression": {cpu: 1, memory: 1024 * 1024 * 1024},
		"module-tests":             {cpu: 4, memory: 8 * 1024 * 1024 * 1024},
		"mutation-kernel-coverage": {cpu: 1, memory: 1024 * 1024 * 1024},
		"performance-smoke":        {cpu: 1, memory: 512 * 1024 * 1024, dependsOn: []string{"repository-audit"}},
		"production-integration":   {cpu: 4, memory: 8 * 1024 * 1024 * 1024},
		"protocol-oracle":          {cpu: 4, memory: 4 * 1024 * 1024 * 1024},
		"race-repeat-1":            {cpu: 4, memory: 12 * 1024 * 1024 * 1024},
		"race-repeat-2":            {cpu: 4, memory: 12 * 1024 * 1024 * 1024},
		"race-repeat-3":            {cpu: 4, memory: 12 * 1024 * 1024 * 1024},
		"race-repeat-4":            {cpu: 4, memory: 12 * 1024 * 1024 * 1024},
		"race-repeat-5":            {cpu: 4, memory: 12 * 1024 * 1024 * 1024},
		"race-repeat-6":            {cpu: 4, memory: 12 * 1024 * 1024 * 1024},
		"race-repeat-supporting":   {cpu: 4, memory: 8 * 1024 * 1024 * 1024},
		"repository-audit":         {cpu: 4, memory: 6 * 1024 * 1024 * 1024, exclusive: []string{"repository-analysis"}},
		"scheduled-oracle":         {cpu: 4, memory: 8 * 1024 * 1024 * 1024},
		"targeted-mutation":        {cpu: 4, memory: 8 * 1024 * 1024 * 1024},
	}
	selected, exists := policies[subgate.ID]
	if !exists {
		return fmt.Errorf("missing soundness execution policy for %s", subgate.ID)
	}
	subgate.Dependencies = selected.dependsOn
	if subgate.Dependencies == nil {
		subgate.Dependencies = []string{}
	}
	subgate.CPUUnits = selected.cpu
	subgate.EstimatedPeakMemoryBytes = selected.memory
	subgate.ExclusivityGroups = selected.exclusive
	if subgate.ExclusivityGroups == nil {
		subgate.ExclusivityGroups = []string{}
	}
	subgate.Distributable = true
	switch subgate.ID {
	case "clean-tree-freshness":
		subgate.ProfileIDs = []soundnessgate.ProfileID{soundnessgate.ProfileComplete}
	case "performance-smoke":
		subgate.ProfileIDs = []soundnessgate.ProfileID{soundnessgate.ProfileConsumer, soundnessgate.ProfileHarness}
	case "baseline", "exceptions", "full-scan", "repository-audit":
		subgate.ProfileIDs = []soundnessgate.ProfileID{
			soundnessgate.ProfileComplete,
			soundnessgate.ProfileConsumer,
			soundnessgate.ProfileHarness,
			soundnessgate.ProfileSemantic,
		}
	case "harness-parity", "module-tests":
		subgate.ProfileIDs = []soundnessgate.ProfileID{
			soundnessgate.ProfileComplete,
			soundnessgate.ProfileHarness,
			soundnessgate.ProfileSemantic,
		}
	default:
		subgate.ProfileIDs = []soundnessgate.ProfileID{soundnessgate.ProfileComplete, soundnessgate.ProfileSemantic}
	}
	return nil
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

// raceRepeatGroupCount partitions the analyzer race/repeat plan into
// deterministic distributable groups so each group fits one reviewed
// four-CPU worker within its subgate timeout.
const raceRepeatGroupCount = 6

func raceRepeatGroupSubgate(index int) soundnessgate.Subgate {
	return newSubgate(
		fmt.Sprintf("race-repeat-%d", index),
		"tools/goplint",
		[]string{
			"./scripts/check-race-repeat.sh",
			"--phase", "analyzer",
			"--group", fmt.Sprintf("%d/%d", index, raceRepeatGroupCount),
		},
		3600,
		"report.json",
		nil,
		population("race-runs", 1),
		population("repeat-runs", 3),
	)
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
