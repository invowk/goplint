// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const (
	// WorkBundleFormatVersion is the supported distributed result format.
	WorkBundleFormatVersion = 2
	distributedStatusPassed = "passed"
)

type (
	// WorkBundle is one self-authenticating distributed work result.
	WorkBundle struct {
		FormatVersion               int                                     `json:"format_version"`
		BundleID                    string                                  `json:"bundle_id"`
		PlanID                      string                                  `json:"plan_id"`
		WorkUnitID                  string                                  `json:"work_unit_id"`
		WorkspaceDigest             string                                  `json:"workspace_digest"`
		ManifestDigest              string                                  `json:"manifest_digest"`
		RegistryDigest              string                                  `json:"registry_digest"`
		ToolchainDigest             string                                  `json:"toolchain_digest"`
		CommandDigest               string                                  `json:"command_digest"`
		BinaryDigest                string                                  `json:"binary_digest"`
		InputRepositoryAuditDigest  string                                  `json:"input_repository_audit_digest,omitempty"`
		OutputRepositoryAuditDigest string                                  `json:"output_repository_audit_digest,omitempty"`
		TerminalStatus              string                                  `json:"terminal_status"`
		Binding                     soundnessevidence.ObservationBinding    `json:"binding"`
		Report                      Report                                  `json:"report"`
		ReportDigest                string                                  `json:"report_digest"`
		Observations                []soundnessevidence.SemanticObservation `json:"observations"`
		QueuedAt                    time.Time                               `json:"queued_at"`
		StartedAt                   time.Time                               `json:"started_at"`
		FinishedAt                  time.Time                               `json:"finished_at"`
		WallDurationNanoseconds     int64                                   `json:"wall_duration_nanoseconds"`
		CPUTimeNanoseconds          int64                                   `json:"cpu_time_nanoseconds"`
		PeakRSSBytes                int64                                   `json:"peak_rss_bytes"`
	}

	// DistributedWorkOptions configures execution of one assigned work unit.
	DistributedWorkOptions struct {
		Root                     string
		OutputDirectory          string
		RepositoryAuditInputPath string
		Stdout                   io.Writer
		Stderr                   io.Writer
	}

	// AggregateBundleOptions configures strict distributed aggregation.
	AggregateBundleOptions struct {
		Root                string
		BundlePaths         []string
		RepositoryAuditPath string
		ReportPath          string
		TelemetryPath       string
	}

	// distributedWorkDependencies makes the real work-unit execution path
	// testable without weakening its exact-plan validation.
	distributedWorkDependencies struct {
		plan     planDependencies
		execute  func(context.Context, string, []string, []string, io.Writer, io.Writer) (commandMetrics, error)
		newRunID func() (string, error)
	}
)

// ExecutePlanWorkUnit validates the exact plan and executes one assigned unit.
func ExecutePlanWorkUnit(
	ctx context.Context,
	plan ExecutionPlan,
	workUnitID string,
	options DistributedWorkOptions,
) (WorkBundle, error) {
	return executePlanWorkUnit(ctx, plan, workUnitID, options, distributedWorkDependencies{
		plan: planDependencies{
			workspaceDigest: WorkspaceDigest, binaryDigest: executableDigest, toolchain: currentToolchainBinding,
		},
		execute:  executeCommand,
		newRunID: newRunID,
	})
}

func executePlanWorkUnit(
	ctx context.Context,
	plan ExecutionPlan,
	workUnitID string,
	options DistributedWorkOptions,
	dependencies distributedWorkDependencies,
) (WorkBundle, error) {
	if err := validateCurrentPlan(ctx, plan, options.Root, dependencies.plan); err != nil {
		return WorkBundle{}, err
	}
	command, expectedReport, dependency, err := distributedBindings(plan, workUnitID)
	if err != nil {
		return WorkBundle{}, err
	}
	root, manifestPath, err := resolvePaths(options.Root, plan.Manifest.Path)
	if err != nil {
		return WorkBundle{}, err
	}
	manifest, manifestDigest, err := LoadManifest(ctx, manifestPath)
	if err != nil {
		return WorkBundle{}, err
	}
	registry, err := soundnessevidence.LoadRegistry(ctx, filepath.Join(root, filepath.FromSlash(plan.Registry.Path)))
	if err != nil {
		return WorkBundle{}, fmt.Errorf("load distributed soundness registry: %w", err)
	}
	if err := manifest.Validate(registry); err != nil {
		return WorkBundle{}, err
	}
	if err := requireOutsideWorkspace(root, options.OutputDirectory, "distributed work output"); err != nil {
		return WorkBundle{}, err
	}
	subgates, _, err := unshardedSubgatesFromPlan(plan)
	if err != nil {
		return WorkBundle{}, err
	}
	var subgate Subgate
	for _, candidate := range subgates {
		if candidate.ID == command.SubgateID {
			subgate = candidate
			break
		}
	}
	if subgate.ID == "" {
		return WorkBundle{}, fmt.Errorf("distributed work unit %q has no subgate", workUnitID)
	}
	unitRoot := filepath.Join(options.OutputDirectory, workUnitID)
	if err := os.MkdirAll(filepath.Join(unitRoot, "observations"), 0o700); err != nil {
		return WorkBundle{}, fmt.Errorf("create distributed work output: %w", err)
	}
	auditPath := filepath.Join(unitRoot, "repository-audit.json")
	inputAuditDigest := ""
	if slices.Contains(dependency.Requires, "repository-audit") {
		if options.RepositoryAuditInputPath == "" {
			return WorkBundle{}, fmt.Errorf("distributed work unit %q requires repository-audit input", workUnitID)
		}
		data, readErr := os.ReadFile(options.RepositoryAuditInputPath)
		if readErr != nil {
			return WorkBundle{}, fmt.Errorf("read distributed repository-audit input: %w", readErr)
		}
		inputAuditDigest = soundnessevidence.DigestBytes(data)
		if writeErr := writeAtomicBytes(auditPath, data); writeErr != nil {
			return WorkBundle{}, writeErr
		}
	}
	runID, err := dependencies.newRunID()
	if err != nil {
		return WorkBundle{}, err
	}
	binding, err := createSubgateBinding(runID, plan.Workspace.Digest, manifestDigest, subgate)
	if err != nil {
		return WorkBundle{}, err
	}
	reportPath := filepath.Join(unitRoot, expectedReport.ReportFile)
	environment := boundedGoEnvironment(
		subgateEnvironment(os.Environ(), binding, filepath.Join(unitRoot, "observations"), reportPath, auditPath),
		command.ReservedResources.CPUUnits,
	)
	stdout := options.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := options.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	queuedAt := time.Now().UTC()
	startedAt := time.Now().UTC()
	commandCtx, cancel := context.WithTimeout(ctx, time.Duration(command.TimeoutSeconds)*time.Second)
	metrics, executeErr := dependencies.execute(
		commandCtx,
		filepath.Join(root, filepath.FromSlash(command.WorkingDirectory)),
		command.Command,
		environment,
		stdout,
		stderr,
	)
	contextErr := commandCtx.Err()
	cancel()
	if contextErr != nil {
		return WorkBundle{}, fmt.Errorf("distributed work unit %q timed out or was canceled; no bundle accepted: %w", workUnitID, contextErr)
	}
	if executeErr != nil {
		return WorkBundle{}, fmt.Errorf("distributed work unit %q failed; no bundle accepted: %w", workUnitID, executeErr)
	}
	finishedAt := time.Now().UTC()
	report, _, err := loadReportWithDigest(ctx, reportPath)
	if err != nil {
		return WorkBundle{}, err
	}
	reportDigest, err := canonicalReportDigest(report)
	if err != nil {
		return WorkBundle{}, err
	}
	if err := subgate.ValidateReport(report, binding); err != nil {
		return WorkBundle{}, err
	}
	observations, err := soundnessevidence.LoadObservations(ctx, filepath.Join(unitRoot, "observations"))
	if err != nil {
		return WorkBundle{}, fmt.Errorf("load distributed soundness observations: %w", err)
	}
	outputAuditDigest := ""
	if workUnitID == "repository-audit" {
		data, readErr := os.ReadFile(auditPath)
		if readErr != nil {
			return WorkBundle{}, fmt.Errorf("distributed repository-audit work omitted its shared artifact: %w", readErr)
		}
		outputAuditDigest = soundnessevidence.DigestBytes(data)
	}
	finalWorkspaceDigest, err := dependencies.plan.workspaceDigest(ctx, root)
	if err != nil {
		return WorkBundle{}, err
	}
	if finalWorkspaceDigest != plan.Workspace.Digest {
		return WorkBundle{}, errors.New("workspace changed during distributed work execution")
	}
	bundle := WorkBundle{
		FormatVersion: WorkBundleFormatVersion, PlanID: plan.PlanID, WorkUnitID: workUnitID,
		WorkspaceDigest: plan.Workspace.Digest, ManifestDigest: plan.Manifest.Digest,
		RegistryDigest: plan.Registry.Digest, ToolchainDigest: plan.Toolchain.Digest,
		CommandDigest: command.CommandDigest, BinaryDigest: command.BinaryDigest,
		InputRepositoryAuditDigest: inputAuditDigest, OutputRepositoryAuditDigest: outputAuditDigest,
		TerminalStatus: distributedStatusPassed, Binding: binding, Report: report,
		ReportDigest: reportDigest, Observations: observations,
		QueuedAt: queuedAt, StartedAt: startedAt, FinishedAt: finishedAt,
		WallDurationNanoseconds: finishedAt.Sub(startedAt).Nanoseconds(),
		CPUTimeNanoseconds:      metrics.CPUTimeNanoseconds, PeakRSSBytes: metrics.PeakRSSBytes,
	}
	normalized, err := normalizeWorkBundle(bundle)
	if err != nil {
		return WorkBundle{}, err
	}
	return normalized, nil
}

// AggregateWorkBundles rejects missing, duplicate, stale, or mismatched work
// and returns one canonical aggregate report.
func AggregateWorkBundles(
	ctx context.Context,
	plan ExecutionPlan,
	options AggregateBundleOptions,
) (RunReport, error) {
	if err := validateCurrentPlan(ctx, plan, options.Root, planDependencies{
		workspaceDigest: WorkspaceDigest, binaryDigest: executableDigest, toolchain: currentToolchainBinding,
	}); err != nil {
		return RunReport{}, err
	}
	root, manifestPath, err := resolvePaths(options.Root, plan.Manifest.Path)
	if err != nil {
		return RunReport{}, err
	}
	manifest, _, err := LoadManifest(ctx, manifestPath)
	if err != nil {
		return RunReport{}, err
	}
	registry, err := soundnessevidence.LoadRegistry(ctx, filepath.Join(root, filepath.FromSlash(plan.Registry.Path)))
	if err != nil {
		return RunReport{}, fmt.Errorf("load aggregate soundness registry: %w", err)
	}
	bundles := make(map[string]WorkBundle, len(options.BundlePaths))
	for _, path := range options.BundlePaths {
		bundle, loadErr := LoadWorkBundle(ctx, path, plan)
		if loadErr != nil {
			return RunReport{}, loadErr
		}
		if _, duplicate := bundles[bundle.WorkUnitID]; duplicate {
			return RunReport{}, fmt.Errorf("distributed aggregate received duplicate bundle for %q", bundle.WorkUnitID)
		}
		bundles[bundle.WorkUnitID] = bundle
	}
	auditDigest, err := distributedAuditDigest(ctx, plan, options.RepositoryAuditPath)
	if err != nil {
		return RunReport{}, err
	}
	if err := validateCompleteBundleSet(plan, bundles, auditDigest); err != nil {
		return RunReport{}, err
	}
	aggregateRunID, err := newRunID()
	if err != nil {
		return RunReport{}, err
	}
	report, err := aggregateBundleSet(plan, manifest, registry, bundles, aggregateRunID)
	if err != nil {
		return RunReport{}, err
	}
	resolvedReportPath, err := resolveExternalArtifactPath(root, options.ReportPath, "", "distributed aggregate report")
	if err != nil {
		return RunReport{}, err
	}
	if resolvedReportPath != "" {
		if err := writeExclusiveJSON(ctx, resolvedReportPath, report); err != nil {
			return RunReport{}, err
		}
	}
	telemetry, err := buildDistributedTelemetry(plan, bundles, report.RunID)
	if err != nil {
		return RunReport{}, err
	}
	resolvedTelemetryPath, err := resolveExternalArtifactPath(
		root, options.TelemetryPath, EnvTelemetryPath, "distributed aggregate telemetry",
	)
	if err != nil {
		return RunReport{}, err
	}
	if resolvedTelemetryPath != "" {
		if err := writeExclusiveJSON(ctx, resolvedTelemetryPath, telemetry); err != nil {
			return RunReport{}, err
		}
	}
	return report, nil
}

func aggregateBundleSet(
	plan ExecutionPlan,
	manifest Manifest,
	registry soundnessevidence.Registry,
	bundles map[string]WorkBundle,
	aggregateRunID string,
) (RunReport, error) {
	results := make([]SubgateResult, 0, len(bundles))
	observations := make([]soundnessevidence.SemanticObservation, 0, len(registry.Registrations))
	for _, command := range plan.Commands {
		bundle := bundles[command.WorkUnitID]
		results = append(results, SubgateResult{
			ID: command.SubgateID, CommandDigest: command.CommandDigest,
			ReportDigest: bundle.ReportDigest, Populations: slices.Clone(bundle.Report.Populations),
		})
		for _, observation := range bundle.Observations {
			observation.Binding.RunID = aggregateRunID
			observations = append(observations, observation)
		}
	}
	slices.SortFunc(results, func(left, right SubgateResult) int { return strings.Compare(left.ID, right.ID) })
	slices.SortFunc(observations, func(left, right soundnessevidence.SemanticObservation) int {
		return strings.Compare(left.RegistrationID, right.RegistrationID)
	})
	report := RunReport{
		FormatVersion: RunReportFormatVersion, Profile: plan.Profile, RunID: aggregateRunID,
		WorkspaceDigest: plan.Workspace.Digest, ManifestDigest: plan.Manifest.Digest,
		Subgates: results, Observations: observations,
	}
	if err := ValidateRunReport(report, manifest, registry); err != nil {
		return RunReport{}, err
	}
	return report, nil
}

// CanonicalWorkBundleJSON returns a validated deterministic bundle encoding.
func CanonicalWorkBundleJSON(bundle WorkBundle, plan ExecutionPlan) ([]byte, error) {
	if err := bundle.Validate(plan); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal canonical distributed work bundle: %w", err)
	}
	return append(data, '\n'), nil
}

// LoadWorkBundle strictly loads and validates one distributed result.
func LoadWorkBundle(ctx context.Context, path string, plan ExecutionPlan) (WorkBundle, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return WorkBundle{}, fmt.Errorf("inspect distributed work bundle: %w", err)
	}
	if !info.Mode().IsRegular() {
		return WorkBundle{}, fmt.Errorf("distributed work bundle %s is not a regular file", path)
	}
	data, err := readFile(ctx, path)
	if err != nil {
		return WorkBundle{}, err
	}
	var bundle WorkBundle
	if err := decodeStrictJSON(data, &bundle); err != nil {
		return WorkBundle{}, err
	}
	if err := bundle.Validate(plan); err != nil {
		return WorkBundle{}, err
	}
	return bundle, nil
}

func requireOutsideWorkspace(root, path, description string) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("%s path %q must be absolute", description, path)
	}
	cleanPath := filepath.Clean(path)
	relative, err := filepath.Rel(root, cleanPath)
	if err != nil {
		return fmt.Errorf("resolve %s path relative to workspace: %w", description, err)
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s path %q must be outside workspace %q", description, path, root)
	}
	return nil
}

// Validate checks every exact plan, command, binary, report, observation, and
// resource-result binding carried by a distributed bundle.
func (bundle WorkBundle) Validate(plan ExecutionPlan) error {
	if bundle.FormatVersion != WorkBundleFormatVersion || bundle.BundleID == "" ||
		bundle.PlanID != plan.PlanID || bundle.TerminalStatus != distributedStatusPassed {
		return errors.New("distributed work bundle has an invalid version, plan, or terminal status")
	}
	command, _, _, err := distributedBindings(plan, bundle.WorkUnitID)
	if err != nil {
		return err
	}
	if bundle.WorkspaceDigest != plan.Workspace.Digest || bundle.ManifestDigest != plan.Manifest.Digest ||
		bundle.RegistryDigest != plan.Registry.Digest || bundle.ToolchainDigest != plan.Toolchain.Digest ||
		bundle.CommandDigest != command.CommandDigest || bundle.BinaryDigest != command.BinaryDigest {
		return fmt.Errorf("distributed work bundle %q has stale plan bindings", bundle.WorkUnitID)
	}
	if bundle.Binding.WorkspaceDigest != plan.Workspace.Digest || bundle.Binding.ManifestDigest != plan.Manifest.Digest ||
		bundle.Binding.CommandDigest != command.CommandDigest || bundle.Binding.SubgateID != command.SubgateID {
		return fmt.Errorf("distributed work bundle %q has stale evidence binding", bundle.WorkUnitID)
	}
	if bundle.Report.Binding != bundle.Binding || bundle.Report.Status != StatusPassed {
		return fmt.Errorf("distributed work bundle %q has a mismatched report", bundle.WorkUnitID)
	}
	if err := bundle.Report.Validate(); err != nil {
		return err
	}
	_, expectedReport, _, err := distributedBindings(plan, bundle.WorkUnitID)
	if err != nil {
		return err
	}
	if len(bundle.Report.Populations) != len(expectedReport.RequiredPopulations) {
		return fmt.Errorf("distributed work bundle %q has an incomplete population set", bundle.WorkUnitID)
	}
	for index, requirement := range expectedReport.RequiredPopulations {
		population := bundle.Report.Populations[index]
		if population.ID != requirement.ID || population.Count < requirement.Minimum {
			return fmt.Errorf("distributed work bundle %q has a weakened population %q", bundle.WorkUnitID, population.ID)
		}
	}
	if err := soundnessevidence.ValidateDigest("distributed report digest", bundle.ReportDigest); err != nil {
		return fmt.Errorf("validate distributed report digest: %w", err)
	}
	reportDigest, err := canonicalReportDigest(bundle.Report)
	if err != nil {
		return err
	}
	if bundle.ReportDigest != reportDigest {
		return fmt.Errorf("distributed work bundle %q report digest does not match its embedded report", bundle.WorkUnitID)
	}
	for _, artifact := range []struct{ name, digest string }{
		{name: "input repository audit", digest: bundle.InputRepositoryAuditDigest},
		{name: "output repository audit", digest: bundle.OutputRepositoryAuditDigest},
	} {
		if artifact.digest != "" {
			if err := soundnessevidence.ValidateDigest("distributed "+artifact.name, artifact.digest); err != nil {
				return fmt.Errorf("validate distributed %s digest: %w", artifact.name, err)
			}
		}
	}
	_, _, dependency, err := distributedBindings(plan, bundle.WorkUnitID)
	if err != nil {
		return err
	}
	requiresAudit := slices.Contains(dependency.Requires, "repository-audit")
	if requiresAudit != (bundle.InputRepositoryAuditDigest != "") {
		return fmt.Errorf("distributed work bundle %q has an invalid repository-audit input binding", bundle.WorkUnitID)
	}
	producesAudit := bundle.WorkUnitID == "repository-audit"
	if producesAudit != (bundle.OutputRepositoryAuditDigest != "") {
		return fmt.Errorf("distributed work bundle %q has an invalid repository-audit output binding", bundle.WorkUnitID)
	}
	for _, observation := range bundle.Observations {
		if observation.Binding != bundle.Binding {
			return fmt.Errorf("distributed work bundle %q has a mismatched observation", bundle.WorkUnitID)
		}
		if err := observation.Validate(); err != nil {
			return fmt.Errorf("validate distributed observation %q: %w", observation.RegistrationID, err)
		}
	}
	observedRegistrationIDs := make([]string, 0, len(bundle.Observations))
	for _, observation := range bundle.Observations {
		observedRegistrationIDs = append(observedRegistrationIDs, observation.RegistrationID)
	}
	slices.Sort(observedRegistrationIDs)
	if !slices.Equal(observedRegistrationIDs, expectedReport.RequiredRegistrationIDs) {
		return fmt.Errorf("distributed work bundle %q has incomplete required observations", bundle.WorkUnitID)
	}
	if bundle.QueuedAt.IsZero() || bundle.StartedAt.Before(bundle.QueuedAt) || bundle.FinishedAt.Before(bundle.StartedAt) ||
		bundle.WallDurationNanoseconds != bundle.FinishedAt.Sub(bundle.StartedAt).Nanoseconds() ||
		bundle.CPUTimeNanoseconds < 0 || bundle.PeakRSSBytes < 0 {
		return fmt.Errorf("distributed work bundle %q has invalid resource metrics", bundle.WorkUnitID)
	}
	computed, err := bundle.calculateID()
	if err != nil {
		return err
	}
	if computed != bundle.BundleID {
		return fmt.Errorf("distributed work bundle %q identity does not match its content", bundle.WorkUnitID)
	}
	return nil
}

func validateCompleteBundleSet(plan ExecutionPlan, bundles map[string]WorkBundle, auditDigest string) error {
	missing := make([]string, 0)
	for _, command := range plan.Commands {
		if _, exists := bundles[command.WorkUnitID]; !exists {
			missing = append(missing, command.WorkUnitID)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("distributed aggregate is missing required work bundles: %s", strings.Join(missing, ", "))
	}
	if len(bundles) != len(plan.Commands) {
		return errors.New("distributed aggregate contains unexpected work bundles")
	}
	if auditBundle, exists := bundles["repository-audit"]; exists {
		if auditDigest == "" || auditBundle.OutputRepositoryAuditDigest != auditDigest {
			return errors.New("distributed aggregate repository-audit artifact does not match its producer bundle")
		}
		for _, dependency := range plan.Dependencies {
			if slices.Contains(dependency.Requires, "repository-audit") &&
				bundles[dependency.WorkUnitID].InputRepositoryAuditDigest != auditDigest {
				return fmt.Errorf("distributed work unit %q consumed a mismatched repository audit", dependency.WorkUnitID)
			}
		}
	}
	return nil
}

func canonicalReportDigest(report Report) (string, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode canonical distributed report: %w", err)
	}
	return soundnessevidence.DigestBytes(append(data, '\n')), nil
}

func distributedAuditDigest(ctx context.Context, plan ExecutionPlan, path string) (string, error) {
	requiresAudit := slices.ContainsFunc(plan.Commands, func(command PlanCommandBinding) bool {
		return command.WorkUnitID == "repository-audit"
	})
	if !requiresAudit {
		if path != "" {
			return "", errors.New("distributed aggregate received an unexpected repository-audit artifact")
		}
		return "", nil
	}
	if path == "" {
		return "", errors.New("distributed aggregate requires the repository-audit artifact")
	}
	data, err := readFile(ctx, path)
	if err != nil {
		return "", fmt.Errorf("load distributed repository-audit artifact: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}

func buildDistributedTelemetry(
	plan ExecutionPlan,
	bundles map[string]WorkBundle,
	runID string,
) (RunTelemetry, error) {
	commands := make(map[string]PlanCommandBinding, len(plan.Commands))
	scheduled := make([]scheduledWorkResult[workUnitProduct], 0, len(plan.Commands))
	subgates := make([]SubgateTelemetry, 0, len(plan.Commands))
	var startedAt, finishedAt time.Time
	for _, command := range plan.Commands {
		bundle := bundles[command.WorkUnitID]
		commands[command.WorkUnitID] = command
		scheduled = append(scheduled, scheduledWorkResult[workUnitProduct]{
			WorkUnitID: command.WorkUnitID, QueuedAt: bundle.QueuedAt,
			StartedAt: bundle.StartedAt, FinishedAt: bundle.FinishedAt,
		})
		if startedAt.IsZero() || bundle.QueuedAt.Before(startedAt) {
			startedAt = bundle.QueuedAt
		}
		if bundle.FinishedAt.After(finishedAt) {
			finishedAt = bundle.FinishedAt
		}
		subgates = append(subgates, SubgateTelemetry{
			ID: command.SubgateID, QueuedAt: bundle.QueuedAt, StartedAt: bundle.StartedAt, FinishedAt: bundle.FinishedAt,
			QueueDurationNanoseconds: bundle.StartedAt.Sub(bundle.QueuedAt).Nanoseconds(),
			WallDurationNanoseconds:  bundle.WallDurationNanoseconds,
			CPUTimeNanoseconds:       bundle.CPUTimeNanoseconds, PeakRSSBytes: bundle.PeakRSSBytes,
			ReservedResources: command.ReservedResources, TimeoutSeconds: command.TimeoutSeconds,
			TerminalStatus: terminalStatusPassed, Populations: slices.Clone(bundle.Report.Populations),
		})
	}
	slices.SortFunc(subgates, func(left, right SubgateTelemetry) int {
		return strings.Compare(left.ID, right.ID)
	})
	maximumCPU, maximumMemory := maximumConcurrentReservations(scheduled, commands)
	telemetry := RunTelemetry{
		FormatVersion: TelemetryFormatVersion, RunID: runID, Profile: plan.Profile,
		WorkspaceDigest: plan.Workspace.Digest, ManifestDigest: plan.Manifest.Digest,
		StartedAt: startedAt, FinishedAt: finishedAt,
		WallDurationNanoseconds: finishedAt.Sub(startedAt).Nanoseconds(),
		CriticalPathNanoseconds: criticalPathNanoseconds(scheduled, plan.Dependencies),
		MaxReservedCPUUnits:     maximumCPU, MaxReservedMemoryBytes: maximumMemory,
		Subgates: subgates,
	}
	if err := telemetry.Validate(); err != nil {
		return RunTelemetry{}, fmt.Errorf("validate distributed aggregate telemetry: %w", err)
	}
	return telemetry, nil
}

func normalizeWorkBundle(bundle WorkBundle) (WorkBundle, error) {
	bundle.Observations = slices.Clone(bundle.Observations)
	slices.SortFunc(bundle.Observations, func(left, right soundnessevidence.SemanticObservation) int {
		return strings.Compare(left.RegistrationID, right.RegistrationID)
	})
	bundle.BundleID = ""
	bundleID, err := bundle.calculateID()
	if err != nil {
		return WorkBundle{}, err
	}
	bundle.BundleID = bundleID
	return bundle, nil
}

func (bundle WorkBundle) calculateID() (string, error) {
	bundle.BundleID = ""
	data, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("marshal distributed work bundle identity: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}

func distributedBindings(
	plan ExecutionPlan,
	workUnitID string,
) (PlanCommandBinding, PlanExpectedReportBinding, PlanDependencyBinding, error) {
	var command PlanCommandBinding
	var report PlanExpectedReportBinding
	var dependency PlanDependencyBinding
	for _, candidate := range plan.Commands {
		if candidate.WorkUnitID == workUnitID {
			command = candidate
			break
		}
	}
	for _, candidate := range plan.ExpectedReports {
		if candidate.WorkUnitID == workUnitID {
			report = candidate
			break
		}
	}
	for _, candidate := range plan.Dependencies {
		if candidate.WorkUnitID == workUnitID {
			dependency = candidate
			break
		}
	}
	if command.WorkUnitID == "" || report.WorkUnitID == "" || dependency.WorkUnitID == "" {
		return PlanCommandBinding{}, PlanExpectedReportBinding{}, PlanDependencyBinding{},
			fmt.Errorf("soundness plan has no complete binding for work unit %q", workUnitID)
	}
	return command, report, dependency, nil
}

func writeAtomicBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create distributed artifact directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".distributed-*.tmp")
	if err != nil {
		return fmt.Errorf("create distributed artifact temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath) //nolint:errcheck // Best-effort private temporary cleanup.
	if _, err := temporary.Write(data); err != nil {
		temporary.Close() //nolint:errcheck // Preserve the primary write error.
		return fmt.Errorf("write distributed artifact temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close distributed artifact temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish distributed artifact: %w", err)
	}
	return nil
}
