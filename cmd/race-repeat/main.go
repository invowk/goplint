// SPDX-License-Identifier: MPL-2.0

// Command race-repeat executes the analyzer package through build-once,
// timing-balanced, structured race and repeat work units.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/racerepeat"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func main() {
	moduleRoot := flag.String("module-root", ".", "goplint Go module root")
	repositoryRoot := flag.String("repository-root", "../..", "repository root for exact-tree binding")
	packagePath := flag.String("package", "./goplint", "analyzer package")
	timingPath := flag.String("timings", "spec/goplint-test-timings.v1.json", "reviewed timing manifest")
	shardCount := flag.Int("shards", racerepeat.DefaultShardCount, "analyzer shard count")
	repeatCount := flag.Int("repeat", 3, "normal execution count for every census member")
	maximumWorkers := flag.Int("max-workers", defaultMaximumWorkers(), "maximum concurrent analyzer work units")
	cpuPerWorker := flag.Int("cpu-per-worker", 0, "CPU limit per work unit (default: effective CPUs divided by workers)")
	outputDirectory := flag.String("output-dir", "", "optional retained output directory outside the workspace")
	workGroup := flag.String(
		"work-group", "",
		"optional deterministic work-unit group selector in index/count form (default: all units)",
	)
	flag.Parse()

	if err := execute(context.Background(), executeOptions{
		moduleRoot: *moduleRoot, repositoryRoot: *repositoryRoot, packagePath: *packagePath,
		timingPath: *timingPath, shardCount: *shardCount, repeatCount: *repeatCount,
		maximumWorkers: *maximumWorkers, cpuPerWorker: *cpuPerWorker, outputDirectory: *outputDirectory,
		workGroup: *workGroup,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "goplint race/repeat:", err)
		os.Exit(1)
	}
}

type executeOptions struct {
	moduleRoot, repositoryRoot, packagePath string
	timingPath, outputDirectory, workGroup  string
	shardCount, repeatCount                 int
	maximumWorkers, cpuPerWorker            int
}

func execute(ctx context.Context, options executeOptions) error {
	maximumWorkers, cpuPerWorker, err := resolveWorkerResources(
		runtime.GOMAXPROCS(0), options.maximumWorkers, options.cpuPerWorker,
	)
	if err != nil {
		return err
	}
	options.maximumWorkers = maximumWorkers
	options.cpuPerWorker = cpuPerWorker
	workspaceDigest, err := soundnessgate.WorkspaceDigest(ctx, options.repositoryRoot)
	if err != nil {
		return fmt.Errorf("calculate initial workspace digest: %w", err)
	}
	timing, err := racerepeat.LoadTimingManifest(options.timingPath)
	if err != nil {
		return fmt.Errorf("load reviewed timing manifest: %w", err)
	}
	if timing.Toolchain != runtime.Version() {
		return fmt.Errorf("reviewed timing toolchain = %q, want current %q", timing.Toolchain, runtime.Version())
	}
	timingData, err := os.ReadFile(options.timingPath)
	if err != nil {
		return fmt.Errorf("read reviewed timing manifest: %w", err)
	}
	outputDirectory := options.outputDirectory
	removeOutput := false
	if outputDirectory == "" {
		outputDirectory, err = os.MkdirTemp("", "goplint-race-repeat-")
		if err != nil {
			return fmt.Errorf("create temporary race/repeat output directory: %w", err)
		}
		removeOutput = true
	}
	if err := requireOutputOutsideWorkspace(options.repositoryRoot, outputDirectory); err != nil {
		return err
	}
	if removeOutput {
		defer os.RemoveAll(outputDirectory) //nolint:errcheck // Private successful-run cleanup is best effort.
	}
	binaryDirectory := filepath.Join(outputDirectory, "binaries")
	binaries, err := racerepeat.BuildBinaries(ctx, options.moduleRoot, options.packagePath, binaryDirectory)
	if err != nil {
		return fmt.Errorf("build race/repeat test binaries: %w", err)
	}
	normalCensus, err := racerepeat.CensusFromBinary(
		ctx, options.moduleRoot, filepath.Join(binaryDirectory, "goplint-normal.test"),
	)
	if err != nil {
		return fmt.Errorf("census normal test binary: %w", err)
	}
	raceCensus, err := racerepeat.CensusFromBinary(
		ctx, options.moduleRoot, filepath.Join(binaryDirectory, "goplint-race.test"),
	)
	if err != nil {
		return fmt.Errorf("census race test binary: %w", err)
	}
	if err := racerepeat.RequireEquivalentModeCensuses(normalCensus, raceCensus); err != nil {
		return fmt.Errorf("compare normal and race test censuses: %w", err)
	}
	plan, err := racerepeat.NewPlan(
		workspaceDigest, options.packagePath,
		racerepeat.ArtifactBinding{Name: filepath.ToSlash(options.timingPath), Digest: soundnessevidence.DigestBytes(timingData)},
		timing, normalCensus, binaries, options.shardCount, options.repeatCount,
	)
	if err != nil {
		return fmt.Errorf("create race/repeat plan: %w", err)
	}
	planData, err := racerepeat.CanonicalPlanJSON(plan)
	if err != nil {
		return fmt.Errorf("encode canonical race/repeat plan: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(outputDirectory, "plan.json"), planData); err != nil {
		return fmt.Errorf("retain race/repeat plan: %w", err)
	}
	selectedUnits := plan.WorkUnits
	if options.workGroup != "" {
		groupIndex, groupCount, err := racerepeat.ParseWorkGroup(options.workGroup)
		if err != nil {
			return fmt.Errorf("parse race/repeat work group: %w", err)
		}
		selectedUnits, err = racerepeat.SelectWorkGroup(plan.WorkUnits, groupIndex, groupCount)
		if err != nil {
			return fmt.Errorf("select race/repeat work group: %w", err)
		}
	}
	results, err := racerepeat.ExecutePlanUnits(ctx, plan, selectedUnits, racerepeat.ExecuteOptions{
		ModuleRoot: options.moduleRoot, BinaryDirectory: binaryDirectory,
		OutputDirectory: filepath.Join(outputDirectory, "work"),
		MaximumWorkers:  options.maximumWorkers, CPUPerWorker: options.cpuPerWorker,
	})
	if err != nil {
		return fmt.Errorf("execute race/repeat plan: %w", err)
	}
	finalWorkspaceDigest, err := soundnessgate.WorkspaceDigest(ctx, options.repositoryRoot)
	if err != nil {
		return fmt.Errorf("calculate final workspace digest: %w", err)
	}
	if finalWorkspaceDigest != workspaceDigest {
		return errors.New("workspace changed during race/repeat execution; no result accepted")
	}
	raceExecutions, repeatExecutions := 0, 0
	for _, unit := range selectedUnits {
		if unit.Mode == "race" {
			raceExecutions += len(unit.MemberIDs)
		} else {
			repeatExecutions += len(unit.MemberIDs)
		}
	}
	summary := struct {
		PlanID           string `json:"plan_id"`
		CensusSize       int    `json:"census_size"`
		WorkGroup        string `json:"work_group,omitempty"`
		WorkUnitCount    int    `json:"work_unit_count"`
		RaceExecutions   int    `json:"race_executions"`
		RepeatExecutions int    `json:"repeat_executions"`
	}{
		PlanID: plan.PlanID, CensusSize: len(plan.Census), WorkGroup: options.workGroup,
		WorkUnitCount: len(results), RaceExecutions: raceExecutions, RepeatExecutions: repeatExecutions,
	}
	if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
		return fmt.Errorf("encode race/repeat summary: %w", err)
	}
	return nil
}

func resolveWorkerResources(effectiveCPU, requestedWorkers, requestedCPUPerWorker int) (int, int, error) {
	if effectiveCPU <= 0 || requestedWorkers <= 0 {
		return 0, 0, errors.New("effective CPU and maximum workers must be positive")
	}
	if requestedWorkers > effectiveCPU {
		return 0, 0, fmt.Errorf("maximum workers %d exceeds reserved CPU %d", requestedWorkers, effectiveCPU)
	}
	maximumCPUPerWorker := effectiveCPU / requestedWorkers
	if requestedCPUPerWorker <= 0 {
		return requestedWorkers, max(1, maximumCPUPerWorker), nil
	}
	if requestedCPUPerWorker > maximumCPUPerWorker {
		return 0, 0, fmt.Errorf(
			"CPU per worker %d across %d workers exceeds reserved CPU %d",
			requestedCPUPerWorker, requestedWorkers, effectiveCPU,
		)
	}
	return requestedWorkers, requestedCPUPerWorker, nil
}

func defaultMaximumWorkers() int {
	if value := os.Getenv("GOPLINT_SOUNDNESS_GO_TEST_PARALLELISM"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return min(4, max(runtime.GOMAXPROCS(0), 1))
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create race/repeat output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".race-repeat-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary race/repeat output file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath) //nolint:errcheck // Best-effort private temporary cleanup.
	if _, err := temporary.Write(data); err != nil {
		temporary.Close() //nolint:errcheck // Preserve the primary write error.
		return fmt.Errorf("write temporary race/repeat output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary race/repeat output: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish race/repeat output: %w", err)
	}
	return nil
}

func requireOutputOutsideWorkspace(repositoryRoot, outputDirectory string) error {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve absolute repository root: %w", err)
	}
	output, err := filepath.Abs(outputDirectory)
	if err != nil {
		return fmt.Errorf("resolve absolute race/repeat output directory: %w", err)
	}
	relative, err := filepath.Rel(root, output)
	if err != nil {
		return fmt.Errorf("resolve race/repeat output relative to repository: %w", err)
	}
	if relative != ".." && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("race/repeat output directory %q must be outside workspace %q", output, root)
	}
	if relative == "." {
		return errors.New("race/repeat output directory must not be the workspace root")
	}
	return nil
}
