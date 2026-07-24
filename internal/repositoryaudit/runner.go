// SPDX-License-Identifier: MPL-2.0

package repositoryaudit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/goplint"
	"github.com/invowk/invowk/tools/goplint/internal/gitenv"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

const analyzerWaitDelay = 10 * time.Second

type (
	// RunOptions configures one canonical superset analyzer traversal.
	RunOptions struct {
		Root                 string
		AnalyzerPath         string
		BaselinePath         string
		ExceptionsPath       string
		SemanticManifestPath string
		PackagePatterns      []string
		WorkspaceDigest      string
		CachePolicy          string
		OutputPath           string
	}

	analyzerExecution struct {
		Stdout       []byte
		Stderr       []byte
		ExitCode     int
		PeakRSSBytes int64
	}

	runnerDependencies struct {
		execute         func(context.Context, string, string, []string) (analyzerExecution, error)
		now             func() time.Time
		workspaceDigest func(context.Context, string) (string, error)
	}
)

// Run performs exactly one canonical analyzer traversal and publishes all
// read-only full-scan, baseline, and stale-exception inputs in one artifact.
func Run(ctx context.Context, options RunOptions) (Result, error) {
	return run(ctx, options, runnerDependencies{
		execute: executeAnalyzer, now: time.Now, workspaceDigest: soundnessgate.WorkspaceDigest,
	})
}

// CurrentInputBinding computes the exact non-package inputs a read-only
// consumer must match before reusing a retained repository audit.
func CurrentInputBinding(options RunOptions) (InputBinding, error) {
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return InputBinding{}, fmt.Errorf("resolve repository audit root: %w", err)
	}
	analyzerPath, err := resolveRequiredPath(root, options.AnalyzerPath)
	if err != nil {
		return InputBinding{}, err
	}
	baselinePath, err := resolveRequiredPath(root, options.BaselinePath)
	if err != nil {
		return InputBinding{}, err
	}
	exceptionsPath, err := resolveRequiredPath(root, options.ExceptionsPath)
	if err != nil {
		return InputBinding{}, err
	}
	manifestPath, err := resolveRequiredPath(root, options.SemanticManifestPath)
	if err != nil {
		return InputBinding{}, err
	}
	return buildInputBinding(
		options, analyzerPath, baselinePath, exceptionsPath, manifestPath,
		canonicalAnalyzerFlags(options.ExceptionsPath), canonicalStrings(options.PackagePatterns),
	)
}

func run(ctx context.Context, options RunOptions, dependencies runnerDependencies) (Result, error) {
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve repository audit root: %w", err)
	}
	if options.WorkspaceDigest == "" {
		return Result{}, errors.New("repository audit workspace digest is empty")
	}
	observedWorkspaceDigest, err := dependencies.workspaceDigest(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("compute repository audit initial workspace digest: %w", err)
	}
	if observedWorkspaceDigest != options.WorkspaceDigest {
		return Result{}, errors.New("repository audit requested workspace digest does not match the current tree")
	}
	packagePatterns := canonicalStrings(options.PackagePatterns)
	if len(packagePatterns) == 0 {
		return Result{}, errors.New("repository audit package patterns are empty")
	}
	analyzerPath, err := resolveRequiredPath(root, options.AnalyzerPath)
	if err != nil {
		return Result{}, err
	}
	baselinePath, err := resolveRequiredPath(root, options.BaselinePath)
	if err != nil {
		return Result{}, err
	}
	exceptionsPath, err := resolveRequiredPath(root, options.ExceptionsPath)
	if err != nil {
		return Result{}, err
	}
	semanticManifestPath, err := resolveRequiredPath(root, options.SemanticManifestPath)
	if err != nil {
		return Result{}, err
	}
	baseline, err := goplint.LoadBaseline(baselinePath)
	if err != nil {
		return Result{}, fmt.Errorf("load repository audit baseline: %w", err)
	}
	exceptions, err := goplint.LoadExceptionConfig(exceptionsPath)
	if err != nil {
		return Result{}, fmt.Errorf("load repository audit exceptions: %w", err)
	}
	temporaryDirectory, err := os.MkdirTemp("", "goplint-repository-audit-")
	if err != nil {
		return Result{}, fmt.Errorf("create repository audit temporary directory: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory) //nolint:errcheck // Private analyzer-output cleanup is intentionally best effort.
	findingsPath := filepath.Join(temporaryDirectory, "findings.jsonl")
	flags := canonicalAnalyzerFlags(options.ExceptionsPath)
	actualArgs := slices.Clone(flags)
	for index := range actualArgs {
		if actualArgs[index] == "-emit-findings-jsonl=<private>" {
			actualArgs[index] = "-emit-findings-jsonl=" + findingsPath
		}
	}
	actualArgs = append(actualArgs, packagePatterns...)
	startedAt := dependencies.now().UTC()
	execution, executeErr := dependencies.execute(ctx, root, analyzerPath, actualArgs)
	finishedAt := dependencies.now().UTC()
	if executeErr != nil {
		return Result{}, executeErr
	}
	if execution.ExitCode != 0 && execution.ExitCode != 3 {
		return Result{}, fmt.Errorf("canonical repository analyzer exited with unexpected status %d: %s", execution.ExitCode, execution.Stderr)
	}
	findingsData, err := os.ReadFile(findingsPath)
	if err != nil {
		return Result{}, fmt.Errorf("read repository audit findings stream: %w", err)
	}
	if err := goplint.ValidateFindingStreamCoverage(findingsData, execution.Stdout); err != nil {
		return Result{}, fmt.Errorf("validate repository audit analyzer output: %w", err)
	}
	records, err := goplint.ParseFindingStream(findingsData)
	if err != nil {
		return Result{}, fmt.Errorf("parse repository audit finding stream: %w", err)
	}
	packageIDs, err := goplint.AnalysisPackageCensus(execution.Stdout)
	if err != nil {
		return Result{}, fmt.Errorf("read repository audit package census: %w", err)
	}
	if len(packageIDs) == 0 {
		return Result{}, errors.New("repository audit analyzer output has an empty package census")
	}
	stalePatterns, _, _, err := goplint.CollectGlobalStaleExceptionPatternsFromStreams(execution.Stdout, findingsData)
	if err != nil {
		return Result{}, fmt.Errorf("derive repository audit stale exceptions: %w", err)
	}
	inputs, err := buildInputBinding(
		options, analyzerPath, baselinePath, exceptionsPath, semanticManifestPath, flags, packagePatterns,
	)
	if err != nil {
		return Result{}, err
	}
	result, err := Build(BuildOptions{
		Inputs: inputs, Records: records, Baseline: baseline, Exceptions: exceptions,
		StalePatterns: stalePatterns, PackageIDs: packageIDs, WorkspaceRoot: root,
		Scan: ScanMetadata{
			StartedAt: startedAt, FinishedAt: finishedAt,
			WallDurationNanoseconds: finishedAt.Sub(startedAt).Nanoseconds(),
			PeakRSSBytes:            execution.PeakRSSBytes,
			ExitCode:                execution.ExitCode,
		},
	})
	if err != nil {
		return Result{}, err
	}
	finalWorkspaceDigest, err := dependencies.workspaceDigest(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("compute repository audit final workspace digest: %w", err)
	}
	if finalWorkspaceDigest != options.WorkspaceDigest {
		return Result{}, errors.New("repository audit workspace changed during the analyzer traversal")
	}
	if options.OutputPath != "" {
		if !filepath.IsAbs(options.OutputPath) {
			return Result{}, errors.New("repository audit output path must be absolute")
		}
		relativeOutput, relativeErr := filepath.Rel(root, options.OutputPath)
		if relativeErr != nil {
			return Result{}, fmt.Errorf("resolve repository audit output relative to workspace: %w", relativeErr)
		}
		if relativeOutput != ".." && !strings.HasPrefix(relativeOutput, ".."+string(filepath.Separator)) {
			return Result{}, errors.New("repository audit output path must be outside the hashed workspace")
		}
		if err := WriteExclusive(ctx, options.OutputPath, result); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func canonicalAnalyzerFlags(exceptionsPath string) []string {
	return []string{
		"-audit-exceptions", "-check-all", "-check-enum-sync", "-json", "-test=false",
		"-config=" + filepath.ToSlash(exceptionsPath),
		"-emit-findings-jsonl=<private>",
	}
}

func buildInputBinding(
	options RunOptions,
	analyzerPath, baselinePath, exceptionsPath, semanticManifestPath string,
	flags, packagePatterns []string,
) (InputBinding, error) {
	analyzerDigest, err := fileDigest(analyzerPath)
	if err != nil {
		return InputBinding{}, err
	}
	baselineDigest, err := fileDigest(baselinePath)
	if err != nil {
		return InputBinding{}, err
	}
	exceptionsDigest, err := fileDigest(exceptionsPath)
	if err != nil {
		return InputBinding{}, err
	}
	semanticManifestDigest, err := fileDigest(semanticManifestPath)
	if err != nil {
		return InputBinding{}, err
	}
	toolchainDigest, err := digestValue(struct {
		GoVersion string `json:"go_version"`
		GOOS      string `json:"goos"`
		GOARCH    string `json:"goarch"`
	}{GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH})
	if err != nil {
		return InputBinding{}, err
	}
	commandDigest, err := digestValue(struct {
		AnalyzerDigest  string   `json:"analyzer_digest"`
		Flags           []string `json:"flags"`
		PackagePatterns []string `json:"package_patterns"`
	}{AnalyzerDigest: analyzerDigest, Flags: canonicalStrings(flags), PackagePatterns: packagePatterns})
	if err != nil {
		return InputBinding{}, err
	}
	cachePolicy := options.CachePolicy
	if cachePolicy == "" {
		cachePolicy = "inherited"
	}
	return InputBinding{
		WorkspaceDigest: options.WorkspaceDigest, AnalyzerDigest: analyzerDigest,
		BaselineDigest: baselineDigest, ExceptionsDigest: exceptionsDigest,
		SemanticManifestDigest: semanticManifestDigest, ToolchainDigest: toolchainDigest,
		CommandDigest: commandDigest, AnalyzerMode: "check-all+enum-sync+audit-exceptions",
		Flags: canonicalStrings(flags), PackagePatterns: packagePatterns,
		CachePolicy: cachePolicy, Purpose: canonicalPurpose,
	}, nil
}

func executeAnalyzer(
	ctx context.Context,
	root, analyzerPath string,
	arguments []string,
) (analyzerExecution, error) {
	command := exec.CommandContext(ctx, analyzerPath, arguments...)
	command.Dir = root
	command.Env = gitenv.WithoutRepositoryLocal(os.Environ())
	command.WaitDelay = analyzerWaitDelay
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		exitError, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			return analyzerExecution{}, fmt.Errorf("execute canonical repository analyzer: %w", err)
		}
		exitCode = exitError.ExitCode()
		if len(bytes.TrimSpace(stdout.Bytes())) == 0 {
			return analyzerExecution{}, fmt.Errorf("canonical repository analyzer failed with exit %d and no machine output: %w: %s", exitCode, err, stderr.Bytes())
		}
	}
	return analyzerExecution{
		Stdout: slices.Clone(stdout.Bytes()), Stderr: slices.Clone(stderr.Bytes()), ExitCode: exitCode,
		PeakRSSBytes: peakRSSBytes(command.ProcessState.SysUsage()),
	}, nil
}

func resolveRequiredPath(root, path string) (string, error) {
	if path == "" {
		return "", errors.New("repository audit required path is empty")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(path))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve repository audit input path: %w", err)
	}
	if _, err := os.Stat(absolute); err != nil {
		return "", fmt.Errorf("inspect repository audit input %s: %w", absolute, err)
	}
	return absolute, nil
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read repository audit input %s: %w", path, err)
	}
	return soundnessevidence.DigestBytes(data), nil
}
