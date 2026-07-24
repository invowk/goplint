// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

const testCommandWaitDelay = 10 * time.Second

type (
	// ExecuteOptions configures local execution of an immutable race/repeat plan.
	ExecuteOptions struct {
		ModuleRoot      string
		BinaryDirectory string
		OutputDirectory string
		MaximumWorkers  int
		CPUPerWorker    int
	}

	testCommandFunc func(context.Context, string, string, WorkUnit, int, []string) ([]byte, error)

	workOutcome struct {
		result WorkResult
		err    error
	}
)

// ExecutePlan executes every immutable work unit with bounded local
// concurrency and retains isolated structured outputs and result bundles.
func ExecutePlan(ctx context.Context, plan Plan, options ExecuteOptions) ([]WorkResult, error) {
	return executePlan(ctx, plan, plan.WorkUnits, options, executeTest2JSON)
}

// ExecutePlanUnits executes one exact subset of plan work units, for
// deterministic distributed work groups. Every selected unit must be an
// exact plan unit; completeness across groups is owned by the deterministic
// group partition and the aggregate's required work-unit set.
func ExecutePlanUnits(
	ctx context.Context,
	plan Plan,
	units []WorkUnit,
	options ExecuteOptions,
) ([]WorkResult, error) {
	planned := make(map[string]int, len(plan.WorkUnits))
	for index, unit := range plan.WorkUnits {
		planned[unit.ID] = index
	}
	for _, unit := range units {
		index, exists := planned[unit.ID]
		if !exists {
			return nil, fmt.Errorf("race/repeat selected work unit %q is not planned", unit.ID)
		}
		if !workUnitsEqual(plan.WorkUnits[index], unit) {
			return nil, fmt.Errorf("race/repeat selected work unit %q does not match its plan binding", unit.ID)
		}
	}
	return executePlan(ctx, plan, units, options, executeTest2JSON)
}

func workUnitsEqual(left, right WorkUnit) bool {
	return left.ID == right.ID && left.Mode == right.Mode && left.Iteration == right.Iteration &&
		left.TotalWeight == right.TotalWeight && left.TimeoutSeconds == right.TimeoutSeconds &&
		left.BinaryDigest == right.BinaryDigest && slices.Equal(left.MemberIDs, right.MemberIDs)
}

func executePlan(
	ctx context.Context,
	plan Plan,
	units []WorkUnit,
	options ExecuteOptions,
	run testCommandFunc,
) ([]WorkResult, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if len(units) == 0 {
		return nil, errors.New("race/repeat execution requires at least one selected work unit")
	}
	if options.ModuleRoot == "" || options.BinaryDirectory == "" || options.OutputDirectory == "" ||
		options.MaximumWorkers <= 0 || options.CPUPerWorker <= 0 {
		return nil, errors.New("race/repeat execution has invalid paths or resource bounds")
	}
	if err := verifyBinaryBindings(plan.Binaries, options.BinaryDirectory); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(options.OutputDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create race/repeat output directory: %w", err)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan WorkUnit)
	outcomes := make(chan workOutcome)
	workerCount := min(options.MaximumWorkers, len(units))
	var workers sync.WaitGroup
	for range workerCount {
		workers.Go(func() {
			for unit := range jobs {
				result, err := executeWorkUnit(workerCtx, plan, options, unit, run)
				outcomes <- workOutcome{result: result, err: err}
				if err != nil {
					cancel()
				}
			}
		})
	}
	go func() {
		defer close(jobs)
		for _, unit := range units {
			select {
			case jobs <- unit:
			case <-workerCtx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(outcomes)
	}()
	results := make([]WorkResult, 0, len(units))
	var firstErr error
	for outcome := range outcomes {
		if outcome.result.WorkUnitID != "" {
			results = append(results, outcome.result)
		}
		if outcome.err != nil && firstErr == nil {
			firstErr = outcome.err
		}
	}
	if firstErr != nil {
		return results, firstErr
	}
	if len(results) != len(units) {
		return results, fmt.Errorf("race/repeat execution retained %d of %d work results", len(results), len(units))
	}
	slices.SortFunc(results, func(left, right WorkResult) int { return strings.Compare(left.WorkUnitID, right.WorkUnitID) })
	return results, nil
}

func executeWorkUnit(
	ctx context.Context,
	plan Plan,
	options ExecuteOptions,
	unit WorkUnit,
	run testCommandFunc,
) (WorkResult, error) {
	binary := binaryForMode(plan.Binaries, unit.Mode)
	binaryPath := filepath.Join(options.BinaryDirectory, binary.FileName)
	packageDirectory, err := packageWorkingDirectory(options.ModuleRoot, plan.Package)
	if err != nil {
		return WorkResult{}, err
	}
	unitCtx, cancel := context.WithTimeout(ctx, time.Duration(unit.TimeoutSeconds)*time.Second)
	output, runErr := run(unitCtx, packageDirectory, binaryPath, unit, options.CPUPerWorker, plan.Environment)
	contextErr := unitCtx.Err()
	cancel()
	status := statusPassed
	if contextErr != nil {
		status = statusCanceled
		if errors.Is(contextErr, context.DeadlineExceeded) {
			status = statusTimedOut
		}
	} else if runErr != nil {
		status = statusFailed
	}
	outputPath := filepath.Join(options.OutputDirectory, unit.ID+".test2json")
	if err := writeAtomic(outputPath, output); err != nil {
		return WorkResult{}, err
	}
	result, parseErr := ParseWorkResult(plan, unit, output, status)
	if parseErr != nil {
		if runErr != nil {
			parseErr = fmt.Errorf("%w; command error: %w", parseErr, runErr)
		}
		return result, parseErr
	}
	encoded, err := CanonicalWorkResultJSON(result, plan)
	if err != nil {
		return result, err
	}
	if err := writeAtomic(filepath.Join(options.OutputDirectory, unit.ID+".result.json"), encoded); err != nil {
		return result, err
	}
	return result, nil
}

func executeTest2JSON(
	ctx context.Context,
	packageDirectory, binaryPath string,
	unit WorkUnit,
	cpuPerWorker int,
	plannedEnvironment []string,
) ([]byte, error) {
	quoted := make([]string, 0, len(unit.MemberIDs))
	for _, memberID := range unit.MemberIDs {
		quoted = append(quoted, regexp.QuoteMeta(memberID))
	}
	pattern := "^(" + strings.Join(quoted, "|") + ")$"
	arguments := []string{
		"tool", "test2json", "-t", "-p", "goplint", binaryPath,
		"-test.v=test2json", "-test.run", pattern, "-test.count=1",
		"-test.timeout=" + strconv.Itoa(unit.TimeoutSeconds) + "s",
		"-test.parallel=" + strconv.Itoa(max(cpuPerWorker, 1)),
	}
	command := exec.CommandContext(ctx, "go", arguments...)
	command.Dir = packageDirectory
	command.WaitDelay = testCommandWaitDelay
	command.Env = append(boundedTestEnvironment(os.Environ(), cpuPerWorker), plannedEnvironment...)
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("go %v: %w", arguments, err)
	}
	return output, nil
}

func packageWorkingDirectory(moduleRoot, packagePath string) (string, error) {
	if !strings.HasPrefix(packagePath, "./") {
		return "", fmt.Errorf("race/repeat package %q must be module-relative", packagePath)
	}
	relative := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(packagePath, "./")))
	if relative == "." || relative == "" || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("race/repeat package %q does not name a package directory", packagePath)
	}
	return filepath.Join(moduleRoot, relative), nil
}

func boundedTestEnvironment(environment []string, cpuPerWorker int) []string {
	removePrefixes := []string{
		"GOMAXPROCS=", soundnessevidence.EnvEvidenceDir + "=", soundnessevidence.EnvSubgateID + "=",
		soundnessgate.EnvSubgateReportPath + "=",
	}
	result := slices.DeleteFunc(slices.Clone(environment), func(entry string) bool {
		return slices.ContainsFunc(removePrefixes, func(prefix string) bool { return strings.HasPrefix(entry, prefix) })
	})
	return append(result, "GOMAXPROCS="+strconv.Itoa(max(cpuPerWorker, 1)))
}

func verifyBinaryBindings(bindings []BinaryBinding, directory string) error {
	for _, binding := range bindings {
		data, err := os.ReadFile(filepath.Join(directory, binding.FileName))
		if err != nil {
			return fmt.Errorf("read race/repeat %s binary: %w", binding.Mode, err)
		}
		if soundnessevidence.DigestBytes(data) != binding.Digest {
			return fmt.Errorf("race/repeat %s binary digest changed after planning", binding.Mode)
		}
	}
	return nil
}

func binaryForMode(bindings []BinaryBinding, mode string) BinaryBinding {
	for _, binding := range bindings {
		if binding.Mode == mode {
			return binding
		}
	}
	return BinaryBinding{}
}

func writeAtomic(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".race-repeat-*.tmp")
	if err != nil {
		return fmt.Errorf("create race/repeat temporary output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath) //nolint:errcheck // Best-effort private temporary cleanup.
	if _, err := temporary.Write(data); err != nil {
		temporary.Close() //nolint:errcheck // Preserve the primary write error.
		return fmt.Errorf("write race/repeat temporary output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close race/repeat temporary output: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish race/repeat output: %w", err)
	}
	return nil
}
