// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const (
	runnerTestWorkspaceDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	runnerChangedDigest       = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

func TestRunWritesStrictRetainedReport(t *testing.T) {
	t.Parallel()

	root, manifestPath, registry := writeRunnerFixture(t, validGateManifest(), validGateRegistry())
	reportPath := filepath.Join(t.TempDir(), "aggregate-report.json")
	dependencies := runnerTestDependencies(t, registry, producerBehavior{})
	result, err := run(t.Context(), Options{
		Root:         root,
		ManifestPath: manifestPath,
		ReportPath:   reportPath,
	}, dependencies)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if result.ReportPath != reportPath {
		t.Fatalf("run() report path = %q, want %q", result.ReportPath, reportPath)
	}
	report, err := LoadRunReport(t.Context(), reportPath)
	if err != nil {
		t.Fatalf("LoadRunReport() error = %v", err)
	}
	manifest, _, err := LoadManifest(t.Context(), manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if err := ValidateRunReport(report, manifest, registry); err != nil {
		t.Fatalf("ValidateRunReport() error = %v", err)
	}
	if report.WorkspaceDigest != runnerTestWorkspaceDigest {
		t.Fatalf("run report workspace digest = %q, want %q", report.WorkspaceDigest, runnerTestWorkspaceDigest)
	}
}

func TestRunRejectsEverySuccessfulNoOpReplacement(t *testing.T) {
	t.Parallel()

	baseManifest := validGateManifest()
	for replacedIndex := range baseManifest.Subgates {
		t.Run(baseManifest.Subgates[replacedIndex].ID, func(t *testing.T) {
			t.Parallel()

			manifest := validGateManifest()
			manifest.Subgates[replacedIndex].Command = []string{"successful-no-op"}
			registry := validGateRegistry()
			root, manifestPath, registry := writeRunnerFixture(t, manifest, registry)
			dependencies := runnerTestDependencies(t, registry, producerBehavior{successfulNoOp: true})
			_, err := run(t.Context(), Options{Root: root, ManifestPath: manifestPath}, dependencies)
			assertGateErrorContains(t, err, "did not produce its required report")
		})
	}
}

func TestRunRejectsAdversarialOutputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		behavior  producerBehavior
		wantError string
	}{
		{
			name:      "success without observations",
			behavior:  producerBehavior{omitObservation: true},
			wantError: "has no observation",
		},
		{
			name:      "duplicate observation",
			behavior:  producerBehavior{duplicateObservation: true},
			wantError: "duplicate registration id",
		},
		{
			name:      "forged marker report",
			behavior:  producerBehavior{forgedReportMarker: true},
			wantError: "decode JSON",
		},
		{
			name:      "forged marker observation",
			behavior:  producerBehavior{forgedObservationMarker: true},
			wantError: "decode JSON",
		},
		{
			name:      "empty admitted corpus",
			behavior:  producerBehavior{zeroPopulation: true},
			wantError: "want at least",
		},
		{
			name:      "stale report",
			behavior:  producerBehavior{staleReport: true},
			wantError: "stale or mismatched binding",
		},
		{
			name:      "stale observation",
			behavior:  producerBehavior{staleObservation: true},
			wantError: "stale or mismatched binding",
		},
		{
			name:      "unrelated command failure is not proof",
			behavior:  producerBehavior{failAfterOutput: true},
			wantError: "command failed; no evidence accepted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			registry := validGateRegistry()
			root, manifestPath, registry := writeRunnerFixture(t, validGateManifest(), registry)
			dependencies := runnerTestDependencies(t, registry, test.behavior)
			_, err := run(t.Context(), Options{Root: root, ManifestPath: manifestPath}, dependencies)
			assertGateErrorContains(t, err, test.wantError)
		})
	}
}

func TestRunRejectsWorkspaceMutationDuringExecution(t *testing.T) {
	t.Parallel()

	registry := validGateRegistry()
	root, manifestPath, registry := writeRunnerFixture(t, validGateManifest(), registry)
	dependencies := runnerTestDependencies(t, registry, producerBehavior{})
	digestCalls := 0
	dependencies.workspaceDigest = func(context.Context, string) (string, error) {
		digestCalls++
		if digestCalls == 1 {
			return runnerTestWorkspaceDigest, nil
		}
		return runnerChangedDigest, nil
	}
	_, err := run(t.Context(), Options{Root: root, ManifestPath: manifestPath}, dependencies)
	assertGateErrorContains(t, err, "workspace changed during aggregate execution")
}

func TestRunRejectsReportInsideHashedWorkspace(t *testing.T) {
	t.Parallel()

	registry := validGateRegistry()
	root, manifestPath, registry := writeRunnerFixture(t, validGateManifest(), registry)
	dependencies := runnerTestDependencies(t, registry, producerBehavior{})
	_, err := run(t.Context(), Options{
		Root:         root,
		ManifestPath: manifestPath,
		ReportPath:   filepath.Join(root, "report.json"),
	}, dependencies)
	assertGateErrorContains(t, err, "must be outside the hashed workspace")
}

type producerBehavior struct {
	successfulNoOp          bool
	omitObservation         bool
	duplicateObservation    bool
	forgedReportMarker      bool
	forgedObservationMarker bool
	zeroPopulation          bool
	staleReport             bool
	staleObservation        bool
	failAfterOutput         bool
}

func runnerTestDependencies(
	t *testing.T,
	registry soundnessevidence.Registry,
	behavior producerBehavior,
) runnerDependencies {
	t.Helper()
	temporaryBase := t.TempDir()
	return runnerDependencies{
		workspaceDigest: func(context.Context, string) (string, error) {
			return runnerTestWorkspaceDigest, nil
		},
		execute: func(
			ctx context.Context,
			_ string,
			command []string,
			environment []string,
			_ io.Writer,
			_ io.Writer,
		) error {
			if behavior.successfulNoOp && command[0] == "successful-no-op" {
				return nil
			}
			return emitTestProducerOutputs(ctx, registry, environment, behavior)
		},
		makeTempDir: func(string, string) (string, error) {
			path := filepath.Join(temporaryBase, "evidence")
			if err := os.Mkdir(path, 0o755); err != nil {
				return "", fmt.Errorf("create test evidence root: %w", err)
			}
			return path, nil
		},
		newRunID: func() (string, error) {
			return "run-test", nil
		},
	}
}

func emitTestProducerOutputs(
	ctx context.Context,
	registry soundnessevidence.Registry,
	environment []string,
	behavior producerBehavior,
) error {
	lookup := environmentLookup(environment)
	binding, err := bindingFromLookup(lookup)
	if err != nil {
		return err
	}
	reportPath, exists := lookup(EnvSubgateReportPath)
	if !exists {
		return errors.New("test producer has no report path")
	}
	if behavior.forgedReportMarker && binding.SubgateID == "semantic-production-a" {
		return os.WriteFile(reportPath, []byte("PASS\n"), 0o600)
	}
	populationCount := 1
	if behavior.zeroPopulation && binding.SubgateID == "semantic-production-a" {
		populationCount = 0
	}
	reportBinding := binding
	if behavior.staleReport && binding.SubgateID == "semantic-production-a" {
		reportBinding.CommandDigest = runnerChangedDigest
	}
	populationID := "cases"
	if binding.SubgateID == "clean-tree-freshness" {
		populationID = "verified-clean-tree-records"
	}
	report := Report{
		FormatVersion: ReportFormatVersion,
		Binding:       reportBinding,
		Status:        StatusPassed,
		Populations:   []Population{{ID: populationID, Count: populationCount}},
	}
	if err := writeExclusiveJSON(ctx, reportPath, report); err != nil {
		return err
	}
	registration, exists := registrationForProducer(registry, binding.SubgateID)
	if !exists {
		return nil
	}
	if !behavior.omitObservation || binding.SubgateID != "semantic-production-a" {
		observationRoot, exists := lookup(soundnessevidence.EnvEvidenceDir)
		if !exists {
			return errors.New("test producer has no observation root")
		}
		if behavior.forgedObservationMarker && binding.SubgateID == "semantic-production-a" {
			if err := os.WriteFile(filepath.Join(observationRoot, "forged.json"), []byte("PASS\n"), 0o600); err != nil {
				return fmt.Errorf("write forged observation: %w", err)
			}
		} else {
			observationBinding := binding
			if behavior.staleObservation && binding.SubgateID == "semantic-production-a" {
				observationBinding.CommandDigest = runnerChangedDigest
			}
			observation := validGateObservation(registration, observationBinding)
			if err := writeExclusiveJSON(ctx, filepath.Join(observationRoot, "observation.json"), observation); err != nil {
				return err
			}
			if behavior.duplicateObservation && binding.SubgateID == "semantic-production-a" {
				if err := writeExclusiveJSON(ctx, filepath.Join(observationRoot, "observation-copy.json"), observation); err != nil {
					return err
				}
			}
		}
	}
	if behavior.failAfterOutput && binding.SubgateID == "semantic-production-a" {
		return errors.New("unrelated failure")
	}
	return nil
}

func registrationForProducer(
	registry soundnessevidence.Registry,
	producerID string,
) (soundnessevidence.Registration, bool) {
	for _, registration := range registry.Registrations {
		if registration.ProducerID == producerID {
			return registration, true
		}
	}
	return soundnessevidence.Registration{}, false
}

func environmentLookup(environment []string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		prefix := name + "="
		for _, entry := range environment {
			if value, ok := strings.CutPrefix(entry, prefix); ok {
				return value, true
			}
		}
		return "", false
	}
}

func writeRunnerFixture(
	t *testing.T,
	manifest Manifest,
	registry soundnessevidence.Registry,
) (string, string, soundnessevidence.Registry) {
	t.Helper()
	root := t.TempDir()
	registryPath := filepath.Join(root, filepath.FromSlash(manifest.RegistryPath))
	manifestPath := filepath.Join(root, "spec", "soundness-gate.v1.json")
	writeGateJSON(t, registryPath, registry)
	writeGateJSON(t, manifestPath, manifest)
	return root, manifestPath, registry
}
