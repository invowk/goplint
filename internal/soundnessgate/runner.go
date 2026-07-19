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
	"slices"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const commandWaitDelay = 10 * time.Second

type (
	// Options configures one aggregate soundness run.
	Options struct {
		Root         string
		ManifestPath string
		Profile      ProfileID
		ReportPath   string
		Stdout       io.Writer
		Stderr       io.Writer
	}

	// Result identifies the exact workspace, manifest, and observations accepted
	// by a successful aggregate run.
	Result struct {
		Profile          ProfileID
		Registry         soundnessevidence.Registry
		Report           RunReport
		RunID            string
		WorkspaceDigest  string
		ManifestDigest   string
		ReportPath       string
		SubgateCount     int
		ObservationCount int
	}

	runnerDependencies struct {
		workspaceDigest func(context.Context, string) (string, error)
		execute         func(context.Context, string, []string, []string, io.Writer, io.Writer) error
		makeTempDir     func(string, string) (string, error)
		newRunID        func() (string, error)
	}

	workspaceEntry struct {
		Path   string `json:"path"`
		Kind   string `json:"kind"`
		Mode   uint32 `json:"mode"`
		Digest string `json:"digest"`
	}
)

// Run executes every reviewed manifest command in a fresh output root and
// validates command-bound reports plus the bidirectional semantic census.
func Run(ctx context.Context, options Options) (Result, error) {
	dependencies := runnerDependencies{
		workspaceDigest: WorkspaceDigest,
		execute:         executeCommand,
		makeTempDir:     os.MkdirTemp,
		newRunID:        newRunID,
	}
	return run(ctx, options, dependencies)
}

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
	for index := range selectedSubgates {
		subgate := selectedSubgates[index]
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
		environment := subgateEnvironment(os.Environ(), binding, observationRoot, reportPath)
		workingDirectory := filepath.Join(root, filepath.FromSlash(subgate.WorkingDirectory))
		commandCtx, cancel := context.WithTimeout(ctx, time.Duration(subgate.TimeoutSeconds)*time.Second)
		err = dependencies.execute(commandCtx, workingDirectory, subgate.Command, environment, stdout, stderr)
		commandErr := commandCtx.Err()
		cancel()
		if commandErr != nil {
			return Result{}, fmt.Errorf("soundness subgate %q timed out or was canceled; no evidence accepted: %w", subgate.ID, commandErr)
		}
		if err != nil {
			return Result{}, fmt.Errorf("soundness subgate %q command failed; no evidence accepted: %w", subgate.ID, err)
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
	slices.SortFunc(observations, func(left, right soundnessevidence.SemanticObservation) int {
		return strings.Compare(left.RegistrationID, right.RegistrationID)
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
	return Result{
		Profile:          profile,
		Registry:         registry,
		Report:           runReport,
		RunID:            runID,
		WorkspaceDigest:  initialWorkspaceDigest,
		ManifestDigest:   manifestDigest,
		ReportPath:       retainedReportPath,
		SubgateCount:     len(selectedSubgates),
		ObservationCount: len(observations),
	}, nil
}

// WorkspaceDigest returns the exact tracked-and-untracked current-tree digest
// used by Run for initial, final, and retained-report freshness checks.
func WorkspaceDigest(ctx context.Context, root string) (string, error) {
	command := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	command.WaitDelay = commandWaitDelay
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
	reportPath := explicitPath
	if reportPath == "" {
		reportPath = os.Getenv(EnvReportPath)
	}
	if reportPath == "" {
		return "", nil
	}
	if !filepath.IsAbs(reportPath) {
		return "", fmt.Errorf("aggregate soundness report path %q must be absolute", reportPath)
	}
	cleanPath := filepath.Clean(reportPath)
	relative, err := filepath.Rel(root, cleanPath)
	if err != nil {
		return "", fmt.Errorf("relativize aggregate soundness report path: %w", err)
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("aggregate soundness report path %q must be outside the hashed workspace", reportPath)
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

func subgateEnvironment(
	environment []string,
	binding soundnessevidence.ObservationBinding,
	observationRoot string,
	reportPath string,
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
) error {
	command := exec.CommandContext(ctx, commandVector[0], commandVector[1:]...)
	command.Dir = workingDirectory
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = commandWaitDelay
	if err := command.Run(); err != nil {
		return fmt.Errorf("execute %q: %w", commandVector, err)
	}
	return nil
}

func newRunID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("read cryptographic randomness: %w", err)
	}
	return "run-" + hex.EncodeToString(bytes), nil
}
