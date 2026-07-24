// SPDX-License-Identifier: MPL-2.0

// Command harness-parity proves that the serial-reference and parallel plan
// executors produce byte-identical normalized evidence over a reviewed
// fixture manifest. It is the orchestration-integrity core of the harness
// assurance tier: the fixture is small enough to execute within a one-minute
// budget, and any divergence, lost observation, or population mismatch fails
// the comparison with the exact normalized difference.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

const (
	fixtureManifestPath   = "testdata/parity/soundness-gate.fixture.json"
	fixtureRegistryPath   = "testdata/parity/semantic-evidence.fixture.json"
	fixtureRegistrationID = "parity-fixture.artifact-parity"

	parityPopulationID = "parity-fixture-comparisons"
	parityMemberID     = "serial-parallel-fixture"

	fixtureCPUUnits       = 2
	fixtureMemoryBytes    = 2 * 1024 * 1024 * 1024
	fixtureMaximumWorkers = 2
	fixtureRunnerClass    = "parity-fixture"
)

func main() {
	fixtureSubgate := flag.String("fixture-subgate", "", "internal fixture producer mode (alpha, beta, or freshness)")
	budgetSeconds := flag.Int("fixture-budget-seconds", 60, "wall budget for both fixture executions")
	root := flag.String("root", ".", "goplint module root containing the parity fixture")
	flag.Parse()
	ctx := context.Background()
	if *fixtureSubgate != "" {
		if err := runFixtureSubgate(ctx, *fixtureSubgate); err != nil {
			fatal(err)
		}
		return
	}
	if err := runParity(ctx, *root, time.Duration(*budgetSeconds)*time.Second); err != nil {
		fatal(err)
	}
}

func runParity(ctx context.Context, root string, budget time.Duration) (resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	temporaryRoot, err := os.MkdirTemp("", "goplint-harness-parity-*")
	if err != nil {
		return fmt.Errorf("create parity report directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(temporaryRoot); removeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove parity report directory: %w", removeErr))
		}
	}()
	reference, err := executeFixtureLane(ctx, root, true, filepath.Join(temporaryRoot, "serial-report.json"))
	if err != nil {
		return fmt.Errorf("execute serial-reference fixture lane: %w", err)
	}
	candidate, err := executeFixtureLane(ctx, root, false, filepath.Join(temporaryRoot, "parallel-report.json"))
	if err != nil {
		return fmt.Errorf("execute parallel fixture lane: %w", err)
	}
	if err := soundnessgate.CompareNormalizedRunReports(reference, candidate); err != nil {
		return fmt.Errorf("serial and parallel executors diverged on the parity fixture: %w", err)
	}
	populations, err := soundnessgate.PopulationsFromObservedMembers([]soundnessgate.ObservedMember{
		{PopulationID: parityPopulationID, MemberID: parityMemberID},
	})
	if err != nil {
		return fmt.Errorf("derive parity populations: %w", err)
	}
	if _, err := soundnessgate.EmitReportFromEnvironment(ctx, populations); err != nil {
		return fmt.Errorf("publish parity subgate report: %w", err)
	}
	fmt.Println("serial and parallel executors produced byte-identical normalized fixture evidence")
	return nil
}

func executeFixtureLane(
	ctx context.Context,
	root string,
	serialReference bool,
	reportPath string,
) (soundnessgate.RunReport, error) {
	budget, err := soundnessgate.DiscoverResourceBudget(soundnessgate.ResourceOverrides{
		CPUUnits:       fixtureCPUUnits,
		MemoryBytes:    fixtureMemoryBytes,
		MaximumWorkers: fixtureMaximumWorkers,
		RunnerClass:    fixtureRunnerClass,
	})
	if err != nil {
		return soundnessgate.RunReport{}, fmt.Errorf("discover fixture resource budget: %w", err)
	}
	budget.SerialReference = serialReference
	plan, err := soundnessgate.GeneratePlan(ctx, soundnessgate.PlanOptions{
		Root:         root,
		ManifestPath: fixtureManifestPath,
		Profile:      soundnessgate.ProfileSemantic,
		Resources:    budget,
	})
	if err != nil {
		return soundnessgate.RunReport{}, fmt.Errorf("generate fixture execution plan: %w", err)
	}
	if serialReference {
		_, err = soundnessgate.RunPlanSerial(ctx, plan, soundnessgate.PlanSerialOptions{
			Root: root, ReportPath: reportPath, Stdout: os.Stdout, Stderr: os.Stderr,
		})
	} else {
		_, err = soundnessgate.RunPlanParallel(ctx, plan, soundnessgate.PlanParallelOptions{
			Root: root, ReportPath: reportPath, Stdout: os.Stdout, Stderr: os.Stderr,
		})
	}
	if err != nil {
		return soundnessgate.RunReport{}, fmt.Errorf("execute fixture plan: %w", err)
	}
	report, err := soundnessgate.LoadRunReport(ctx, reportPath)
	if err != nil {
		return soundnessgate.RunReport{}, fmt.Errorf("load fixture run report: %w", err)
	}
	return report, nil
}

func runFixtureSubgate(ctx context.Context, name string) error {
	switch name {
	case "alpha":
		if err := emitFixtureObservation(ctx); err != nil {
			return err
		}
		return emitFixtureReport(ctx, []soundnessgate.ObservedMember{
			{PopulationID: "fixture-cases", MemberID: "case-alpha-001"},
			{PopulationID: "fixture-cases", MemberID: "case-alpha-002"},
		})
	case "beta":
		return emitFixtureReport(ctx, []soundnessgate.ObservedMember{
			{PopulationID: "fixture-echoes", MemberID: "echo-001"},
		})
	case "freshness":
		return emitFixtureReport(ctx, []soundnessgate.ObservedMember{
			{PopulationID: "verified-clean-tree-records", MemberID: "fixture-tree"},
		})
	default:
		return fmt.Errorf("fixture subgate %q is unknown; want alpha, beta, or freshness", name)
	}
}

func emitFixtureObservation(ctx context.Context) error {
	registry, err := soundnessevidence.LoadRegistry(ctx, fixtureRegistryPath)
	if err != nil {
		return fmt.Errorf("load parity fixture registry: %w", err)
	}
	index := slices.IndexFunc(registry.Registrations, func(registration soundnessevidence.Registration) bool {
		return registration.ID == fixtureRegistrationID
	})
	if index < 0 {
		return fmt.Errorf("parity fixture registry omits registration %q", fixtureRegistrationID)
	}
	registration := registry.Registrations[index]
	cases := []soundnessevidence.SemanticCase{
		{
			ID:                 registration.ID + "/case-001",
			Category:           registration.Category,
			Layer:              registration.Layer,
			FeatureID:          registration.FeatureID,
			Boundary:           registration.Boundary,
			ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(registration.Boundary),
			Outcome:            registration.Expected.Outcome,
			DiagnosticCount:    registration.Expected.Diagnostics.Minimum,
			Stages:             slices.Clone(registration.Expected.RequiredStages),
			Properties:         slices.Clone(registration.Expected.RequiredProperties),
			Dimensions:         slices.Clone(registration.Expected.RequiredDimensions),
		},
	}
	observation, err := soundnessevidence.ObservationFromCases(
		registration.ID,
		registration.ProducerID,
		registration.TestID,
		cases,
	)
	if err != nil {
		return fmt.Errorf("build parity fixture observation: %w", err)
	}
	if _, err := soundnessevidence.EmitObservationFromEnvironment(ctx, observation); err != nil {
		return fmt.Errorf("publish parity fixture observation: %w", err)
	}
	return nil
}

func emitFixtureReport(ctx context.Context, members []soundnessgate.ObservedMember) error {
	populations, err := soundnessgate.PopulationsFromObservedMembers(members)
	if err != nil {
		return fmt.Errorf("derive fixture populations: %w", err)
	}
	if _, err := soundnessgate.EmitReportFromEnvironment(ctx, populations); err != nil {
		return fmt.Errorf("publish fixture subgate report: %w", err)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "goplint harness parity:", err)
	os.Exit(1)
}
