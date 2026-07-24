// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"bytes"
	"testing"
)

func TestRunPlanSerialMatchesLegacyNormalizedReportAndEvidence(t *testing.T) {
	t.Parallel()

	manifest := validGateManifest()
	manifest.Subgates[0].Dependencies = []string{manifest.Subgates[1].ID}
	root, manifestPath, registry := writeRunnerFixture(t, manifest, validGateRegistry())
	planDeps := deterministicPlanDependencies()
	plan, err := generatePlan(t.Context(), PlanOptions{
		Root:         root,
		ManifestPath: manifestPath,
		Profile:      ProfileCore,
		Resources: ResourceBudget{
			CPUUnits:        4,
			MemoryBytes:     16 * 1024 * 1024 * 1024,
			MaximumWorkers:  1,
			RunnerClass:     "test-runner",
			SerialReference: true,
		},
	}, planDeps)
	if err != nil {
		t.Fatalf("generatePlan() error = %v", err)
	}
	legacy, err := run(t.Context(), Options{
		Root: root, ManifestPath: manifestPath, Profile: ProfileCore,
	}, runnerTestDependencies(t, registry, producerBehavior{}))
	if err != nil {
		t.Fatalf("legacy run() error = %v", err)
	}
	planned, err := runPlanSerial(
		t.Context(),
		plan,
		PlanSerialOptions{Root: root},
		runnerTestDependencies(t, registry, producerBehavior{}),
		planDeps,
	)
	if err != nil {
		t.Fatalf("runPlanSerial() error = %v", err)
	}
	legacyJSON, err := NormalizedRunReportJSON(legacy.Report)
	if err != nil {
		t.Fatalf("legacy NormalizedRunReportJSON() error = %v", err)
	}
	plannedJSON, err := NormalizedRunReportJSON(planned.Report)
	if err != nil {
		t.Fatalf("planned NormalizedRunReportJSON() error = %v", err)
	}
	if !bytes.Equal(legacyJSON, plannedJSON) {
		t.Fatalf("serial plan report/evidence differs from legacy:\nlegacy:  %s\nplanned: %s", legacyJSON, plannedJSON)
	}
	for _, telemetry := range planned.Telemetry.Subgates {
		if telemetry.ReservedResources.MemoryBytes == 0 {
			t.Fatalf("planned subgate %q did not retain its resource reservation", telemetry.ID)
		}
	}
}

func TestRunPlanSerialRejectsPlanThatNoLongerMatchesCurrentInputs(t *testing.T) {
	t.Parallel()

	root, manifestPath, registry := writeRunnerFixture(t, validGateManifest(), validGateRegistry())
	planDeps := deterministicPlanDependencies()
	plan, err := generatePlan(t.Context(), PlanOptions{
		Root:         root,
		ManifestPath: manifestPath,
		Profile:      ProfileCore,
		Resources: ResourceBudget{
			CPUUnits:        4,
			MemoryBytes:     16 * 1024 * 1024 * 1024,
			MaximumWorkers:  1,
			RunnerClass:     "test-runner",
			SerialReference: true,
		},
	}, planDeps)
	if err != nil {
		t.Fatalf("generatePlan() error = %v", err)
	}
	plan.Workspace.Digest = runnerChangedDigest
	assignPlanID(&plan)
	_, err = runPlanSerial(
		t.Context(),
		plan,
		PlanSerialOptions{Root: root},
		runnerTestDependencies(t, registry, producerBehavior{}),
		planDeps,
	)
	assertGateErrorContains(t, err, "does not match the exact current workspace")
}

func TestSerialSubgatesFromPlanUsesDependencyOrder(t *testing.T) {
	t.Parallel()

	root, manifestPath, _ := writeRunnerFixture(t, validGateManifest(), validGateRegistry())
	plan, err := generatePlan(t.Context(), PlanOptions{
		Root:         root,
		ManifestPath: manifestPath,
		Profile:      ProfileCore,
		Resources: ResourceBudget{
			CPUUnits:        4,
			MemoryBytes:     16 * 1024 * 1024 * 1024,
			MaximumWorkers:  1,
			RunnerClass:     "test-runner",
			SerialReference: true,
		},
	}, deterministicPlanDependencies())
	if err != nil {
		t.Fatalf("generatePlan() error = %v", err)
	}
	if len(plan.Dependencies) < 2 {
		t.Fatalf("plan has %d dependencies, want at least 2", len(plan.Dependencies))
	}
	firstID := plan.Dependencies[0].WorkUnitID
	dependencyID := plan.Dependencies[1].WorkUnitID
	plan.Dependencies[0].Requires = []string{dependencyID}

	subgates, _, err := serialSubgatesFromPlan(plan)
	if err != nil {
		t.Fatalf("serialSubgatesFromPlan() error = %v", err)
	}
	if subgates[0].ID != dependencyID || subgates[1].ID != firstID {
		t.Fatalf("serial order = [%s, %s], want dependency order [%s, %s]", subgates[0].ID, subgates[1].ID, dependencyID, firstID)
	}
}
