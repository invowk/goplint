// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

func TestNewPlanBindsBuildOnceBinariesAndExactModeIterationCoverage(t *testing.T) {
	t.Parallel()

	manifest := testTimingManifest()
	census := []CensusEntry{{ID: "TestA", Kind: KindTest}, {ID: "TestB", Kind: KindTest}}
	binaries := testBinaryBindings()
	plan, err := NewPlan(
		soundnessevidence.DigestBytes([]byte("workspace")), "./goplint",
		ArtifactBinding{Name: "spec/goplint-test-timings.v1.json", Digest: soundnessevidence.DigestBytes([]byte("timing"))},
		manifest, census, binaries, 2, 3,
	)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if len(plan.WorkUnits) != 8 {
		t.Fatalf("work-unit count = %d, want 8", len(plan.WorkUnits))
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	broken := plan
	broken.WorkUnits = slices.Clone(plan.WorkUnits)
	broken.WorkUnits[0].MemberIDs = []string{"TestA", "TestB"}
	if _, err := normalizePlan(broken); err == nil {
		t.Fatal("normalizePlan() accepted overlapping coverage")
	}
}

func TestBuildBinariesBuildsEachModeExactlyOnceAndBindsContent(t *testing.T) {
	t.Parallel()

	outputDirectory := t.TempDir()
	var arguments [][]string
	run := func(_ context.Context, _ string, executable string, args ...string) error {
		arguments = append(arguments, append([]string{executable}, args...))
		for index, argument := range args {
			if argument == "-o" {
				return os.WriteFile(args[index+1], []byte(args[index+1]), 0o700)
			}
		}
		return nil
	}
	bindings, err := buildBinaries(t.Context(), ".", "./goplint", outputDirectory, run)
	if err != nil {
		t.Fatalf("buildBinaries() error = %v", err)
	}
	if len(arguments) != 2 || len(bindings) != 2 {
		t.Fatalf("build calls = %d, bindings = %d", len(arguments), len(bindings))
	}
	if slices.Contains(arguments[0], "-race") || !slices.Contains(arguments[1], "-race") {
		t.Fatalf("build arguments = %v", arguments)
	}
	for _, binding := range bindings {
		data, readErr := os.ReadFile(filepath.Join(outputDirectory, binding.FileName))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if binding.Digest != soundnessevidence.DigestBytes(data) {
			t.Fatalf("binary %s digest does not bind its bytes", binding.Mode)
		}
	}
}

func testBinaryBindings() []BinaryBinding {
	return []BinaryBinding{
		{Mode: "normal", FileName: "goplint-normal.test", Digest: soundnessevidence.DigestBytes([]byte("normal"))},
		{Mode: "race", FileName: "goplint-race.test", Digest: soundnessevidence.DigestBytes([]byte("race"))},
	}
}
