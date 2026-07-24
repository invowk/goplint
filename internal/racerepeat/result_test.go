// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

func TestParseWorkResultRequiresExactlyOneRunAndPassPerTopLevelMember(t *testing.T) {
	t.Parallel()

	plan := testRaceRepeatPlan(t)
	unit := plan.WorkUnits[0]
	output := test2JSONOutput(unit.MemberIDs, "pass")
	result, err := ParseWorkResult(plan, unit, output, statusPassed)
	if err != nil {
		t.Fatalf("ParseWorkResult() error = %v", err)
	}
	if err := result.Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tests := []struct {
		name   string
		output []byte
		status string
	}{
		{name: "missing", output: nil, status: statusPassed},
		{name: "crash", output: test2JSONOutput(unit.MemberIDs, "run"), status: statusFailed},
		{name: "timeout", output: output, status: statusTimedOut},
		{name: "skip", output: test2JSONOutput(unit.MemberIDs, "skip"), status: statusPassed},
		{name: "unexpected", output: append(output, []byte(`{"Action":"run","Test":"TestUnexpected"}`+"\n")...), status: statusPassed},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, parseErr := ParseWorkResult(plan, unit, testCase.output, testCase.status); parseErr == nil {
				t.Fatal("ParseWorkResult() accepted an invalid terminal result")
			}
		})
	}
}

func TestExecutePlanProvesExactRaceAndRepeatPopulationCounts(t *testing.T) {
	t.Parallel()

	plan := testRaceRepeatPlan(t)
	binaryDirectory := t.TempDir()
	writeTestBinaries(t, binaryDirectory)
	options := ExecuteOptions{
		ModuleRoot: ".", BinaryDirectory: binaryDirectory,
		OutputDirectory: t.TempDir(), MaximumWorkers: 3, CPUPerWorker: 2,
	}
	run := func(_ context.Context, _, _ string, unit WorkUnit, _ int, _ []string) ([]byte, error) {
		return test2JSONOutput(unit.MemberIDs, "pass"), nil
	}
	results, err := executePlan(t.Context(), plan, plan.WorkUnits, options, run)
	if err != nil {
		t.Fatalf("executePlan() error = %v", err)
	}
	counts := make(map[string]int)
	for _, result := range results {
		unit := workUnitByID(plan, result.WorkUnitID)
		for _, memberID := range result.ExpectedIDs {
			counts[unit.Mode+":"+memberID]++
		}
	}
	for _, memberID := range []string{"TestA", "TestB"} {
		if counts["race:"+memberID] != 1 || counts["normal:"+memberID] != 3 {
			t.Fatalf("execution counts for %s = race %d, normal %d", memberID, counts["race:"+memberID], counts["normal:"+memberID])
		}
	}
}

func TestExecutePlanRunsPrebuiltBinaryFromPackageDirectory(t *testing.T) {
	t.Parallel()

	plan := testRaceRepeatPlan(t)
	binaryDirectory := t.TempDir()
	writeTestBinaries(t, binaryDirectory)
	moduleRoot := t.TempDir()
	wantDirectory := filepath.Join(moduleRoot, "goplint")
	if err := os.Mkdir(wantDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(_ context.Context, directory, _ string, unit WorkUnit, _ int, _ []string) ([]byte, error) {
		if directory != wantDirectory {
			return nil, fmt.Errorf("working directory = %q, want %q", directory, wantDirectory)
		}
		return test2JSONOutput(unit.MemberIDs, "pass"), nil
	}
	_, err := executePlan(t.Context(), plan, plan.WorkUnits, ExecuteOptions{
		ModuleRoot: moduleRoot, BinaryDirectory: binaryDirectory,
		OutputDirectory: t.TempDir(), MaximumWorkers: 2, CPUPerWorker: 1,
	}, run)
	if err != nil {
		t.Fatalf("executePlan() error = %v", err)
	}
}

func TestExecutePlanCancelsRemainingWorkAfterCrash(t *testing.T) {
	t.Parallel()

	plan := testRaceRepeatPlan(t)
	binaryDirectory := t.TempDir()
	writeTestBinaries(t, binaryDirectory)
	options := ExecuteOptions{
		ModuleRoot: ".", BinaryDirectory: binaryDirectory,
		OutputDirectory: t.TempDir(), MaximumWorkers: 2, CPUPerWorker: 1,
	}
	run := func(ctx context.Context, _, _ string, unit WorkUnit, _ int, _ []string) ([]byte, error) {
		if unit.ID == plan.WorkUnits[0].ID {
			return nil, errors.New("injected crash")
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if _, err := executePlan(t.Context(), plan, plan.WorkUnits, options, run); err == nil {
		t.Fatal("executePlan() accepted a crashed work unit")
	}
}

func TestExecutePlanRejectsChangedBinaryAfterPlanning(t *testing.T) {
	t.Parallel()

	plan := testRaceRepeatPlan(t)
	binaryDirectory := t.TempDir()
	writeTestBinaries(t, binaryDirectory)
	if err := os.WriteFile(filepath.Join(binaryDirectory, "goplint-race.test"), []byte("changed"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := executePlan(t.Context(), plan, plan.WorkUnits, ExecuteOptions{
		ModuleRoot: ".", BinaryDirectory: binaryDirectory,
		OutputDirectory: t.TempDir(), MaximumWorkers: 1, CPUPerWorker: 1,
	}, func(context.Context, string, string, WorkUnit, int, []string) ([]byte, error) { return nil, nil })
	if err == nil {
		t.Fatal("executePlan() accepted a changed binary")
	}
}

func testRaceRepeatPlan(t *testing.T) Plan {
	t.Helper()
	plan, err := NewPlan(
		soundnessevidence.DigestBytes([]byte("workspace")), "./goplint",
		ArtifactBinding{Name: "spec/goplint-test-timings.v1.json", Digest: soundnessevidence.DigestBytes([]byte("timing"))},
		testTimingManifest(), []CensusEntry{{ID: "TestA", Kind: KindTest}, {ID: "TestB", Kind: KindTest}},
		testBinaryBindings(), 2, 3,
	)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	return plan
}

func test2JSONOutput(memberIDs []string, terminalAction string) []byte {
	result := []byte{}
	for _, memberID := range memberIDs {
		result = fmt.Appendf(result, "{\"Action\":\"run\",\"Test\":%q}\n", memberID)
		if terminalAction != "run" {
			result = fmt.Appendf(result, "{\"Action\":%q,\"Test\":%q}\n", terminalAction, memberID)
		}
	}
	return result
}

func writeTestBinaries(t *testing.T, directory string) {
	t.Helper()
	for _, binding := range testBinaryBindings() {
		if err := os.WriteFile(filepath.Join(directory, binding.FileName), []byte(binding.Mode), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func workUnitByID(plan Plan, id string) WorkUnit {
	for _, unit := range plan.WorkUnits {
		if unit.ID == id {
			return unit
		}
	}
	return WorkUnit{}
}

func TestExecuteWorkUnitHonorsParentCancellation(t *testing.T) {
	t.Parallel()

	plan := testRaceRepeatPlan(t)
	unit := plan.WorkUnits[0]
	binaryDirectory := t.TempDir()
	writeTestBinaries(t, binaryDirectory)
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	_, err := executeWorkUnit(ctx, plan, ExecuteOptions{
		ModuleRoot: ".", BinaryDirectory: binaryDirectory, OutputDirectory: t.TempDir(), CPUPerWorker: 1,
	}, unit, func(ctx context.Context, _, _ string, _ WorkUnit, _ int, _ []string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err == nil {
		t.Fatal("executeWorkUnit() accepted canceled execution")
	}
}
