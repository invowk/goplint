// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const selectedSubgatesCensusID = "selected-subgates"

const performanceCertificationSubgateID = "benchmarks"

type (
	// PlanOptions configures deterministic execution-plan generation.
	PlanOptions struct {
		Root         string
		ManifestPath string
		Profile      ProfileID
		Resources    ResourceBudget
	}

	planDependencies struct {
		workspaceDigest func(context.Context, string) (string, error)
		binaryDigest    func(context.Context, string, string, string) (string, error)
		toolchain       func() (ToolchainBinding, error)
	}
)

// GeneratePlan creates one immutable canonical plan from the exact current
// workspace and reviewed manifest inputs without executing any work unit.
func GeneratePlan(ctx context.Context, options PlanOptions) (ExecutionPlan, error) {
	dependencies := planDependencies{
		workspaceDigest: WorkspaceDigest,
		binaryDigest:    executableDigest,
		toolchain:       currentToolchainBinding,
	}
	return generatePlan(ctx, options, dependencies)
}

// NormalizeExecutionPlan canonically sorts every set-like plan collection and
// recomputes the self-authenticating plan identity.
func NormalizeExecutionPlan(plan ExecutionPlan) (ExecutionPlan, error) {
	normalized := plan
	normalized.Commands = slices.Clone(plan.Commands)
	for index := range normalized.Commands {
		normalized.Commands[index].Command = slices.Clone(normalized.Commands[index].Command)
		normalized.Commands[index].ExclusivityGroups = slices.Clone(normalized.Commands[index].ExclusivityGroups)
		slices.Sort(normalized.Commands[index].ExclusivityGroups)
	}
	slices.SortFunc(normalized.Commands, comparePlanCommands)
	normalized.Dependencies = slices.Clone(plan.Dependencies)
	for index := range normalized.Dependencies {
		normalized.Dependencies[index].Requires = slices.Clone(normalized.Dependencies[index].Requires)
		slices.Sort(normalized.Dependencies[index].Requires)
	}
	slices.SortFunc(normalized.Dependencies, comparePlanDependencies)
	normalized.Censuses = slices.Clone(plan.Censuses)
	for index := range normalized.Censuses {
		normalized.Censuses[index].MemberIDs = slices.Clone(normalized.Censuses[index].MemberIDs)
		slices.Sort(normalized.Censuses[index].MemberIDs)
		digest, err := planCensusDigest(normalized.Censuses[index])
		if err != nil {
			return ExecutionPlan{}, err
		}
		normalized.Censuses[index].Digest = digest
	}
	slices.SortFunc(normalized.Censuses, comparePlanCensuses)
	normalized.Shards = slices.Clone(plan.Shards)
	for index := range normalized.Shards {
		normalized.Shards[index].MemberIDs = slices.Clone(normalized.Shards[index].MemberIDs)
		slices.Sort(normalized.Shards[index].MemberIDs)
	}
	slices.SortFunc(normalized.Shards, comparePlanShards)
	normalized.ExpectedReports = slices.Clone(plan.ExpectedReports)
	for index := range normalized.ExpectedReports {
		report := &normalized.ExpectedReports[index]
		report.RequiredRegistrationIDs = slices.Clone(report.RequiredRegistrationIDs)
		slices.Sort(report.RequiredRegistrationIDs)
		report.RequiredPopulations = slices.Clone(report.RequiredPopulations)
		slices.SortFunc(report.RequiredPopulations, comparePopulationRequirements)
	}
	slices.SortFunc(normalized.ExpectedReports, comparePlanExpectedReports)
	normalized.PlanID = ""
	planID, err := normalized.CalculateID()
	if err != nil {
		return ExecutionPlan{}, err
	}
	normalized.PlanID = planID
	return normalized, nil
}

// CanonicalPlanJSON returns the normalized indented plan representation.
func CanonicalPlanJSON(plan ExecutionPlan) ([]byte, error) {
	normalized, err := NormalizeExecutionPlan(plan)
	if err != nil {
		return nil, err
	}
	if err := normalized.Validate(); err != nil {
		return nil, fmt.Errorf("validate normalized soundness execution plan: %w", err)
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode canonical soundness execution plan: %w", err)
	}
	return append(data, '\n'), nil
}

// LoadExecutionPlan strictly decodes and validates a retained immutable plan.
func LoadExecutionPlan(ctx context.Context, path string) (ExecutionPlan, error) {
	data, err := readFile(ctx, path)
	if err != nil {
		return ExecutionPlan{}, fmt.Errorf("load soundness execution plan %s: %w", path, err)
	}
	var plan ExecutionPlan
	if err := decodeStrictJSON(data, &plan); err != nil {
		return ExecutionPlan{}, fmt.Errorf("decode soundness execution plan %s: %w", path, err)
	}
	if err := plan.Validate(); err != nil {
		return ExecutionPlan{}, fmt.Errorf("validate soundness execution plan %s: %w", path, err)
	}
	return plan, nil
}

func generatePlan(ctx context.Context, options PlanOptions, dependencies planDependencies) (ExecutionPlan, error) {
	root, manifestPath, err := resolvePaths(options.Root, options.ManifestPath)
	if err != nil {
		return ExecutionPlan{}, err
	}
	manifest, manifestDigest, err := LoadManifest(ctx, manifestPath)
	if err != nil {
		return ExecutionPlan{}, err
	}
	registryPath := filepath.Join(root, filepath.FromSlash(manifest.RegistryPath))
	registry, err := soundnessevidence.LoadRegistry(ctx, registryPath)
	if err != nil {
		return ExecutionPlan{}, fmt.Errorf("load soundness evidence registry %s: %w", registryPath, err)
	}
	if err := manifest.Validate(registry); err != nil {
		return ExecutionPlan{}, err
	}
	profile := options.Profile
	if profile == "" {
		profile = ProfileComplete
	}
	selectedSubgates, err := manifest.SubgatesForProfile(profile)
	if err != nil {
		return ExecutionPlan{}, err
	}
	if err := options.Resources.validate(); err != nil {
		return ExecutionPlan{}, err
	}
	workspaceDigest, err := dependencies.workspaceDigest(ctx, root)
	if err != nil {
		return ExecutionPlan{}, fmt.Errorf("compute soundness plan workspace digest: %w", err)
	}
	registryData, err := readFile(ctx, registryPath)
	if err != nil {
		return ExecutionPlan{}, fmt.Errorf("read soundness plan registry: %w", err)
	}
	toolchain, err := dependencies.toolchain()
	if err != nil {
		return ExecutionPlan{}, err
	}
	manifestRelativePath, err := filepath.Rel(root, manifestPath)
	if err != nil {
		return ExecutionPlan{}, fmt.Errorf("relativize soundness plan manifest: %w", err)
	}
	plan := ExecutionPlan{
		FormatVersion: ExecutionPlanFormatVersion,
		Profile:       profile,
		Workspace: WorkspaceBinding{
			Root:   ".",
			Digest: workspaceDigest,
		},
		Manifest: ArtifactBinding{
			Path:   filepath.ToSlash(manifestRelativePath),
			Digest: manifestDigest,
		},
		Registry: ArtifactBinding{
			Path:   manifest.RegistryPath,
			Digest: soundnessevidence.DigestBytes(registryData),
		},
		Toolchain:       toolchain,
		Resources:       options.Resources,
		Commands:        make([]PlanCommandBinding, 0, len(selectedSubgates)),
		Dependencies:    make([]PlanDependencyBinding, 0, len(selectedSubgates)),
		Censuses:        make([]PlanCensusBinding, 0, 1),
		Shards:          make([]PlanShardBinding, 0, len(selectedSubgates)),
		ExpectedReports: make([]PlanExpectedReportBinding, 0, len(selectedSubgates)),
	}
	selectedIDs := make([]string, 0, len(selectedSubgates))
	selectedSet := make(map[string]bool, len(selectedSubgates))
	for _, subgate := range selectedSubgates {
		selectedIDs = append(selectedIDs, subgate.ID)
		selectedSet[subgate.ID] = true
	}
	for _, subgate := range selectedSubgates {
		if subgate.CPUUnits > options.Resources.CPUUnits || subgate.EstimatedPeakMemoryBytes > options.Resources.MemoryBytes {
			return ExecutionPlan{}, fmt.Errorf("soundness subgate %q requires resources that exceed the plan budget", subgate.ID)
		}
		for _, dependencyID := range subgate.Dependencies {
			if !selectedSet[dependencyID] {
				return ExecutionPlan{}, fmt.Errorf("soundness profile %q selects %q without dependency %q", profile, subgate.ID, dependencyID)
			}
		}
		binaryDigest, err := dependencies.binaryDigest(ctx, root, subgate.WorkingDirectory, subgate.Command[0])
		if err != nil {
			return ExecutionPlan{}, fmt.Errorf("bind soundness subgate %q executable: %w", subgate.ID, err)
		}
		reservation := plannedResourceReservation(subgate, options.Resources)
		command := PlanCommandBinding{
			WorkUnitID:        subgate.ID,
			SubgateID:         subgate.ID,
			WorkingDirectory:  subgate.WorkingDirectory,
			Command:           slices.Clone(subgate.Command),
			BinaryDigest:      binaryDigest,
			ReservedResources: reservation,
			ExclusivityGroups: slices.Clone(subgate.ExclusivityGroups),
			Distributable:     subgate.Distributable,
			TimeoutSeconds:    subgate.TimeoutSeconds,
		}
		command.CommandDigest, err = planCommandDigest(command)
		if err != nil {
			return ExecutionPlan{}, err
		}
		plan.Commands = append(plan.Commands, command)
		plan.Dependencies = append(plan.Dependencies, PlanDependencyBinding{
			WorkUnitID: subgate.ID,
			Requires:   slices.Clone(subgate.Dependencies),
		})
		plan.Shards = append(plan.Shards, PlanShardBinding{
			ID:             subgate.ID,
			WorkUnitID:     subgate.ID,
			CensusID:       selectedSubgatesCensusID,
			Mode:           "subgate",
			Iteration:      1,
			MemberIDs:      []string{subgate.ID},
			TotalWeight:    int64(reservation.CPUUnits),
			TimeoutSeconds: subgate.TimeoutSeconds,
		})
		plan.ExpectedReports = append(plan.ExpectedReports, PlanExpectedReportBinding{
			WorkUnitID:              subgate.ID,
			SubgateID:               subgate.ID,
			ReportFile:              subgate.ReportFile,
			CommandDigest:           command.CommandDigest,
			RequiredRegistrationIDs: slices.Clone(subgate.RequiredRegistrationIDs),
			RequiredPopulations:     slices.Clone(subgate.RequiredPopulations),
		})
	}
	plan.Censuses = append(plan.Censuses, PlanCensusBinding{
		ID:        selectedSubgatesCensusID,
		Kind:      "subgates",
		MemberIDs: selectedIDs,
	})
	normalized, err := NormalizeExecutionPlan(plan)
	if err != nil {
		return ExecutionPlan{}, err
	}
	if err := normalized.Validate(); err != nil {
		return ExecutionPlan{}, fmt.Errorf("validate generated soundness execution plan: %w", err)
	}
	return normalized, nil
}

func plannedResourceReservation(subgate Subgate, budget ResourceBudget) ResourceReservation {
	cpuUnits := subgate.CPUUnits
	if subgate.ID == performanceCertificationSubgateID {
		// The short algorithmic certification thresholds are runner-class
		// calibrated, so they execute without local contention. The separately
		// planned full-scan certification retains its reviewed four-CPU weight.
		// Analyzer race/repeat capacity is expressed as deterministic
		// four-CPU work groups, so no subgate needs a larger local carve-out.
		cpuUnits = budget.CPUUnits
	}
	return ResourceReservation{
		CPUUnits: cpuUnits, MemoryBytes: subgate.EstimatedPeakMemoryBytes, WorkerSlots: 1,
	}
}

func executableDigest(ctx context.Context, root, workingDirectory, executable string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("resolve executable before I/O: %w", err)
	}
	var path string
	if strings.Contains(executable, "/") {
		path = filepath.Join(root, filepath.FromSlash(workingDirectory), filepath.FromSlash(executable))
	} else {
		resolved, err := exec.LookPath(executable)
		if err != nil {
			return "", fmt.Errorf("resolve executable %q: %w", executable, err)
		}
		path = resolved
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read executable %s: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("resolve executable after I/O: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}

func currentToolchainBinding() (ToolchainBinding, error) {
	binding := ToolchainBinding{
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
	digest, err := toolchainDigest(binding)
	if err != nil {
		return ToolchainBinding{}, err
	}
	binding.Digest = digest
	return binding, nil
}

func planCommandDigest(command PlanCommandBinding) (string, error) {
	return CommandDigest(Subgate{
		ID:               command.SubgateID,
		WorkingDirectory: command.WorkingDirectory,
		Command:          command.Command,
	})
}

func planCensusDigest(census PlanCensusBinding) (string, error) {
	return calculateBindingDigest(struct {
		ID        string   `json:"id"`
		Kind      string   `json:"kind"`
		MemberIDs []string `json:"member_ids"`
	}{ID: census.ID, Kind: census.Kind, MemberIDs: census.MemberIDs})
}

func toolchainDigest(binding ToolchainBinding) (string, error) {
	return calculateBindingDigest(struct {
		GoVersion string `json:"go_version"`
		GOOS      string `json:"goos"`
		GOARCH    string `json:"goarch"`
	}{GoVersion: binding.GoVersion, GOOS: binding.GOOS, GOARCH: binding.GOARCH})
}

func comparePlanCommands(left, right PlanCommandBinding) int {
	return strings.Compare(left.WorkUnitID, right.WorkUnitID)
}

func comparePlanDependencies(left, right PlanDependencyBinding) int {
	return strings.Compare(left.WorkUnitID, right.WorkUnitID)
}

func comparePlanCensuses(left, right PlanCensusBinding) int {
	return strings.Compare(left.ID, right.ID)
}

func comparePlanShards(left, right PlanShardBinding) int {
	return strings.Compare(left.ID, right.ID)
}

func comparePlanExpectedReports(left, right PlanExpectedReportBinding) int {
	return strings.Compare(left.WorkUnitID, right.WorkUnitID)
}

func comparePopulationRequirements(left, right PopulationRequirement) int {
	return strings.Compare(left.ID, right.ID)
}
