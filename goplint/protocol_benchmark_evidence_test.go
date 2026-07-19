// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"path/filepath"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/protocoloracle"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func TestEmitProtocolBenchmarkEvidence(t *testing.T) {
	t.Parallel()

	manifest, err := protocoloracle.LoadBoundsManifest(filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		t.Fatalf("LoadBoundsManifest() error: %v", err)
	}
	observations := []soundnessgate.ObservedMember{
		{PopulationID: "analyzer-benchmarks", MemberID: "BenchmarkProtocolGeneratedAnalyzer"},
	}
	if err := protocoloracle.Enumerate(manifest, func(program protocoloracle.Program) error {
		observations = append(observations, soundnessgate.ObservedMember{
			PopulationID: "generated-programs",
			MemberID:     program.CaseID,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	programCount := len(observations) - 1
	if programCount != manifest.Blocking.ExpectedProgramCount || programCount == 0 {
		t.Fatalf("benchmark corpus count = %d, want %d", programCount, manifest.Blocking.ExpectedProgramCount)
	}
	emitSoundnessSubgateReport(t, observedPopulations(t, observations))
}

func TestGeneratedAnalyzerBenchmarkRejectsReferenceOnlyEvidence(t *testing.T) {
	t.Parallel()

	if err := validateGeneratedAnalyzerBenchmarkEvidence(1, generatedAnalyzerPipelineTrace{}); err == nil {
		t.Fatal("reference-only benchmark evidence satisfied the generated-analyzer contract")
	}
	pipeline := generatedAnalyzerPipelineTrace{}
	pipeline.add(requiredGeneratedAnalyzerPipeline()...)
	if err := validateGeneratedAnalyzerBenchmarkEvidence(1, pipeline); err != nil {
		t.Fatalf("complete generated-analyzer evidence rejected: %v", err)
	}
}
