// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const envGoTestParallelism = "GOPLINT_SOUNDNESS_GO_TEST_PARALLELISM"

type (
	// PlanParallelOptions configures resource-aware local execution of an
	// immutable execution plan.
	PlanParallelOptions struct {
		Root          string
		ReportPath    string
		TelemetryPath string
		Stdout        io.Writer
		Stderr        io.Writer
	}

	workUnitProduct struct {
		SubgateResult SubgateResult
		Observations  []soundnessevidence.SemanticObservation
		Binding       soundnessevidence.ObservationBinding
		Metrics       commandMetrics
	}

	reservationEvent struct {
		at     time.Time
		cpu    int
		memory int64
	}

	synchronizedWriter struct {
		writer io.Writer
		mutex  sync.Mutex
	}
)

// RunPlanParallel validates an immutable plan against the exact current
// inputs and executes dependency-ready work within its resource policy.
func RunPlanParallel(ctx context.Context, plan ExecutionPlan, options PlanParallelOptions) (Result, error) {
	runnerDeps := runnerDependencies{
		workspaceDigest: WorkspaceDigest,
		execute:         executeCommand,
		makeTempDir:     os.MkdirTemp,
		newRunID:        newRunID,
		now:             time.Now,
	}
	planDeps := planDependencies{
		workspaceDigest: WorkspaceDigest,
		binaryDigest:    executableDigest,
		toolchain:       currentToolchainBinding,
	}
	return runPlanParallel(ctx, plan, options, runnerDeps, planDeps)
}

func runPlanParallel(
	ctx context.Context,
	plan ExecutionPlan,
	options PlanParallelOptions,
	runnerDeps runnerDependencies,
	planDeps planDependencies,
) (Result, error) {
	if plan.Resources.SerialReference {
		return Result{}, errors.New("parallel soundness execution rejects a serial-reference plan")
	}
	if err := validateCurrentPlan(ctx, plan, options.Root, planDeps); err != nil {
		return Result{}, err
	}
	root, manifestPath, err := resolvePaths(options.Root, plan.Manifest.Path)
	if err != nil {
		return Result{}, err
	}
	manifest, manifestDigest, err := LoadManifest(ctx, manifestPath)
	if err != nil {
		return Result{}, err
	}
	registryPath := filepath.Join(root, filepath.FromSlash(manifest.RegistryPath))
	registry, err := soundnessevidence.LoadRegistry(ctx, registryPath)
	if err != nil {
		return Result{}, fmt.Errorf("load soundness evidence registry %s: %w", registryPath, err)
	}
	if err := manifest.Validate(registry); err != nil {
		return Result{}, err
	}
	subgates, _, err := unshardedSubgatesFromPlan(plan)
	if err != nil {
		return Result{}, err
	}
	subgatesByID := make(map[string]Subgate, len(subgates))
	for _, subgate := range subgates {
		subgatesByID[subgate.ID] = subgate
	}
	initialWorkspaceDigest, err := runnerDeps.workspaceDigest(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("compute initial soundness workspace digest: %w", err)
	}
	if initialWorkspaceDigest != plan.Workspace.Digest {
		return Result{}, errors.New("parallel soundness execution observed a workspace different from its plan")
	}
	runID, err := runnerDeps.newRunID()
	if err != nil {
		return Result{}, fmt.Errorf("create soundness run id: %w", err)
	}
	evidenceRoot, err := runnerDeps.makeTempDir("", "goplint-soundness-parallel-")
	if err != nil {
		return Result{}, fmt.Errorf("create parallel evidence root: %w", err)
	}
	defer os.RemoveAll(evidenceRoot) //nolint:errcheck // Private temporary evidence cleanup is intentionally best effort.
	stdout := parallelCommandWriter(options.Stdout)
	stderr := parallelCommandWriter(options.Stderr)
	runStartedAt := runnerDeps.now().UTC()
	scheduled, err := schedulePlan(ctx, plan, runnerDeps.now, func(workerCtx context.Context, command PlanCommandBinding) (workUnitProduct, error) {
		subgate, exists := subgatesByID[command.SubgateID]
		if !exists {
			return workUnitProduct{}, fmt.Errorf("planned subgate %q is unavailable", command.SubgateID)
		}
		binding, err := createSubgateBinding(runID, initialWorkspaceDigest, manifestDigest, subgate)
		if err != nil {
			return workUnitProduct{}, err
		}
		subgateRoot := filepath.Join(evidenceRoot, command.WorkUnitID)
		observationRoot := filepath.Join(subgateRoot, "observations")
		if err := os.MkdirAll(observationRoot, 0o755); err != nil {
			return workUnitProduct{}, fmt.Errorf("create soundness work unit %q evidence directory: %w", command.WorkUnitID, err)
		}
		reportPath := filepath.Join(subgateRoot, filepath.FromSlash(subgate.ReportFile))
		environment := subgateEnvironment(
			os.Environ(), binding, observationRoot, reportPath,
			filepath.Join(evidenceRoot, "repository-audit.json"),
		)
		environment = boundedGoEnvironment(environment, command.ReservedResources.CPUUnits)
		workingDirectory := filepath.Join(root, filepath.FromSlash(command.WorkingDirectory))
		commandCtx, cancel := context.WithTimeout(workerCtx, time.Duration(command.TimeoutSeconds)*time.Second)
		metrics, executeErr := runnerDeps.execute(commandCtx, workingDirectory, command.Command, environment, stdout, stderr)
		commandErr := commandCtx.Err()
		cancel()
		if commandErr != nil {
			return workUnitProduct{}, fmt.Errorf("timed out or was canceled; no evidence accepted: %w", commandErr)
		}
		if executeErr != nil {
			return workUnitProduct{}, fmt.Errorf("command failed; no evidence accepted: %w", executeErr)
		}
		report, reportDigest, err := loadReportWithDigest(workerCtx, reportPath)
		if err != nil {
			return workUnitProduct{}, fmt.Errorf("did not produce its required report: %w", err)
		}
		if err := subgate.ValidateReport(report, binding); err != nil {
			return workUnitProduct{}, err
		}
		observations, err := soundnessevidence.LoadObservations(workerCtx, observationRoot)
		if err != nil {
			return workUnitProduct{}, fmt.Errorf("load observations: %w", err)
		}
		return workUnitProduct{
			SubgateResult: SubgateResult{
				ID: subgate.ID, CommandDigest: binding.CommandDigest,
				ReportDigest: reportDigest, Populations: slices.Clone(report.Populations),
			},
			Observations: observations,
			Binding:      binding,
			Metrics:      metrics,
		}, nil
	})
	if err != nil {
		return Result{}, err
	}
	runFinishedAt := runnerDeps.now().UTC()
	expectedBindings := make(map[string]soundnessevidence.ObservationBinding, len(scheduled))
	observations := make([]soundnessevidence.SemanticObservation, 0, len(registry.Registrations))
	subgateResults := make([]SubgateResult, 0, len(scheduled))
	subgateTelemetry := make([]SubgateTelemetry, 0, len(scheduled))
	commandsByID := make(map[string]PlanCommandBinding, len(plan.Commands))
	for _, command := range plan.Commands {
		commandsByID[command.WorkUnitID] = command
	}
	for _, result := range scheduled {
		command := commandsByID[result.WorkUnitID]
		product := result.Value
		expectedBindings[command.SubgateID] = product.Binding
		observations = append(observations, product.Observations...)
		subgateResults = append(subgateResults, product.SubgateResult)
		subgateTelemetry = append(subgateTelemetry, SubgateTelemetry{
			ID: command.SubgateID, QueuedAt: result.QueuedAt, StartedAt: result.StartedAt, FinishedAt: result.FinishedAt,
			QueueDurationNanoseconds: result.StartedAt.Sub(result.QueuedAt).Nanoseconds(),
			WallDurationNanoseconds:  result.FinishedAt.Sub(result.StartedAt).Nanoseconds(),
			CPUTimeNanoseconds:       product.Metrics.CPUTimeNanoseconds,
			PeakRSSBytes:             product.Metrics.PeakRSSBytes,
			ReservedResources:        command.ReservedResources,
			TimeoutSeconds:           command.TimeoutSeconds,
			TerminalStatus:           terminalStatusPassed,
			Populations:              slices.Clone(product.SubgateResult.Populations),
		})
	}
	if err := soundnessevidence.ValidateObservations(registry, observations, expectedBindings); err != nil {
		return Result{}, fmt.Errorf("validate aggregate semantic census: %w", err)
	}
	finalWorkspaceDigest, err := runnerDeps.workspaceDigest(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("compute final soundness workspace digest: %w", err)
	}
	if finalWorkspaceDigest != initialWorkspaceDigest {
		return Result{}, errors.New("soundness workspace changed during parallel aggregate execution")
	}
	slices.SortFunc(observations, func(left, right soundnessevidence.SemanticObservation) int {
		return strings.Compare(left.RegistrationID, right.RegistrationID)
	})
	slices.SortFunc(subgateResults, func(left, right SubgateResult) int { return strings.Compare(left.ID, right.ID) })
	runReport := RunReport{
		FormatVersion: RunReportFormatVersion, Profile: plan.Profile, RunID: runID,
		WorkspaceDigest: initialWorkspaceDigest, ManifestDigest: manifestDigest,
		Subgates: subgateResults, Observations: observations,
	}
	if err := ValidateRunReport(runReport, manifest, registry); err != nil {
		return Result{}, fmt.Errorf("validate retained aggregate report: %w", err)
	}
	retainedReportPath, err := resolveReportPath(root, options.ReportPath)
	if err != nil {
		return Result{}, err
	}
	if retainedReportPath != "" {
		if err := writeExclusiveJSON(ctx, retainedReportPath, runReport); err != nil {
			return Result{}, fmt.Errorf("retain aggregate soundness report: %w", err)
		}
	}
	maximumCPU, maximumMemory := maximumConcurrentReservations(scheduled, commandsByID)
	telemetry := RunTelemetry{
		FormatVersion: TelemetryFormatVersion, RunID: runID, Profile: plan.Profile,
		WorkspaceDigest: initialWorkspaceDigest, ManifestDigest: manifestDigest,
		StartedAt: runStartedAt, FinishedAt: runFinishedAt,
		WallDurationNanoseconds: runFinishedAt.Sub(runStartedAt).Nanoseconds(),
		CriticalPathNanoseconds: criticalPathNanoseconds(scheduled, plan.Dependencies),
		MaxReservedCPUUnits:     maximumCPU, MaxReservedMemoryBytes: maximumMemory,
		Subgates: subgateTelemetry,
	}
	if err := telemetry.Validate(); err != nil {
		return Result{}, fmt.Errorf("validate aggregate telemetry: %w", err)
	}
	retainedTelemetryPath, err := resolveExternalArtifactPath(root, options.TelemetryPath, EnvTelemetryPath, "aggregate soundness telemetry")
	if err != nil {
		return Result{}, err
	}
	if retainedTelemetryPath != "" {
		if err := writeExclusiveJSON(ctx, retainedTelemetryPath, telemetry); err != nil {
			return Result{}, fmt.Errorf("retain aggregate soundness telemetry: %w", err)
		}
	}
	return Result{
		Profile: plan.Profile, Registry: registry, Report: runReport, Telemetry: telemetry,
		RunID: runID, WorkspaceDigest: initialWorkspaceDigest, ManifestDigest: manifestDigest,
		ReportPath: retainedReportPath, TelemetryPath: retainedTelemetryPath,
		SubgateCount: len(subgateResults), ObservationCount: len(observations),
	}, nil
}

func parallelCommandWriter(writer io.Writer) io.Writer {
	if writer == nil {
		return io.Discard
	}
	return &synchronizedWriter{writer: writer}
}

func (writer *synchronizedWriter) Write(data []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	written, err := writer.writer.Write(data)
	if err != nil {
		return written, fmt.Errorf("write synchronized soundness output: %w", err)
	}
	return written, nil
}

func boundedGoEnvironment(environment []string, cpuUnits int) []string {
	parallelism := strconv.Itoa(max(cpuUnits, 1))
	result := slices.DeleteFunc(slices.Clone(environment), func(entry string) bool {
		return strings.HasPrefix(entry, "GOMAXPROCS=") || strings.HasPrefix(entry, "GOFLAGS=") ||
			strings.HasPrefix(entry, envGoTestParallelism+"=")
	})
	goFlags := "-p=" + parallelism
	for _, entry := range environment {
		if value, exists := strings.CutPrefix(entry, "GOFLAGS="); exists && strings.TrimSpace(value) != "" {
			goFlags = strings.TrimSpace(value) + " " + goFlags
		}
	}
	return append(result,
		"GOMAXPROCS="+parallelism,
		"GOFLAGS="+goFlags,
		envGoTestParallelism+"="+parallelism,
	)
}

func maximumConcurrentReservations(
	results []scheduledWorkResult[workUnitProduct],
	commands map[string]PlanCommandBinding,
) (int, int64) {
	events := make([]reservationEvent, 0, len(results)*2)
	for _, result := range results {
		reservation := commands[result.WorkUnitID].ReservedResources
		events = append(events,
			reservationEvent{at: result.StartedAt, cpu: reservation.CPUUnits, memory: reservation.MemoryBytes},
			reservationEvent{at: result.FinishedAt, cpu: -reservation.CPUUnits, memory: -reservation.MemoryBytes},
		)
	}
	slices.SortFunc(events, func(left, right reservationEvent) int {
		if compared := left.at.Compare(right.at); compared != 0 {
			return compared
		}
		return left.cpu - right.cpu
	})
	currentCPU, maximumCPU := 0, 0
	var currentMemory, maximumMemory int64
	for _, event := range events {
		currentCPU += event.cpu
		currentMemory += event.memory
		maximumCPU = max(maximumCPU, currentCPU)
		maximumMemory = max(maximumMemory, currentMemory)
	}
	return maximumCPU, maximumMemory
}

func criticalPathNanoseconds(
	results []scheduledWorkResult[workUnitProduct],
	dependencies []PlanDependencyBinding,
) int64 {
	durations := make(map[string]int64, len(results))
	for _, result := range results {
		durations[result.WorkUnitID] = result.FinishedAt.Sub(result.StartedAt).Nanoseconds()
	}
	requires := make(map[string][]string, len(dependencies))
	for _, dependency := range dependencies {
		requires[dependency.WorkUnitID] = dependency.Requires
	}
	memo := make(map[string]int64, len(results))
	var visit func(string) int64
	visit = func(workUnitID string) int64 {
		if value, exists := memo[workUnitID]; exists {
			return value
		}
		var predecessorMaximum int64
		for _, dependencyID := range requires[workUnitID] {
			predecessorMaximum = max(predecessorMaximum, visit(dependencyID))
		}
		memo[workUnitID] = predecessorMaximum + durations[workUnitID]
		return memo[workUnitID]
	}
	var criticalPath int64
	for workUnitID := range durations {
		criticalPath = max(criticalPath, visit(workUnitID))
	}
	return criticalPath
}
