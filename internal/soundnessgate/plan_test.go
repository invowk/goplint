// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"bytes"
	"context"
	"testing"
)

func TestGeneratePlanIsDeterministic(t *testing.T) {
	t.Parallel()

	root, manifestPath, _ := writeRunnerFixture(t, validGateManifest(), validGateRegistry())
	dependencies := deterministicPlanDependencies()
	options := PlanOptions{
		Root:         root,
		ManifestPath: manifestPath,
		Profile:      ProfileCore,
		Resources: ResourceBudget{
			CPUUnits:        4,
			MemoryBytes:     16 * 1024 * 1024 * 1024,
			MaximumWorkers:  2,
			RunnerClass:     "test-runner",
			SerialReference: false,
		},
	}
	left, err := generatePlan(t.Context(), options, dependencies)
	if err != nil {
		t.Fatalf("first generatePlan() error = %v", err)
	}
	right, err := generatePlan(t.Context(), options, dependencies)
	if err != nil {
		t.Fatalf("second generatePlan() error = %v", err)
	}
	leftJSON, err := CanonicalPlanJSON(left)
	if err != nil {
		t.Fatalf("left CanonicalPlanJSON() error = %v", err)
	}
	rightJSON, err := CanonicalPlanJSON(right)
	if err != nil {
		t.Fatalf("right CanonicalPlanJSON() error = %v", err)
	}
	if !bytes.Equal(leftJSON, rightJSON) {
		t.Fatalf("generated plans differ:\nleft:  %s\nright: %s", leftJSON, rightJSON)
	}
	wantCommands := len(validGateManifest().Profiles[3].SubgateIDs)
	if len(left.Commands) != wantCommands {
		t.Fatalf("generated command count = %d, want %d", len(left.Commands), wantCommands)
	}
}

func TestGeneratePlanRejectsImpossibleResources(t *testing.T) {
	t.Parallel()

	root, manifestPath, _ := writeRunnerFixture(t, validGateManifest(), validGateRegistry())
	_, err := generatePlan(t.Context(), PlanOptions{
		Root:         root,
		ManifestPath: manifestPath,
		Profile:      ProfileCore,
		Resources: ResourceBudget{
			CPUUnits:        1,
			MemoryBytes:     512,
			MaximumWorkers:  1,
			RunnerClass:     "test-runner",
			SerialReference: true,
		},
	}, deterministicPlanDependencies())
	assertGateErrorContains(t, err, "requires resources that exceed the plan budget")
}

func TestPlannedResourceReservationPreservesCertificationRunnerClass(t *testing.T) {
	t.Parallel()

	certification := Subgate{
		ID: performanceCertificationSubgateID, CPUUnits: 4, EstimatedPeakMemoryBytes: 6 * 1024 * 1024 * 1024,
	}
	local := plannedResourceReservation(certification, ResourceBudget{CPUUnits: 24})
	if local.CPUUnits != 24 || local.MemoryBytes != certification.EstimatedPeakMemoryBytes {
		t.Fatalf("local certification reservation = %+v", local)
	}
	hosted := plannedResourceReservation(certification, ResourceBudget{CPUUnits: 4})
	if hosted.CPUUnits != 4 {
		t.Fatalf("hosted certification CPU reservation = %d, want 4", hosted.CPUUnits)
	}
	raceRepeatGroup := certification
	raceRepeatGroup.ID = "race-repeat-1"
	if reservation := plannedResourceReservation(raceRepeatGroup, ResourceBudget{CPUUnits: 24}); reservation.CPUUnits != 4 {
		t.Fatalf("local race/repeat group CPU reservation = %d, want its reviewed 4", reservation.CPUUnits)
	}
	if reservation := plannedResourceReservation(raceRepeatGroup, ResourceBudget{CPUUnits: 4}); reservation.CPUUnits != 4 {
		t.Fatalf("hosted race/repeat group CPU reservation = %d, want 4", reservation.CPUUnits)
	}
	correctness := certification
	correctness.ID = "protocol-oracle"
	if reservation := plannedResourceReservation(correctness, ResourceBudget{CPUUnits: 24}); reservation.CPUUnits != 4 {
		t.Fatalf("correctness CPU reservation = %d, want 4", reservation.CPUUnits)
	}
}

func TestCanonicalPlanJSONNormalizesSetOrdering(t *testing.T) {
	t.Parallel()

	canonical := validExecutionPlan()
	shuffled := validExecutionPlan()
	shuffled.Commands[0], shuffled.Commands[1] = shuffled.Commands[1], shuffled.Commands[0]
	shuffled.Dependencies[0], shuffled.Dependencies[1] = shuffled.Dependencies[1], shuffled.Dependencies[0]
	shuffled.Shards[0], shuffled.Shards[1] = shuffled.Shards[1], shuffled.Shards[0]
	shuffled.ExpectedReports[0], shuffled.ExpectedReports[1] = shuffled.ExpectedReports[1], shuffled.ExpectedReports[0]
	canonicalJSON, err := CanonicalPlanJSON(canonical)
	if err != nil {
		t.Fatalf("canonical CanonicalPlanJSON() error = %v", err)
	}
	shuffledJSON, err := CanonicalPlanJSON(shuffled)
	if err != nil {
		t.Fatalf("shuffled CanonicalPlanJSON() error = %v", err)
	}
	if !bytes.Equal(canonicalJSON, shuffledJSON) {
		t.Fatalf("canonical plan normalization differs:\ncanonical: %s\nshuffled:  %s", canonicalJSON, shuffledJSON)
	}
}

func deterministicPlanDependencies() planDependencies {
	toolchain := ToolchainBinding{GoVersion: "go1.26.5", GOOS: "linux", GOARCH: "amd64"}
	digest, err := toolchainDigest(toolchain)
	if err != nil {
		panic(err)
	}
	toolchain.Digest = digest
	return planDependencies{
		workspaceDigest: func(context.Context, string) (string, error) {
			return runnerTestWorkspaceDigest, nil
		},
		binaryDigest: func(context.Context, string, string, string) (string, error) {
			return "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", nil
		},
		toolchain: func() (ToolchainBinding, error) {
			return toolchain, nil
		},
	}
}
