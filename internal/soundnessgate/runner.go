// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/gitenv"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const commandWaitDelay = 10 * time.Second

type (
	// Options configures one aggregate soundness run.
	Options struct {
		Root          string
		ManifestPath  string
		Profile       ProfileID
		ReportPath    string
		TelemetryPath string
		Stdout        io.Writer
		Stderr        io.Writer
		executionPlan *ExecutionPlan
	}

	// Result identifies the exact workspace, manifest, and observations accepted
	// by a successful aggregate run.
	Result struct {
		Profile          ProfileID
		Registry         soundnessevidence.Registry
		Report           RunReport
		Telemetry        RunTelemetry
		RunID            string
		WorkspaceDigest  string
		ManifestDigest   string
		ReportPath       string
		TelemetryPath    string
		SubgateCount     int
		ObservationCount int
	}

	runnerDependencies struct {
		workspaceDigest func(context.Context, string) (string, error)
		execute         func(context.Context, string, []string, []string, io.Writer, io.Writer) (commandMetrics, error)
		makeTempDir     func(string, string) (string, error)
		newRunID        func() (string, error)
		now             func() time.Time
	}

	commandMetrics struct {
		CPUTimeNanoseconds int64
		PeakRSSBytes       int64
	}

	workspaceEntry struct {
		Path   string `json:"path"`
		Kind   string `json:"kind"`
		Mode   uint32 `json:"mode"`
		Digest string `json:"digest"`
	}
)

// run executes every selected manifest command serially in a fresh output
// root and validates command-bound reports plus the bidirectional semantic
// census. It is the reference serial engine behind RunPlanSerial.
func run(ctx context.Context, options Options, dependencies runnerDependencies) (Result, error) {
	root, manifestPath, err := resolvePaths(options.Root, options.ManifestPath)
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
	profile := options.Profile
	if profile == "" {
		profile = ProfileComplete
	}
	selectedSubgates, err := manifest.SubgatesForProfile(profile)
	if err != nil {
		return Result{}, err
	}
	selectedSubgates, err = dependencyOrderedSubgates(selectedSubgates)
	if err != nil {
		return Result{}, err
	}
	reservations := make(map[string]ResourceReservation, len(selectedSubgates))
	if options.executionPlan != nil {
		selectedSubgates, reservations, err = serialSubgatesFromPlan(*options.executionPlan)
		if err != nil {
			return Result{}, err
		}
	}
	initialWorkspaceDigest, err := dependencies.workspaceDigest(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("compute initial soundness workspace digest: %w", err)
	}
	runID, err := dependencies.newRunID()
	if err != nil {
		return Result{}, fmt.Errorf("create soundness run id: %w", err)
	}
	evidenceRoot, err := dependencies.makeTempDir("", "goplint-soundness-")
	if err != nil {
		return Result{}, fmt.Errorf("create aggregate evidence root: %w", err)
	}
	defer os.RemoveAll(evidenceRoot) //nolint:errcheck // Cleanup of the private temporary evidence tree is intentionally best effort.
	runStartedAt := dependencies.now().UTC()
	legacyReservation := ResourceReservation{
		CPUUnits:    runtime.GOMAXPROCS(0),
		MemoryBytes: 0,
		WorkerSlots: 1,
	}

	stdout := options.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := options.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	expectedBindings := make(map[string]soundnessevidence.ObservationBinding, len(selectedSubgates))
	observations := make([]soundnessevidence.SemanticObservation, 0, len(registry.Registrations))
	subgateResults := make([]SubgateResult, 0, len(selectedSubgates))
	subgateTelemetry := make([]SubgateTelemetry, 0, len(selectedSubgates))
	for index := range selectedSubgates {
		subgate := selectedSubgates[index]
		reservation := legacyReservation
		if plannedReservation, exists := reservations[subgate.ID]; exists {
			reservation = plannedReservation
		}
		subgateStartedAt := dependencies.now().UTC()
		binding, err := createSubgateBinding(runID, initialWorkspaceDigest, manifestDigest, subgate)
		if err != nil {
			return Result{}, err
		}
		expectedBindings[subgate.ID] = binding
		subgateRoot := filepath.Join(evidenceRoot, subgate.ID)
		observationRoot := filepath.Join(subgateRoot, "observations")
		if err := os.MkdirAll(observationRoot, 0o755); err != nil {
			return Result{}, fmt.Errorf("create soundness subgate %q evidence directory: %w", subgate.ID, err)
		}
		reportPath := filepath.Join(subgateRoot, filepath.FromSlash(subgate.ReportFile))
		environment := subgateEnvironment(
			os.Environ(), binding, observationRoot, reportPath,
			filepath.Join(evidenceRoot, "repository-audit.json"),
		)
		workingDirectory := filepath.Join(root, filepath.FromSlash(subgate.WorkingDirectory))
		commandCtx, cancel := context.WithTimeout(ctx, time.Duration(subgate.TimeoutSeconds)*time.Second)
		metrics, executeErr := dependencies.execute(commandCtx, workingDirectory, subgate.Command, environment, stdout, stderr)
		subgateFinishedAt := dependencies.now().UTC()
		commandErr := commandCtx.Err()
		cancel()
		if commandErr != nil {
			return Result{}, fmt.Errorf("soundness subgate %q timed out or was canceled; no evidence accepted: %w", subgate.ID, commandErr)
		}
		if executeErr != nil {
			return Result{}, fmt.Errorf("soundness subgate %q command failed; no evidence accepted: %w", subgate.ID, executeErr)
		}
		report, reportDigest, err := loadReportWithDigest(ctx, reportPath)
		if err != nil {
			return Result{}, fmt.Errorf("soundness subgate %q did not produce its required report: %w", subgate.ID, err)
		}
		if err := subgate.ValidateReport(report, binding); err != nil {
			return Result{}, err
		}
		subgateResults = append(subgateResults, SubgateResult{
			ID:            subgate.ID,
			CommandDigest: binding.CommandDigest,
			ReportDigest:  reportDigest,
			Populations:   slices.Clone(report.Populations),
		})
		subgateTelemetry = append(subgateTelemetry, SubgateTelemetry{
			ID:                       subgate.ID,
			QueuedAt:                 runStartedAt,
			StartedAt:                subgateStartedAt,
			FinishedAt:               subgateFinishedAt,
			QueueDurationNanoseconds: subgateStartedAt.Sub(runStartedAt).Nanoseconds(),
			WallDurationNanoseconds:  subgateFinishedAt.Sub(subgateStartedAt).Nanoseconds(),
			CPUTimeNanoseconds:       metrics.CPUTimeNanoseconds,
			PeakRSSBytes:             metrics.PeakRSSBytes,
			ReservedResources:        reservation,
			TimeoutSeconds:           subgate.TimeoutSeconds,
			TimedOut:                 false,
			TerminalStatus:           terminalStatusPassed,
			Populations:              slices.Clone(report.Populations),
		})
		subgateObservations, err := soundnessevidence.LoadObservations(ctx, observationRoot)
		if err != nil {
			return Result{}, fmt.Errorf("soundness subgate %q observations: %w", subgate.ID, err)
		}
		observations = append(observations, subgateObservations...)
	}
	if err := soundnessevidence.ValidateObservations(registry, observations, expectedBindings); err != nil {
		return Result{}, fmt.Errorf("validate aggregate semantic census: %w", err)
	}
	finalWorkspaceDigest, err := dependencies.workspaceDigest(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("compute final soundness workspace digest: %w", err)
	}
	if finalWorkspaceDigest != initialWorkspaceDigest {
		return Result{}, fmt.Errorf(
			"soundness workspace changed during aggregate execution: initial %s, final %s",
			initialWorkspaceDigest,
			finalWorkspaceDigest,
		)
	}
	runFinishedAt := dependencies.now().UTC()
	slices.SortFunc(observations, func(left, right soundnessevidence.SemanticObservation) int {
		return strings.Compare(left.RegistrationID, right.RegistrationID)
	})
	slices.SortFunc(subgateResults, func(left, right SubgateResult) int {
		return strings.Compare(left.ID, right.ID)
	})
	slices.SortFunc(subgateTelemetry, func(left, right SubgateTelemetry) int {
		return strings.Compare(left.ID, right.ID)
	})
	runReport := RunReport{
		FormatVersion:   RunReportFormatVersion,
		Profile:         profile,
		RunID:           runID,
		WorkspaceDigest: initialWorkspaceDigest,
		ManifestDigest:  manifestDigest,
		Subgates:        subgateResults,
		Observations:    observations,
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
	telemetry := RunTelemetry{
		FormatVersion:           TelemetryFormatVersion,
		RunID:                   runID,
		Profile:                 profile,
		WorkspaceDigest:         initialWorkspaceDigest,
		ManifestDigest:          manifestDigest,
		StartedAt:               runStartedAt,
		FinishedAt:              runFinishedAt,
		WallDurationNanoseconds: runFinishedAt.Sub(runStartedAt).Nanoseconds(),
		CriticalPathNanoseconds: runFinishedAt.Sub(runStartedAt).Nanoseconds(),
		MaxReservedCPUUnits:     maximumReservedCPU(subgateTelemetry),
		MaxReservedMemoryBytes:  maximumReservedMemory(subgateTelemetry),
		Subgates:                subgateTelemetry,
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
		Profile:          profile,
		Registry:         registry,
		Report:           runReport,
		Telemetry:        telemetry,
		RunID:            runID,
		WorkspaceDigest:  initialWorkspaceDigest,
		ManifestDigest:   manifestDigest,
		ReportPath:       retainedReportPath,
		TelemetryPath:    retainedTelemetryPath,
		SubgateCount:     len(selectedSubgates),
		ObservationCount: len(observations),
	}, nil
}

// WorkspaceDigest returns the exact tracked-and-untracked current-tree digest
// used by Run for initial, final, and retained-report freshness checks.
func WorkspaceDigest(ctx context.Context, root string) (string, error) {
	command := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	command.WaitDelay = commandWaitDelay
	command.Env = gitenv.WithoutRepositoryLocal(os.Environ())
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("enumerate tracked and untracked workspace files: %w", err)
	}
	rawPaths := bytes.Split(output, []byte{0})
	paths := make([]string, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		if len(rawPath) != 0 {
			paths = append(paths, string(rawPath))
		}
	}
	slices.Sort(paths)
	entries := make([]workspaceEntry, 0, len(paths))
	for _, path := range paths {
		entry, err := workspaceEntryForPath(root, path)
		if err != nil {
			return "", err
		}
		entries = append(entries, entry)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("encode workspace identity: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}

func resolvePaths(root, manifestPath string) (string, string, error) {
	if strings.TrimSpace(root) == "" {
		return "", "", errors.New("soundness gate root is empty")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve soundness gate root: %w", err)
	}
	rootInfo, err := os.Stat(absoluteRoot)
	if err != nil {
		return "", "", fmt.Errorf("inspect soundness gate root: %w", err)
	}
	if !rootInfo.IsDir() {
		return "", "", fmt.Errorf("soundness gate root %s is not a directory", absoluteRoot)
	}
	if strings.TrimSpace(manifestPath) == "" {
		return "", "", errors.New("soundness gate manifest path is empty")
	}
	absoluteManifest := manifestPath
	if !filepath.IsAbs(absoluteManifest) {
		absoluteManifest = filepath.Join(absoluteRoot, filepath.FromSlash(manifestPath))
	}
	absoluteManifest, err = filepath.Abs(absoluteManifest)
	if err != nil {
		return "", "", fmt.Errorf("resolve soundness gate manifest: %w", err)
	}
	relativeManifest, err := filepath.Rel(absoluteRoot, absoluteManifest)
	if err != nil {
		return "", "", fmt.Errorf("relativize soundness gate manifest: %w", err)
	}
	if relativeManifest == ".." || strings.HasPrefix(relativeManifest, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("soundness gate manifest %s is outside root %s", absoluteManifest, absoluteRoot)
	}
	return absoluteRoot, absoluteManifest, nil
}

func resolveReportPath(root, explicitPath string) (string, error) {
	return resolveExternalArtifactPath(root, explicitPath, EnvReportPath, "aggregate soundness report")
}

func resolveExternalArtifactPath(root, explicitPath, environmentKey, description string) (string, error) {
	artifactPath := explicitPath
	if artifactPath == "" {
		artifactPath = os.Getenv(environmentKey)
	}
	if artifactPath == "" {
		return "", nil
	}
	if !filepath.IsAbs(artifactPath) {
		return "", fmt.Errorf("%s path %q must be absolute", description, artifactPath)
	}
	cleanPath := filepath.Clean(artifactPath)
	relative, err := filepath.Rel(root, cleanPath)
	if err != nil {
		return "", fmt.Errorf("relativize %s path: %w", description, err)
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s path %q must be outside the hashed workspace", description, artifactPath)
	}
	return cleanPath, nil
}

func createSubgateBinding(
	runID string,
	workspaceDigestValue string,
	manifestDigest string,
	subgate Subgate,
) (soundnessevidence.ObservationBinding, error) {
	commandDigest, err := CommandDigest(subgate)
	if err != nil {
		return soundnessevidence.ObservationBinding{}, err
	}
	binding := soundnessevidence.ObservationBinding{
		RunID:           runID,
		WorkspaceDigest: workspaceDigestValue,
		ManifestDigest:  manifestDigest,
		CommandDigest:   commandDigest,
		SubgateID:       subgate.ID,
	}
	if err := binding.Validate(); err != nil {
		return soundnessevidence.ObservationBinding{}, fmt.Errorf("soundness subgate %q binding: %w", subgate.ID, err)
	}
	return binding, nil
}

// dependencyOrderedSubgates returns a deterministic topological order so a
// serial execution always runs producers before their dependents. Ready
// subgates are admitted in ascending ID order; in-profile dependency cycles
// or dangling references fail closed.
func dependencyOrderedSubgates(subgates []Subgate) ([]Subgate, error) {
	byID := make(map[string]Subgate, len(subgates))
	pending := make(map[string][]string, len(subgates))
	for _, subgate := range subgates {
		byID[subgate.ID] = subgate
		pending[subgate.ID] = slices.Clone(subgate.Dependencies)
	}
	ordered := make([]Subgate, 0, len(subgates))
	completed := make(map[string]bool, len(subgates))
	for len(ordered) < len(subgates) {
		ready := make([]string, 0, len(pending))
		for id, requires := range pending {
			satisfied := true
			for _, dependency := range requires {
				if _, selected := byID[dependency]; selected && !completed[dependency] {
					satisfied = false
					break
				}
			}
			if satisfied {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			return nil, errors.New("soundness serial execution found a subgate dependency cycle")
		}
		slices.Sort(ready)
		for _, id := range ready {
			ordered = append(ordered, byID[id])
			completed[id] = true
			delete(pending, id)
		}
	}
	return ordered, nil
}

func subgateEnvironment(
	environment []string,
	binding soundnessevidence.ObservationBinding,
	observationRoot string,
	reportPath string,
	repositoryAuditPath string,
) []string {
	replacements := []struct {
		key   string
		value string
	}{
		{key: soundnessevidence.EnvRunID, value: binding.RunID},
		{key: soundnessevidence.EnvWorkspaceDigest, value: binding.WorkspaceDigest},
		{key: soundnessevidence.EnvManifestDigest, value: binding.ManifestDigest},
		{key: soundnessevidence.EnvCommandDigest, value: binding.CommandDigest},
		{key: soundnessevidence.EnvSubgateID, value: binding.SubgateID},
		{key: soundnessevidence.EnvEvidenceDir, value: observationRoot},
		{key: EnvSubgateReportPath, value: reportPath},
		{key: EnvRepositoryAuditPath, value: repositoryAuditPath},
	}
	result := slices.DeleteFunc(slices.Clone(environment), func(entry string) bool {
		return strings.HasPrefix(entry, EnvReportPath+"=")
	})
	for _, replacement := range replacements {
		prefix := replacement.key + "="
		result = slices.DeleteFunc(result, func(entry string) bool {
			return strings.HasPrefix(entry, prefix)
		})
		result = append(result, prefix+replacement.value)
	}
	return result
}

func workspaceEntryForPath(root, path string) (workspaceEntry, error) {
	absolutePath := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(absolutePath)
	if errors.Is(err, os.ErrNotExist) {
		return workspaceEntry{Path: path, Kind: "deleted"}, nil
	}
	if err != nil {
		return workspaceEntry{}, fmt.Errorf("inspect workspace path %s: %w", path, err)
	}
	entry := workspaceEntry{
		Path: path,
		Mode: uint32(info.Mode().Perm()),
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(absolutePath)
		if err != nil {
			return workspaceEntry{}, fmt.Errorf("read workspace symlink %s: %w", path, err)
		}
		entry.Kind = "symlink"
		entry.Digest = soundnessevidence.DigestBytes([]byte(target))
		return entry, nil
	}
	if !info.Mode().IsRegular() {
		return workspaceEntry{}, fmt.Errorf("workspace path %s is not a regular file or symbolic link", path)
	}
	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return workspaceEntry{}, fmt.Errorf("read workspace path %s: %w", path, err)
	}
	entry.Kind = "regular"
	entry.Digest = soundnessevidence.DigestBytes(data)
	return entry, nil
}

func executeCommand(
	ctx context.Context,
	workingDirectory string,
	commandVector []string,
	environment []string,
	stdout io.Writer,
	stderr io.Writer,
) (commandMetrics, error) {
	command := exec.CommandContext(ctx, commandVector[0], commandVector[1:]...)
	command.Dir = workingDirectory
	command.Env = gitenv.WithoutRepositoryLocal(environment)
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = commandWaitDelay
	if err := command.Run(); err != nil {
		return commandMetrics{}, fmt.Errorf("execute %q: %w", commandVector, err)
	}
	return commandMetrics{
		CPUTimeNanoseconds: command.ProcessState.UserTime().Nanoseconds() + command.ProcessState.SystemTime().Nanoseconds(),
		PeakRSSBytes:       peakRSSBytes(command.ProcessState.SysUsage()),
	}, nil
}

func newRunID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("read cryptographic randomness: %w", err)
	}
	return "run-" + hex.EncodeToString(bytes), nil
}
