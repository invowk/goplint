// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

type (
	// PlanSerialOptions configures serial reference execution of an immutable
	// execution plan.
	PlanSerialOptions struct {
		Root          string
		ReportPath    string
		TelemetryPath string
		Stdout        io.Writer
		Stderr        io.Writer
	}
)

// RunPlanSerial validates an immutable plan against the exact current inputs
// and executes its unsharded work units through the reference serial runner.
func RunPlanSerial(ctx context.Context, plan ExecutionPlan, options PlanSerialOptions) (Result, error) {
	runnerDeps := runnerDependencies{
		workspaceDigest: WorkspaceDigest,
		execute:         executeCommand,
		makeTempDir:     os.MkdirTemp,
		newRunID:        newRunID,
		now:             time.Now,
	}
	planDependencies := planDependencies{
		workspaceDigest: WorkspaceDigest,
		binaryDigest:    executableDigest,
		toolchain:       currentToolchainBinding,
	}
	return runPlanSerial(ctx, plan, options, runnerDeps, planDependencies)
}

// NormalizedRunReportJSON returns deterministic semantic comparison bytes with
// run-scoped identities removed while preserving command, input, population,
// observation, and verdict content.
func NormalizedRunReportJSON(report RunReport) ([]byte, error) {
	normalized := report
	normalized.RunID = ""
	normalized.Subgates = slices.Clone(report.Subgates)
	for index := range normalized.Subgates {
		normalized.Subgates[index].Populations = slices.Clone(normalized.Subgates[index].Populations)
		slices.SortFunc(normalized.Subgates[index].Populations, func(left, right Population) int {
			return strings.Compare(left.ID, right.ID)
		})
		// The report digest includes the deliberately unique run id.
		normalized.Subgates[index].ReportDigest = ""
	}
	slices.SortFunc(normalized.Subgates, func(left, right SubgateResult) int {
		return strings.Compare(left.ID, right.ID)
	})
	normalized.Observations = slices.Clone(report.Observations)
	for index := range normalized.Observations {
		normalized.Observations[index].Binding.RunID = ""
	}
	slices.SortFunc(normalized.Observations, func(left, right soundnessevidence.SemanticObservation) int {
		return strings.Compare(left.RegistrationID, right.RegistrationID)
	})
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode normalized soundness run report: %w", err)
	}
	return append(data, '\n'), nil
}

func runPlanSerial(
	ctx context.Context,
	plan ExecutionPlan,
	options PlanSerialOptions,
	runnerDeps runnerDependencies,
	planDeps planDependencies,
) (Result, error) {
	if err := validateCurrentPlan(ctx, plan, options.Root, planDeps); err != nil {
		return Result{}, err
	}
	return run(ctx, Options{
		Root:          options.Root,
		ManifestPath:  plan.Manifest.Path,
		Profile:       plan.Profile,
		ReportPath:    options.ReportPath,
		TelemetryPath: options.TelemetryPath,
		Stdout:        options.Stdout,
		Stderr:        options.Stderr,
		executionPlan: &plan,
	}, runnerDeps)
}

func validateCurrentPlan(
	ctx context.Context,
	plan ExecutionPlan,
	root string,
	planDeps planDependencies,
) error {
	if err := plan.Validate(); err != nil {
		return fmt.Errorf("validate current soundness plan: %w", err)
	}
	regenerated, err := generatePlan(ctx, PlanOptions{
		Root:         root,
		ManifestPath: plan.Manifest.Path,
		Profile:      plan.Profile,
		Resources:    plan.Resources,
	}, planDeps)
	if err != nil {
		return fmt.Errorf("regenerate current soundness plan: %w", err)
	}
	want, err := CanonicalPlanJSON(regenerated)
	if err != nil {
		return err
	}
	got, err := CanonicalPlanJSON(plan)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return errors.New("soundness plan does not match the exact current workspace, manifest, registry, toolchain, commands, binaries, or census")
	}
	return nil
}

func serialSubgatesFromPlan(plan ExecutionPlan) ([]Subgate, map[string]ResourceReservation, error) {
	if !plan.Resources.SerialReference {
		return nil, nil, errors.New("serial-reference execution requires a plan with serial_reference enabled")
	}
	return unshardedSubgatesFromPlan(plan)
}

func unshardedSubgatesFromPlan(plan ExecutionPlan) ([]Subgate, map[string]ResourceReservation, error) {
	orderedCommands, err := dependencyOrderedPlanCommands(plan)
	if err != nil {
		return nil, nil, err
	}
	reports := make(map[string]PlanExpectedReportBinding, len(plan.ExpectedReports))
	for _, report := range plan.ExpectedReports {
		reports[report.WorkUnitID] = report
	}
	result := make([]Subgate, 0, len(plan.Commands))
	reservations := make(map[string]ResourceReservation, len(plan.Commands))
	for _, command := range orderedCommands {
		if command.WorkUnitID != command.SubgateID {
			return nil, nil, fmt.Errorf("serial-reference work unit %q is sharded or does not preserve its subgate identity", command.WorkUnitID)
		}
		report, exists := reports[command.WorkUnitID]
		if !exists {
			return nil, nil, fmt.Errorf("serial-reference work unit %q has no expected report", command.WorkUnitID)
		}
		subgate := Subgate{
			ID:                       command.SubgateID,
			WorkingDirectory:         command.WorkingDirectory,
			Command:                  slices.Clone(command.Command),
			CPUUnits:                 command.ReservedResources.CPUUnits,
			EstimatedPeakMemoryBytes: command.ReservedResources.MemoryBytes,
			ExclusivityGroups:        slices.Clone(command.ExclusivityGroups),
			Distributable:            command.Distributable,
			ProfileIDs:               []ProfileID{plan.Profile},
			TimeoutSeconds:           command.TimeoutSeconds,
			ReportFile:               report.ReportFile,
			RequiredRegistrationIDs:  slices.Clone(report.RequiredRegistrationIDs),
			RequiredPopulations:      slices.Clone(report.RequiredPopulations),
		}
		commandDigest, err := CommandDigest(subgate)
		if err != nil {
			return nil, nil, err
		}
		if commandDigest != command.CommandDigest || command.CommandDigest != report.CommandDigest {
			return nil, nil, fmt.Errorf("serial-reference work unit %q command identity is inconsistent", command.WorkUnitID)
		}
		result = append(result, subgate)
		reservations[subgate.ID] = command.ReservedResources
	}
	return result, reservations, nil
}

func dependencyOrderedPlanCommands(plan ExecutionPlan) ([]PlanCommandBinding, error) {
	requires := make(map[string][]string, len(plan.Dependencies))
	for _, dependency := range plan.Dependencies {
		requires[dependency.WorkUnitID] = dependency.Requires
	}
	ordered := make([]PlanCommandBinding, 0, len(plan.Commands))
	completed := make(map[string]bool, len(plan.Commands))
	for len(ordered) < len(plan.Commands) {
		admitted := false
		for _, command := range plan.Commands {
			if completed[command.WorkUnitID] {
				continue
			}
			ready := true
			for _, dependencyID := range requires[command.WorkUnitID] {
				if !completed[dependencyID] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			ordered = append(ordered, command)
			completed[command.WorkUnitID] = true
			admitted = true
			break
		}
		if !admitted {
			return nil, errors.New("serial-reference plan has no dependency-ready work unit")
		}
	}
	return ordered, nil
}

func maximumReservedCPU(subgates []SubgateTelemetry) int {
	maximum := 0
	for _, subgate := range subgates {
		maximum = max(maximum, subgate.ReservedResources.CPUUnits)
	}
	return maximum
}

func maximumReservedMemory(subgates []SubgateTelemetry) int64 {
	var maximum int64
	for _, subgate := range subgates {
		maximum = max(maximum, subgate.ReservedResources.MemoryBytes)
	}
	return maximum
}
