// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

func TestWorkBundleValidateRejectsStaleOrIncompleteBindings(t *testing.T) {
	t.Parallel()

	plan := validExecutionPlan()
	bundle := validWorkBundle(t, plan, plan.Commands[0].WorkUnitID)
	tests := []struct {
		name   string
		mutate func(*WorkBundle)
	}{
		{name: "plan", mutate: func(value *WorkBundle) { value.PlanID = runnerChangedDigest }},
		{name: "workspace", mutate: func(value *WorkBundle) { value.WorkspaceDigest = runnerChangedDigest }},
		{name: "command", mutate: func(value *WorkBundle) { value.CommandDigest = runnerChangedDigest }},
		{name: "binary", mutate: func(value *WorkBundle) { value.BinaryDigest = runnerChangedDigest }},
		{name: "terminal", mutate: func(value *WorkBundle) { value.TerminalStatus = "canceled" }},
		{name: "population", mutate: func(value *WorkBundle) { value.Report.Populations = nil }},
		{name: "bundle identity", mutate: func(value *WorkBundle) { value.PeakRSSBytes++ }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutated := bundle
			mutated.Report.Populations = slices.Clone(bundle.Report.Populations)
			testCase.mutate(&mutated)
			if err := mutated.Validate(plan); err == nil {
				t.Fatal("Validate() accepted a stale or incomplete distributed bundle")
			}
		})
	}
}

func TestWorkBundleValidateRejectsReportDigestThatDoesNotBindEmbeddedReport(t *testing.T) {
	t.Parallel()

	plan := validExecutionPlan()
	bundle := validWorkBundle(t, plan, plan.Commands[0].WorkUnitID)
	bundle.Report.Populations[0].Count++
	mutated, err := normalizeWorkBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := mutated.Validate(plan); err == nil {
		t.Fatal("Validate() accepted a recomputed bundle identity with a stale embedded-report digest")
	}
}

func TestValidateCompleteBundleSetRejectsMissingUnexpectedAndMismatchedDependencyArtifacts(t *testing.T) {
	t.Parallel()

	plan := validExecutionPlan()
	plan.Commands[0].WorkUnitID = "repository-audit"
	plan.Dependencies[0].WorkUnitID = "repository-audit"
	plan.ExpectedReports[0].WorkUnitID = "repository-audit"
	plan.Shards[0].WorkUnitID = "repository-audit"
	plan.Commands[1].WorkUnitID = "dependent"
	plan.Dependencies[1] = PlanDependencyBinding{WorkUnitID: "dependent", Requires: []string{"repository-audit"}}
	plan.ExpectedReports[1].WorkUnitID = "dependent"
	plan.Shards[1].WorkUnitID = "dependent"
	plan, err := NormalizeExecutionPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	auditDigest := soundnessevidence.DigestBytes([]byte("audit"))
	bundles := map[string]WorkBundle{
		"repository-audit": {WorkUnitID: "repository-audit", OutputRepositoryAuditDigest: auditDigest},
		"dependent":        {WorkUnitID: "dependent", InputRepositoryAuditDigest: auditDigest},
	}
	if err := validateCompleteBundleSet(plan, bundles, auditDigest); err != nil {
		t.Fatalf("validateCompleteBundleSet() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(map[string]WorkBundle)
	}{
		{name: "missing", mutate: func(values map[string]WorkBundle) { delete(values, "dependent") }},
		{name: "unexpected", mutate: func(values map[string]WorkBundle) { values["extra"] = WorkBundle{} }},
		{name: "mismatched audit", mutate: func(values map[string]WorkBundle) {
			value := values["dependent"]
			value.InputRepositoryAuditDigest = runnerChangedDigest
			values["dependent"] = value
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			values := make(map[string]WorkBundle, len(bundles))
			maps.Copy(values, bundles)
			testCase.mutate(values)
			if err := validateCompleteBundleSet(plan, values, auditDigest); err == nil {
				t.Fatal("validateCompleteBundleSet() accepted incomplete artifacts")
			}
		})
	}
}

func TestFixtureDrivenDistributedPlanWorkersAndAggregate(t *testing.T) {
	t.Parallel()

	manifest := validGateManifest()
	registry := validGateRegistry()
	root, manifestPath, _ := writeRunnerFixture(t, manifest, registry)
	plan, err := generatePlan(t.Context(), PlanOptions{
		Root: root, ManifestPath: manifestPath, Profile: ProfileSemantic,
		Resources: ResourceBudget{
			CPUUnits: 4, MemoryBytes: 16 * 1024 * 1024 * 1024,
			MaximumWorkers: 4, RunnerClass: "fixture-runner",
		},
	}, deterministicPlanDependencies())
	if err != nil {
		t.Fatal(err)
	}
	bundles := make(map[string]WorkBundle, len(plan.Commands))
	for _, command := range plan.Commands {
		bundle := validFixtureWorkBundle(t, plan, registry, command.WorkUnitID)
		if err := bundle.Validate(plan); err != nil {
			t.Fatalf("worker bundle %s: %v", command.WorkUnitID, err)
		}
		bundles[command.WorkUnitID] = bundle
	}
	if err := validateCompleteBundleSet(plan, bundles, ""); err != nil {
		t.Fatal(err)
	}
	report, err := aggregateBundleSet(plan, manifest, registry, bundles, "run-aggregate")
	if err != nil {
		t.Fatalf("aggregateBundleSet() error = %v", err)
	}
	if len(report.Subgates) != len(plan.Commands) || len(report.Observations) != len(registry.Registrations) {
		t.Fatalf("aggregate report has %d subgates and %d observations", len(report.Subgates), len(report.Observations))
	}
	telemetry, err := buildDistributedTelemetry(plan, bundles, "run-aggregate")
	if err != nil {
		t.Fatalf("buildDistributedTelemetry() error = %v", err)
	}
	wantMaximumCPU := 0
	for _, command := range plan.Commands {
		wantMaximumCPU += command.ReservedResources.CPUUnits
	}
	if len(telemetry.Subgates) != len(plan.Commands) || telemetry.MaxReservedCPUUnits != wantMaximumCPU {
		t.Fatalf("distributed telemetry = %+v", telemetry)
	}
}

func validWorkBundle(t *testing.T, plan ExecutionPlan, workUnitID string) WorkBundle {
	t.Helper()
	command, expected, _, err := distributedBindings(plan, workUnitID)
	if err != nil {
		t.Fatal(err)
	}
	binding := soundnessevidence.ObservationBinding{
		RunID: "run-worker", WorkspaceDigest: plan.Workspace.Digest,
		ManifestDigest: plan.Manifest.Digest, CommandDigest: command.CommandDigest, SubgateID: command.SubgateID,
	}
	populations := make([]Population, 0, len(expected.RequiredPopulations))
	for _, requirement := range expected.RequiredPopulations {
		populations = append(populations, Population{ID: requirement.ID, Count: requirement.Minimum})
	}
	report := Report{FormatVersion: ReportFormatVersion, Binding: binding, Status: StatusPassed, Populations: populations}
	reportDigest, err := canonicalReportDigest(report)
	if err != nil {
		t.Fatal(err)
	}
	queuedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	bundle, err := normalizeWorkBundle(WorkBundle{
		FormatVersion: WorkBundleFormatVersion, PlanID: plan.PlanID, WorkUnitID: workUnitID,
		WorkspaceDigest: plan.Workspace.Digest, ManifestDigest: plan.Manifest.Digest,
		RegistryDigest: plan.Registry.Digest, ToolchainDigest: plan.Toolchain.Digest,
		CommandDigest: command.CommandDigest, BinaryDigest: command.BinaryDigest,
		TerminalStatus: distributedStatusPassed, Binding: binding,
		Report: report, ReportDigest: reportDigest, Observations: []soundnessevidence.SemanticObservation{},
		QueuedAt: queuedAt, StartedAt: queuedAt, FinishedAt: queuedAt.Add(time.Second),
		WallDurationNanoseconds: time.Second.Nanoseconds(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func validFixtureWorkBundle(
	t *testing.T,
	plan ExecutionPlan,
	registry soundnessevidence.Registry,
	workUnitID string,
) WorkBundle {
	t.Helper()
	command, expected, _, err := distributedBindings(plan, workUnitID)
	if err != nil {
		t.Fatal(err)
	}
	binding := soundnessevidence.ObservationBinding{
		RunID: "run-" + workUnitID, WorkspaceDigest: plan.Workspace.Digest,
		ManifestDigest: plan.Manifest.Digest, CommandDigest: command.CommandDigest, SubgateID: command.SubgateID,
	}
	populations := make([]Population, 0, len(expected.RequiredPopulations))
	for _, requirement := range expected.RequiredPopulations {
		populations = append(populations, Population{ID: requirement.ID, Count: requirement.Minimum})
	}
	observations := make([]soundnessevidence.SemanticObservation, 0, len(expected.RequiredRegistrationIDs))
	for _, registrationID := range expected.RequiredRegistrationIDs {
		for _, registration := range registry.Registrations {
			if registration.ID == registrationID {
				observations = append(observations, validGateObservation(registration, binding))
				break
			}
		}
	}
	report := Report{FormatVersion: ReportFormatVersion, Binding: binding, Status: StatusPassed, Populations: populations}
	reportDigest, err := canonicalReportDigest(report)
	if err != nil {
		t.Fatal(err)
	}
	queuedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	bundle, err := normalizeWorkBundle(WorkBundle{
		FormatVersion: WorkBundleFormatVersion, PlanID: plan.PlanID, WorkUnitID: workUnitID,
		WorkspaceDigest: plan.Workspace.Digest, ManifestDigest: plan.Manifest.Digest,
		RegistryDigest: plan.Registry.Digest, ToolchainDigest: plan.Toolchain.Digest,
		CommandDigest: command.CommandDigest, BinaryDigest: command.BinaryDigest,
		TerminalStatus: distributedStatusPassed, Binding: binding,
		Report: report, ReportDigest: reportDigest, Observations: observations,
		QueuedAt: queuedAt, StartedAt: queuedAt, FinishedAt: queuedAt.Add(time.Second),
		WallDurationNanoseconds: time.Second.Nanoseconds(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestExecutePlanWorkUnitBindsSharedAuditArtifactToChildEnvironment(t *testing.T) {
	t.Parallel()

	manifest := validGateManifest()
	auditPopulation := PopulationRequirement{ID: "repository-scans", Minimum: 1}
	consumerPopulation := PopulationRequirement{ID: "cases", Minimum: 1}
	extraSubgates := []Subgate{
		{
			ID: "repository-audit", WorkingDirectory: ".",
			Command: []string{"producer", "repository-audit"}, TimeoutSeconds: 10,
			ReportFile: "report.json", RequiredRegistrationIDs: []string{},
			RequiredPopulations: []PopulationRequirement{auditPopulation},
			Dependencies:        []string{}, CPUUnits: 1, EstimatedPeakMemoryBytes: 1024,
			ExclusivityGroups: []string{}, Distributable: true,
			ProfileIDs: []ProfileID{ProfileComplete, ProfileHarness, ProfileSemantic},
		},
		{
			ID: "audit-consumer", WorkingDirectory: ".",
			Command: []string{"producer", "audit-consumer"}, TimeoutSeconds: 10,
			ReportFile: "report.json", RequiredRegistrationIDs: []string{},
			RequiredPopulations: []PopulationRequirement{consumerPopulation},
			Dependencies:        []string{"repository-audit"}, CPUUnits: 1, EstimatedPeakMemoryBytes: 1024,
			ExclusivityGroups: []string{}, Distributable: true,
			ProfileIDs: []ProfileID{ProfileComplete, ProfileHarness, ProfileSemantic},
		},
	}
	manifest.Subgates = append(manifest.Subgates, extraSubgates...)
	slices.SortFunc(manifest.Subgates, func(a, b Subgate) int { return strings.Compare(a.ID, b.ID) })
	for index := range manifest.Profiles {
		if manifest.Profiles[index].ID == ProfileConsumer {
			continue
		}
		manifest.Profiles[index].SubgateIDs = append(
			manifest.Profiles[index].SubgateIDs, "repository-audit", "audit-consumer",
		)
		slices.Sort(manifest.Profiles[index].SubgateIDs)
	}
	registry := validGateRegistry()
	root, manifestPath, _ := writeRunnerFixture(t, manifest, registry)
	plan, err := generatePlan(t.Context(), PlanOptions{
		Root: root, ManifestPath: manifestPath, Profile: ProfileSemantic,
		Resources: ResourceBudget{
			CPUUnits: 4, MemoryBytes: 16 * 1024 * 1024 * 1024,
			MaximumWorkers: 4, RunnerClass: "fixture-runner",
		},
	}, deterministicPlanDependencies())
	if err != nil {
		t.Fatal(err)
	}
	auditPayload := []byte(`{"result_id":"sha256:fixture-shared-audit"}` + "\n")
	dependencies := distributedWorkDependencies{
		plan:     deterministicPlanDependencies(),
		execute:  sharedAuditTestExecute(t, auditPayload),
		newRunID: func() (string, error) { return "run-distributed-audit-test", nil },
	}

	producedBundle, err := executePlanWorkUnit(t.Context(), plan, "repository-audit", DistributedWorkOptions{
		Root: root, OutputDirectory: t.TempDir(),
	}, dependencies)
	if err != nil {
		t.Fatalf("executePlanWorkUnit(repository-audit) error = %v", err)
	}
	wantDigest := soundnessevidence.DigestBytes(auditPayload)
	if producedBundle.OutputRepositoryAuditDigest != wantDigest {
		t.Fatalf(
			"repository-audit bundle output digest = %q, want the child-environment artifact digest %q",
			producedBundle.OutputRepositoryAuditDigest, wantDigest,
		)
	}

	inputPath := filepath.Join(t.TempDir(), "repository-audit.json")
	if err := os.WriteFile(inputPath, auditPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	consumedBundle, err := executePlanWorkUnit(t.Context(), plan, "audit-consumer", DistributedWorkOptions{
		Root: root, OutputDirectory: t.TempDir(), RepositoryAuditInputPath: inputPath,
	}, dependencies)
	if err != nil {
		t.Fatalf("executePlanWorkUnit(audit-consumer) error = %v", err)
	}
	if consumedBundle.InputRepositoryAuditDigest != wantDigest {
		t.Fatalf(
			"audit-consumer bundle input digest = %q, want %q",
			consumedBundle.InputRepositoryAuditDigest, wantDigest,
		)
	}
}

// sharedAuditTestExecute emulates a spawned work-unit process that honors the
// child environment contract exactly: it writes its report to
// GOPLINT_SUBGATE_REPORT_PATH and produces or consumes the shared repository
// audit at GOPLINT_REPOSITORY_AUDIT_PATH.
func sharedAuditTestExecute(
	t *testing.T,
	auditPayload []byte,
) func(context.Context, string, []string, []string, io.Writer, io.Writer) (commandMetrics, error) {
	t.Helper()
	return func(
		ctx context.Context,
		_ string,
		_ []string,
		environment []string,
		_ io.Writer,
		_ io.Writer,
	) (commandMetrics, error) {
		lookup := environmentLookup(environment)
		binding, err := bindingFromLookup(lookup)
		if err != nil {
			return commandMetrics{}, err
		}
		reportPath, exists := lookup(EnvSubgateReportPath)
		if !exists {
			return commandMetrics{}, errors.New("child environment has no report path")
		}
		auditPath, exists := lookup(EnvRepositoryAuditPath)
		if !exists {
			return commandMetrics{}, errors.New("child environment has no repository-audit path")
		}
		population := Population{ID: "cases", Count: 1}
		switch binding.SubgateID {
		case "repository-audit":
			population = Population{ID: "repository-scans", Count: 1}
			if err := os.WriteFile(auditPath, auditPayload, 0o600); err != nil {
				return commandMetrics{}, fmt.Errorf("write shared audit artifact: %w", err)
			}
		case "audit-consumer":
			content, err := os.ReadFile(auditPath)
			if err != nil {
				return commandMetrics{}, fmt.Errorf("read bound shared audit input: %w", err)
			}
			if !bytes.Equal(content, auditPayload) {
				return commandMetrics{}, errors.New("bound shared audit input does not match the produced artifact")
			}
		}
		report := Report{
			FormatVersion: ReportFormatVersion,
			Binding:       binding,
			Status:        StatusPassed,
			Populations:   []Population{population},
		}
		if err := writeExclusiveJSON(ctx, reportPath, report); err != nil {
			return commandMetrics{}, err
		}
		return commandMetrics{CPUTimeNanoseconds: 7, PeakRSSBytes: 11}, nil
	}
}
