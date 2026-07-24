// SPDX-License-Identifier: MPL-2.0

package benchmarkpolicy

import (
	"maps"
	"slices"
	"testing"
)

func TestManifestValidateRejectsRelabelingAndWeakenedCertification(t *testing.T) {
	t.Parallel()

	valid := validCertificationPolicy()
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{name: "relabeled smoke", mutate: func(policy *Manifest) { policy.Policy = PolicyConsumerSmoke }},
		{name: "missing samples", mutate: func(policy *Manifest) { policy.Samples = 1 }},
		{name: "toolchain mismatch", mutate: func(policy *Manifest) { policy.GoToolchain = "go9.99" }},
		{name: "runner mismatch", mutate: func(policy *Manifest) { policy.RunnerClass = "other-runner" }},
		{name: "omitted phase", mutate: func(policy *Manifest) { policy.RequiredAnalyzerPhases = policy.RequiredAnalyzerPhases[:6] }},
		{name: "weakened population", mutate: func(policy *Manifest) { policy.RequiredPopulations = []string{"analyzer-benchmarks"} }},
		{name: "omitted workload", mutate: func(policy *Manifest) { delete(policy.Benchmarks, "BenchmarkProtocolGeneratedAnalyzer") }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			manifest := valid
			manifest.RequiredAnalyzerPhases = slices.Clone(valid.RequiredAnalyzerPhases)
			manifest.RequiredPopulations = slices.Clone(valid.RequiredPopulations)
			manifest.Benchmarks = make(map[string]BenchmarkThreshold, len(valid.Benchmarks))
			maps.Copy(manifest.Benchmarks, valid.Benchmarks)
			testCase.mutate(&manifest)
			if err := manifest.Validate(PolicyCertification, "go1.26.5", "github-4cpu"); err == nil {
				t.Fatal("Validate() accepted weakened or mismatched certification")
			}
		})
	}
}

func validCertificationPolicy() Manifest {
	benchmarks := make(map[string]BenchmarkThreshold, len(certificationBenchmarks))
	for _, id := range certificationBenchmarks {
		benchmarks[id] = BenchmarkThreshold{MaxNanoseconds: 1, MaxBytes: 1, MaxAllocations: 1}
	}
	recursive := benchmarks["BenchmarkProtocolRecursiveTabulation"]
	recursive.MaxPathEdges = 1
	recursive.MinSummaryReuse = 1
	benchmarks["BenchmarkProtocolRecursiveTabulation"] = recursive
	return Manifest{
		FormatVersion: 2, Policy: PolicyCertification, Samples: 5,
		GoToolchain: "go1.26", RunnerClass: "github-4cpu",
		RequiredAnalyzerPhases: slices.Clone(requiredPhases),
		RequiredPopulations:    []string{"analyzer-benchmarks", "generated-programs"},
		Benchmarks:             benchmarks,
		RepositoryFullScan:     ScanThreshold{MaxWallMilliseconds: 1, MaxPeakBytes: 1},
	}
}
