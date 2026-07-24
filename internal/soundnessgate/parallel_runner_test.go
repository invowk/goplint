// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

func TestRunPlanParallelMatchesLegacyNormalizedReportAndBoundsChildren(t *testing.T) {
	t.Parallel()

	root, manifestPath, registry := writeRunnerFixture(t, validGateManifest(), validGateRegistry())
	planDeps := deterministicPlanDependencies()
	plan, err := generatePlan(t.Context(), PlanOptions{
		Root: root, ManifestPath: manifestPath, Profile: ProfileCore,
		Resources: ResourceBudget{
			CPUUnits: 4, MemoryBytes: 16 * 1024 * 1024 * 1024,
			MaximumWorkers: 4, RunnerClass: "test-runner",
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
	parallelDeps, environments := parallelRunnerTestDependencies(t, registry)
	parallel, err := runPlanParallel(
		t.Context(), plan, PlanParallelOptions{Root: root}, parallelDeps, planDeps,
	)
	if err != nil {
		t.Fatalf("runPlanParallel() error = %v", err)
	}
	legacyJSON, err := NormalizedRunReportJSON(legacy.Report)
	if err != nil {
		t.Fatalf("legacy NormalizedRunReportJSON() error = %v", err)
	}
	parallelJSON, err := NormalizedRunReportJSON(parallel.Report)
	if err != nil {
		t.Fatalf("parallel NormalizedRunReportJSON() error = %v", err)
	}
	if !bytes.Equal(legacyJSON, parallelJSON) {
		t.Fatalf("parallel report/evidence differs from legacy:\nlegacy:   %s\nparallel: %s", legacyJSON, parallelJSON)
	}
	commands := make(map[string]PlanCommandBinding, len(plan.Commands))
	for _, command := range plan.Commands {
		commands[command.SubgateID] = command
	}
	for subgateID, environment := range environments() {
		wantParallelism := strconv.Itoa(commands[subgateID].ReservedResources.CPUUnits)
		lookup := environmentLookup(environment)
		if got, _ := lookup("GOMAXPROCS"); got != wantParallelism {
			t.Fatalf("subgate %q GOMAXPROCS = %q, want %q", subgateID, got, wantParallelism)
		}
		if got, _ := lookup(envGoTestParallelism); got != wantParallelism {
			t.Fatalf("subgate %q test parallelism = %q, want %q", subgateID, got, wantParallelism)
		}
		if got, _ := lookup("GOFLAGS"); !strings.Contains(got, "-p="+wantParallelism) {
			t.Fatalf("subgate %q GOFLAGS = %q, want bounded build parallelism", subgateID, got)
		}
	}
	if parallel.Telemetry.MaxReservedCPUUnits > plan.Resources.CPUUnits ||
		parallel.Telemetry.MaxReservedMemoryBytes > plan.Resources.MemoryBytes {
		t.Fatalf("parallel telemetry exceeds plan budget: %+v", parallel.Telemetry)
	}
}

func TestRunPlanParallelTimeoutCleansPrivateEvidence(t *testing.T) {
	t.Parallel()

	manifest := validGateManifest()
	for index := range manifest.Subgates {
		manifest.Subgates[index].TimeoutSeconds = 1
	}
	root, manifestPath, _ := writeRunnerFixture(t, manifest, validGateRegistry())
	planDeps := deterministicPlanDependencies()
	plan, err := generatePlan(t.Context(), PlanOptions{
		Root: root, ManifestPath: manifestPath, Profile: ProfileCore,
		Resources: ResourceBudget{
			CPUUnits: 4, MemoryBytes: 16 * 1024 * 1024 * 1024,
			MaximumWorkers: 4, RunnerClass: "test-runner",
		},
	}, planDeps)
	if err != nil {
		t.Fatalf("generatePlan() error = %v", err)
	}
	evidenceRoot := filepath.Join(t.TempDir(), "timeout-evidence")
	dependencies := runnerDependencies{
		workspaceDigest: func(context.Context, string) (string, error) { return runnerTestWorkspaceDigest, nil },
		execute: func(ctx context.Context, _ string, _ []string, _ []string, _ io.Writer, _ io.Writer) (commandMetrics, error) {
			<-ctx.Done()
			return commandMetrics{}, context.Cause(ctx)
		},
		makeTempDir: func(string, string) (string, error) {
			return evidenceRoot, os.Mkdir(evidenceRoot, 0o755)
		},
		newRunID: func() (string, error) { return "run-test", nil },
		now:      time.Now,
	}
	_, err = runPlanParallel(t.Context(), plan, PlanParallelOptions{Root: root}, dependencies, planDeps)
	assertGateErrorContains(t, err, "timed out or was canceled")
	if _, statErr := os.Stat(evidenceRoot); !os.IsNotExist(statErr) {
		t.Fatalf("private evidence root cleanup error = %v, want removed", statErr)
	}
}

func TestParallelCommandWriterWrapsRegularFilesForConcurrentChildren(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "parallel-output-*.log")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	wrapped := parallelCommandWriter(file)
	if wrapped == file {
		t.Fatal("parallelCommandWriter() returned the raw regular file")
	}
	if _, ok := wrapped.(*synchronizedWriter); !ok {
		t.Fatalf("parallelCommandWriter() type = %T, want *synchronizedWriter", wrapped)
	}

	const writes = 64
	var waitGroup sync.WaitGroup
	for range writes {
		waitGroup.Go(func() {
			if _, writeErr := wrapped.Write([]byte("soundness-output\n")); writeErr != nil {
				t.Errorf("Write() error = %v", writeErr)
			}
		})
	}
	waitGroup.Wait()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got, want := strings.Count(string(data), "soundness-output\n"), writes; got != want {
		t.Fatalf("serialized output count = %d, want %d", got, want)
	}
}

func parallelRunnerTestDependencies(
	t *testing.T,
	registry soundnessevidence.Registry,
) (runnerDependencies, func() map[string][]string) {
	t.Helper()
	temporaryBase := t.TempDir()
	now := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	var mutex sync.Mutex
	environments := make(map[string][]string)
	dependencies := runnerDependencies{
		workspaceDigest: func(context.Context, string) (string, error) {
			return runnerTestWorkspaceDigest, nil
		},
		execute: func(
			ctx context.Context,
			_ string,
			_ []string,
			environment []string,
			_ io.Writer,
			_ io.Writer,
		) (commandMetrics, error) {
			lookup := environmentLookup(environment)
			subgateID, _ := lookup(soundnessevidence.EnvSubgateID)
			mutex.Lock()
			environments[subgateID] = append([]string(nil), environment...)
			mutex.Unlock()
			if err := emitTestProducerOutputs(ctx, registry, environment, producerBehavior{}); err != nil {
				return commandMetrics{}, err
			}
			return commandMetrics{CPUTimeNanoseconds: 7, PeakRSSBytes: 11}, nil
		},
		makeTempDir: func(string, string) (string, error) {
			path := filepath.Join(temporaryBase, "evidence")
			if err := os.Mkdir(path, 0o755); err != nil {
				return "", fmt.Errorf("create test evidence root: %w", err)
			}
			return path, nil
		},
		newRunID: func() (string, error) { return "run-test", nil },
		now: func() time.Time {
			mutex.Lock()
			defer mutex.Unlock()
			result := now
			now = now.Add(time.Second)
			return result
		},
	}
	return dependencies, func() map[string][]string {
		mutex.Lock()
		defer mutex.Unlock()
		result := make(map[string][]string, len(environments))
		for key, value := range environments {
			result[key] = append([]string(nil), value...)
		}
		return result
	}
}
