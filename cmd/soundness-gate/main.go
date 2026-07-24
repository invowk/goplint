// SPDX-License-Identifier: MPL-2.0

// Command soundness-gate executes the canonical aggregate goplint soundness
// manifest and validates its fresh evidence.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/invowk/invowk/tools/goplint/goplint"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func main() {
	root := flag.String("root", "../..", "repository root")
	manifest := flag.String("manifest", "tools/goplint/spec/soundness-gate.v1.json", "root-relative aggregate manifest")
	profile := flag.String("profile", string(soundnessgate.ProfileSemantic), "reviewed manifest profile: consumer, semantic, or complete")
	telemetry := flag.String("telemetry", "", "absolute path outside the workspace for versioned execution telemetry")
	executor := flag.String("executor", executorDefault(), "executor: plan-serial or parallel")
	cpuBudget := flag.Int("cpu-budget", 0, "positive CPU-unit override (default: environment or effective GOMAXPROCS)")
	memoryBudget := flag.Int64("memory-budget-bytes", 0, "positive memory-byte override (default: environment or available memory with headroom)")
	maximumWorkers := flag.Int("max-workers", 0, "positive worker-count override (default: environment or CPU budget)")
	runnerClass := flag.String("runner-class", "", "reviewed runner-class override")
	action := flag.String("action", "run", "action: run, plan, work, or aggregate")
	planPath := flag.String("plan", "", "immutable execution-plan input or output path")
	workUnit := flag.String("work-unit", "", "assigned work-unit identity")
	bundleDirectory := flag.String("bundle-dir", "", "distributed bundle output or aggregate input directory")
	repositoryAudit := flag.String("repository-audit", "", "repository-audit artifact required by dependent distributed work")
	reportPath := flag.String("report", "", "retained aggregate report output")
	flag.Parse()
	ctx := context.Background()
	if *action != "run" {
		if err := executeDistributed(ctx, distributedOptions{
			root: *root, manifest: *manifest, profile: soundnessgate.ProfileID(*profile),
			action: *action, planPath: *planPath, workUnit: *workUnit,
			bundleDirectory: *bundleDirectory, repositoryAudit: *repositoryAudit,
			reportPath: *reportPath, telemetryPath: *telemetry,
			cpuBudget: *cpuBudget, memoryBudget: *memoryBudget, maximumWorkers: *maximumWorkers,
			runnerClass: *runnerClass,
		}); err != nil {
			fmt.Fprintln(os.Stderr, "goplint soundness gate:", err)
			os.Exit(1)
		}
		return
	}
	result, err := execute(ctx, executeOptions{
		root: *root, manifest: *manifest, profile: soundnessgate.ProfileID(*profile),
		telemetry: *telemetry, report: *reportPath,
		executor: *executor, cpuBudget: *cpuBudget, memoryBudget: *memoryBudget, maximumWorkers: *maximumWorkers,
		runnerClass: *runnerClass,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "goplint soundness gate:", err)
		os.Exit(1)
	}
	var census bytes.Buffer
	if err := goplint.WriteSemanticCoverageCensus(&census, result.Registry, result.Report.Observations); err != nil {
		fmt.Fprintln(os.Stderr, "goplint soundness gate: semantic census:", err)
		os.Exit(1)
	}
	if _, err := census.WriteTo(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "goplint soundness gate: write semantic census:", err)
		os.Exit(1)
	}
	summary := struct {
		Profile          soundnessgate.ProfileID `json:"profile"`
		RunID            string                  `json:"run_id"`
		WorkspaceDigest  string                  `json:"workspace_digest"`
		ManifestDigest   string                  `json:"manifest_digest"`
		SubgateCount     int                     `json:"subgate_count"`
		ObservationCount int                     `json:"observation_count"`
	}{
		Profile:          result.Profile,
		RunID:            result.RunID,
		WorkspaceDigest:  result.WorkspaceDigest,
		ManifestDigest:   result.ManifestDigest,
		SubgateCount:     result.SubgateCount,
		ObservationCount: result.ObservationCount,
	}
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(summary); err != nil {
		fmt.Fprintln(os.Stderr, "goplint soundness gate: encode summary:", err)
		os.Exit(1)
	}
}

type distributedOptions struct {
	root, manifest, action, planPath, workUnit   string
	bundleDirectory, repositoryAudit, reportPath string
	telemetryPath                                string
	profile                                      soundnessgate.ProfileID
	cpuBudget, maximumWorkers                    int
	memoryBudget                                 int64
	runnerClass                                  string
}

func executeDistributed(ctx context.Context, options distributedOptions) error {
	switch options.action {
	case "plan":
		if options.planPath == "" {
			return errors.New("plan action requires -plan output")
		}
		budget, err := soundnessgate.DiscoverResourceBudget(soundnessgate.ResourceOverrides{
			CPUUnits: options.cpuBudget, MemoryBytes: options.memoryBudget, MaximumWorkers: options.maximumWorkers,
			RunnerClass: options.runnerClass,
		})
		if err != nil {
			return fmt.Errorf("discover distributed plan resource budget: %w", err)
		}
		plan, err := soundnessgate.GeneratePlan(ctx, soundnessgate.PlanOptions{
			Root: options.root, ManifestPath: options.manifest, Profile: options.profile, Resources: budget,
		})
		if err != nil {
			return fmt.Errorf("generate distributed soundness plan: %w", err)
		}
		data, err := soundnessgate.CanonicalPlanJSON(plan)
		if err != nil {
			return fmt.Errorf("encode canonical distributed soundness plan: %w", err)
		}
		if err := publishFile(options.planPath, data); err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			PlanID    string   `json:"plan_id"`
			WorkUnits []string `json:"work_units"`
		}{PlanID: plan.PlanID, WorkUnits: planWorkUnitIDs(plan)}); err != nil {
			return fmt.Errorf("encode distributed plan summary: %w", err)
		}
		return nil
	case "work":
		if options.planPath == "" || options.workUnit == "" || options.bundleDirectory == "" {
			return errors.New("work action requires -plan, -work-unit, and -bundle-dir")
		}
		plan, err := soundnessgate.LoadExecutionPlan(ctx, options.planPath)
		if err != nil {
			return fmt.Errorf("load distributed soundness plan: %w", err)
		}
		bundle, err := soundnessgate.ExecutePlanWorkUnit(ctx, plan, options.workUnit, soundnessgate.DistributedWorkOptions{
			Root: options.root, OutputDirectory: options.bundleDirectory,
			RepositoryAuditInputPath: options.repositoryAudit, Stdout: os.Stdout, Stderr: os.Stderr,
		})
		if err != nil {
			return fmt.Errorf("execute distributed work unit %q: %w", options.workUnit, err)
		}
		data, err := soundnessgate.CanonicalWorkBundleJSON(bundle, plan)
		if err != nil {
			return fmt.Errorf("encode canonical work bundle %q: %w", options.workUnit, err)
		}
		return publishFile(filepath.Join(options.bundleDirectory, options.workUnit+".bundle.json"), data)
	case "aggregate":
		if options.planPath == "" || options.bundleDirectory == "" {
			return errors.New("aggregate action requires -plan and -bundle-dir")
		}
		plan, err := soundnessgate.LoadExecutionPlan(ctx, options.planPath)
		if err != nil {
			return fmt.Errorf("load aggregate soundness plan: %w", err)
		}
		bundlePaths, err := discoverBundles(options.bundleDirectory)
		if err != nil {
			return err
		}
		report, err := soundnessgate.AggregateWorkBundles(ctx, plan, soundnessgate.AggregateBundleOptions{
			Root: options.root, BundlePaths: bundlePaths,
			RepositoryAuditPath: options.repositoryAudit,
			ReportPath:          options.reportPath, TelemetryPath: options.telemetryPath,
		})
		if err != nil {
			return fmt.Errorf("aggregate distributed work bundles: %w", err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			RunID        string `json:"run_id"`
			SubgateCount int    `json:"subgate_count"`
		}{RunID: report.RunID, SubgateCount: len(report.Subgates)}); err != nil {
			return fmt.Errorf("encode aggregate soundness summary: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("action %q is invalid; want run, plan, work, or aggregate", options.action)
	}
}

func planWorkUnitIDs(plan soundnessgate.ExecutionPlan) []string {
	ids := make([]string, 0, len(plan.Commands))
	for _, command := range plan.Commands {
		ids = append(ids, command.WorkUnitID)
	}
	return ids
}

func discoverBundles(root string) ([]string, error) {
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("distributed bundle path %s is a symbolic link", path)
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".bundle.json") {
			paths = append(paths, path)
		}
		return nil
	})
	slices.Sort(paths)
	if err != nil {
		return nil, fmt.Errorf("walk distributed bundle directory %q: %w", root, err)
	}
	return paths, nil
}

func publishFile(path string, data []byte) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("distributed output path %q must be absolute", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create distributed output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".soundness-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary distributed output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath) //nolint:errcheck // Best-effort private temporary cleanup.
	if _, err := temporary.Write(data); err != nil {
		temporary.Close() //nolint:errcheck // Preserve the primary write error.
		return fmt.Errorf("write temporary distributed output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary distributed output: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish distributed output: %w", err)
	}
	return nil
}

type executeOptions struct {
	root           string
	manifest       string
	profile        soundnessgate.ProfileID
	telemetry      string
	report         string
	executor       string
	cpuBudget      int
	memoryBudget   int64
	maximumWorkers int
	runnerClass    string
}

func execute(ctx context.Context, options executeOptions) (soundnessgate.Result, error) {
	switch options.executor {
	case "plan-serial", "parallel":
		budget, err := soundnessgate.DiscoverResourceBudget(soundnessgate.ResourceOverrides{
			CPUUnits: options.cpuBudget, MemoryBytes: options.memoryBudget,
			MaximumWorkers: options.maximumWorkers, RunnerClass: options.runnerClass,
		})
		if err != nil {
			return soundnessgate.Result{}, fmt.Errorf("discover soundness resource budget: %w", err)
		}
		budget.SerialReference = options.executor == "plan-serial"
		plan, err := soundnessgate.GeneratePlan(ctx, soundnessgate.PlanOptions{
			Root: options.root, ManifestPath: options.manifest, Profile: options.profile, Resources: budget,
		})
		if err != nil {
			return soundnessgate.Result{}, fmt.Errorf("generate soundness execution plan: %w", err)
		}
		if options.executor == "plan-serial" {
			result, err := soundnessgate.RunPlanSerial(ctx, plan, soundnessgate.PlanSerialOptions{
				Root: options.root, TelemetryPath: options.telemetry, ReportPath: options.report,
				Stdout: os.Stdout, Stderr: os.Stderr,
			})
			if err != nil {
				return soundnessgate.Result{}, fmt.Errorf("run serial soundness plan: %w", err)
			}
			return result, nil
		}
		result, err := soundnessgate.RunPlanParallel(ctx, plan, soundnessgate.PlanParallelOptions{
			Root: options.root, TelemetryPath: options.telemetry, ReportPath: options.report,
			Stdout: os.Stdout, Stderr: os.Stderr,
		})
		if err != nil {
			return soundnessgate.Result{}, fmt.Errorf("run parallel soundness plan: %w", err)
		}
		return result, nil
	default:
		return soundnessgate.Result{}, fmt.Errorf("executor %q is invalid; want plan-serial or parallel", options.executor)
	}
}

func executorDefault() string {
	if value := os.Getenv("GOPLINT_SOUNDNESS_EXECUTOR"); value != "" {
		return value
	}
	return "parallel"
}
