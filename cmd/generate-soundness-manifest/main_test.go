// SPDX-License-Identifier: MPL-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func TestMutationKernelSubgateMatchesLiveCoverage(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	registry, err := soundnessevidence.LoadRegistry(
		t.Context(),
		filepath.Join(root, "tools", "goplint", "spec", "semantic-evidence.v2.json"),
	)
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	manifest, err := buildManifest(registry)
	if err != nil {
		t.Fatalf("buildManifest() error: %v", err)
	}
	subgate := manifestSubgate(t, manifest, "mutation-kernel-coverage")
	wantCommand := []string{
		"go",
		"run",
		"./cmd/mutation-kernel-coverage",
		"-root",
		".",
		"-manifest",
		"testdata/subgates/mutation-kernel-coverage.v1.json",
	}
	if !reflect.DeepEqual(subgate.Command, wantCommand) {
		t.Errorf("mutation kernel command = %v, want %v", subgate.Command, wantCommand)
	}
	wantPopulations := map[string]int{
		"mutation-kernel-bindings":   110,
		"mutation-kernel-categories": 8,
		"mutation-kernel-mutants":    27,
	}
	for _, requirement := range subgate.RequiredPopulations {
		if wantPopulations[requirement.ID] != requirement.Minimum {
			t.Errorf("population %q minimum = %d, want %d", requirement.ID, requirement.Minimum,
				wantPopulations[requirement.ID])
		}
		delete(wantPopulations, requirement.ID)
	}
	if len(wantPopulations) != 0 {
		t.Errorf("missing mutation kernel populations: %v", wantPopulations)
	}
}

func TestMakeScanReportProducersMatchManifest(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	registry, err := soundnessevidence.LoadRegistry(
		t.Context(),
		filepath.Join(root, "tools", "goplint", "spec", "semantic-evidence.v2.json"),
	)
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	manifest, err := buildManifest(registry)
	if err != nil {
		t.Fatalf("buildManifest() error: %v", err)
	}
	for _, subgateID := range []string{"baseline", "full-scan"} {
		t.Run(subgateID, func(t *testing.T) {
			t.Parallel()

			subgate := manifestSubgate(t, manifest, subgateID)
			validateMakeReportProducer(t, root, subgate)
		})
	}
}

func validateMakeReportProducer(t *testing.T, root string, subgate soundnessgate.Subgate) {
	t.Helper()

	commandDigest, err := soundnessgate.CommandDigest(subgate)
	if err != nil {
		t.Fatalf("CommandDigest() error: %v", err)
	}
	binding := soundnessevidence.ObservationBinding{
		RunID:           "make-producer-contract",
		WorkspaceDigest: soundnessevidence.DigestBytes([]byte("workspace")),
		ManifestDigest:  soundnessevidence.DigestBytes([]byte("manifest")),
		CommandDigest:   commandDigest,
		SubgateID:       subgate.ID,
	}
	reportPath := filepath.Join(t.TempDir(), "report.json")
	reporterCommand := makeReporterCommand(t, root, subgate.Command[1])
	command := exec.CommandContext(t.Context(), reporterCommand[0], reporterCommand[1:]...)
	command.Dir = filepath.Join(root, "tools", "goplint")
	command.Env = append(os.Environ(),
		soundnessevidence.EnvRunID+"="+binding.RunID,
		soundnessevidence.EnvWorkspaceDigest+"="+binding.WorkspaceDigest,
		soundnessevidence.EnvManifestDigest+"="+binding.ManifestDigest,
		soundnessevidence.EnvCommandDigest+"="+binding.CommandDigest,
		soundnessevidence.EnvSubgateID+"="+binding.SubgateID,
		soundnessgate.EnvSubgateReportPath+"="+reportPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("execute %q: %v\n%s", subgate.ID, err, output)
	}
	report, err := soundnessgate.LoadReport(t.Context(), reportPath)
	if err != nil {
		t.Fatalf("LoadReport() error: %v", err)
	}
	if err := subgate.ValidateReport(report, binding); err != nil {
		t.Fatalf("ValidateReport() error: %v", err)
	}
}

func makeReporterCommand(t *testing.T, root, target string) []string {
	t.Helper()

	command := exec.CommandContext(t.Context(), "make", "-n", target)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatalf("make -n %s: %v", target, err)
	}
	const marker = "go run ./cmd/subgate-report"
	for line := range strings.SplitSeq(string(output), "\n") {
		markerIndex := strings.Index(line, marker)
		if markerIndex < 0 {
			continue
		}
		fields := strings.Fields(line[markerIndex:])
		if len(fields) < 4 {
			t.Fatalf("make target %s has malformed report producer %q", target, line)
		}
		return fields
	}
	t.Fatalf("make target %s has no executable subgate report producer", target)
	return nil
}

func manifestSubgate(t *testing.T, manifest soundnessgate.Manifest, id string) soundnessgate.Subgate {
	t.Helper()

	for _, subgate := range manifest.Subgates {
		if subgate.ID == id {
			return subgate
		}
	}
	t.Fatalf("manifest has no subgate %q", id)
	return soundnessgate.Subgate{}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error: %v", err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", "..", "..", ".."))
}
