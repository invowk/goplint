// SPDX-License-Identifier: MPL-2.0

// Package benchmarkpolicy validates distinct smoke and certification policies.
package benchmarkpolicy

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	PolicyConsumerSmoke = "consumer-smoke"
	PolicyCertification = "certification"
)

var (
	certificationBenchmarks = []string{
		"BenchmarkConstructorSuccessfulReturnClassification",
		"BenchmarkProtocolAliasJoin",
		"BenchmarkProtocolCanonicalSolver",
		"BenchmarkProtocolGeneratedAnalyzer",
		"BenchmarkProtocolPackageProcedureInventory",
		"BenchmarkProtocolRecursiveTabulation",
		"BenchmarkProtocolReferenceInterpreter",
		"BenchmarkProtocolRefinementEvidence",
	}
	smokeBenchmarks = []string{"BenchmarkProtocolCanonicalSolver", "BenchmarkProtocolRecursiveTabulation"}
	requiredPhases  = []string{
		"source-extraction", "identity", "graph-construction", "propagation",
		"refinement", "aggregation", "reporting",
	}
)

type (
	// Manifest is one reviewed benchmark policy.
	Manifest struct {
		FormatVersion          int                           `toml:"format_version"`
		Policy                 string                        `toml:"policy"`
		Samples                int                           `toml:"samples"`
		GoToolchain            string                        `toml:"go_toolchain"`
		RunnerClass            string                        `toml:"runner_class"`
		RequiredAnalyzerPhases []string                      `toml:"required_analyzer_phases"`
		RequiredPopulations    []string                      `toml:"required_populations"`
		Benchmarks             map[string]BenchmarkThreshold `toml:"benchmarks"`
		RepositoryFullScan     ScanThreshold                 `toml:"repository_full_scan"`
	}

	// BenchmarkThreshold bounds one algorithmic workload.
	BenchmarkThreshold struct {
		MaxNanoseconds  float64 `toml:"max_ns_per_op"`
		MaxBytes        float64 `toml:"max_bytes_per_op"`
		MaxAllocations  float64 `toml:"max_allocs_per_op"`
		MaxPathEdges    float64 `toml:"max_path_edges_per_op"`
		MinSummaryReuse float64 `toml:"min_summary_reuses_per_op"`
	}

	// ScanThreshold bounds one canonical repository scan.
	ScanThreshold struct {
		MaxWallMilliseconds int64 `toml:"max_wall_ms"`
		MaxPeakBytes        int64 `toml:"max_peak_bytes"`
	}
)

// Load strictly decodes and validates a benchmark policy.
func Load(path, expectedPolicy, actualToolchain, actualRunnerClass string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("reading benchmark policy: %w", err)
	}
	var manifest Manifest
	_, err = toml.Decode(string(data), &manifest)
	if err != nil {
		return Manifest{}, fmt.Errorf("decoding benchmark policy: %w", err)
	}
	if err := manifest.Validate(expectedPolicy, actualToolchain, actualRunnerClass); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate rejects relabeled, weakened, incomplete, or runner-mismatched policies.
func (manifest Manifest) Validate(expectedPolicy, actualToolchain, actualRunnerClass string) error {
	if manifest.Policy != expectedPolicy {
		return fmt.Errorf("benchmark policy = %q, want %q", manifest.Policy, expectedPolicy)
	}
	wantVersion, wantSamples := 1, 1
	wantBenchmarks := smokeBenchmarks
	wantPopulations := []string{"consumer-performance-smoke"}
	if manifest.Policy == PolicyCertification {
		wantVersion, wantSamples = 2, 5
		wantBenchmarks = certificationBenchmarks
		wantPopulations = []string{"analyzer-benchmarks", "generated-programs"}
		if !slices.Equal(manifest.RequiredAnalyzerPhases, requiredPhases) {
			return errors.New("certification policy omits or reorders required analyzer phases")
		}
	} else if manifest.Policy != PolicyConsumerSmoke {
		return fmt.Errorf("unknown benchmark policy %q", manifest.Policy)
	}
	if manifest.FormatVersion != wantVersion || manifest.Samples != wantSamples {
		return fmt.Errorf("%s policy requires format_version=%d and samples=%d", manifest.Policy, wantVersion, wantSamples)
	}
	if manifest.GoToolchain == "" || !strings.HasPrefix(actualToolchain, manifest.GoToolchain) {
		return fmt.Errorf("benchmark toolchain mismatch: got %q, policy requires %q", actualToolchain, manifest.GoToolchain)
	}
	if manifest.RunnerClass == "" || actualRunnerClass != "" && manifest.RunnerClass != actualRunnerClass {
		return fmt.Errorf("benchmark runner class mismatch: got %q, policy requires %q", actualRunnerClass, manifest.RunnerClass)
	}
	if !slices.Equal(manifest.RequiredPopulations, wantPopulations) {
		return errors.New("benchmark policy weakens or reorders required semantic populations")
	}
	keys := make([]string, 0, len(manifest.Benchmarks))
	for key := range manifest.Benchmarks {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, wantBenchmarks) {
		return fmt.Errorf("benchmark policy workload set = %v, want %v", keys, wantBenchmarks)
	}
	for id, threshold := range manifest.Benchmarks {
		if threshold.MaxNanoseconds <= 0 || threshold.MaxBytes <= 0 || threshold.MaxAllocations <= 0 {
			return fmt.Errorf("benchmark %q has incomplete time, byte, or allocation limits", id)
		}
		if id == "BenchmarkProtocolRecursiveTabulation" &&
			(threshold.MaxPathEdges <= 0 || threshold.MinSummaryReuse <= 0) {
			return errors.New("recursive tabulation policy omits state-count or reuse limits")
		}
	}
	if manifest.RepositoryFullScan.MaxWallMilliseconds <= 0 || manifest.RepositoryFullScan.MaxPeakBytes <= 0 {
		return errors.New("benchmark policy omits repository full-scan limits")
	}
	return nil
}
