// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSchedulePlanAdmitsIndependentWorkWithoutOvercommit(t *testing.T) {
	t.Parallel()

	plan := schedulerTestPlan()
	started := make(chan PlanCommandBinding, len(plan.Commands))
	release := make(chan struct{}, len(plan.Commands))
	finished := make(chan error, 1)
	var mutex sync.Mutex
	reservedCPU := 0
	maximumCPU := 0
	go func() {
		_, err := schedulePlan(t.Context(), plan, time.Now, func(_ context.Context, command PlanCommandBinding) (string, error) {
			mutex.Lock()
			reservedCPU += command.ReservedResources.CPUUnits
			maximumCPU = max(maximumCPU, reservedCPU)
			mutex.Unlock()
			started <- command
			<-release
			mutex.Lock()
			reservedCPU -= command.ReservedResources.CPUUnits
			mutex.Unlock()
			return command.WorkUnitID, nil
		})
		finished <- err
	}()
	first := <-started
	second := <-started
	if first.WorkUnitID == second.WorkUnitID {
		t.Fatalf("scheduler launched duplicate work unit %q", first.WorkUnitID)
	}
	release <- struct{}{}
	release <- struct{}{}
	if err := <-finished; err != nil {
		t.Fatalf("schedulePlan() error = %v", err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if maximumCPU != plan.Resources.CPUUnits || reservedCPU != 0 {
		t.Fatalf("reserved CPU maximum/final = %d/%d, want %d/0", maximumCPU, reservedCPU, plan.Resources.CPUUnits)
	}
}

func TestSchedulePlanDefersForMemoryAndExclusivity(t *testing.T) {
	t.Parallel()

	plan := schedulerTestPlan()
	plan.Resources.MemoryBytes = 1024
	plan.Commands[0].ExclusivityGroups = []string{"analyzer-cache"}
	plan.Commands[1].ExclusivityGroups = []string{"analyzer-cache"}
	assignPlanID(&plan)
	started := make(chan string, len(plan.Commands))
	release := make(chan struct{}, len(plan.Commands))
	finished := make(chan error, 1)
	go func() {
		_, err := schedulePlan(t.Context(), plan, time.Now, func(_ context.Context, command PlanCommandBinding) (struct{}, error) {
			started <- command.WorkUnitID
			<-release
			return struct{}{}, nil
		})
		finished <- err
	}()
	first := <-started
	select {
	case second := <-started:
		t.Fatalf("scheduler launched %q while exclusive/memory-bound %q was running", second, first)
	default:
	}
	release <- struct{}{}
	second := <-started
	if first == second {
		t.Fatalf("scheduler relaunched %q", first)
	}
	release <- struct{}{}
	if err := <-finished; err != nil {
		t.Fatalf("schedulePlan() error = %v", err)
	}
}

func TestSchedulePlanHonorsDependenciesAndFailsFast(t *testing.T) {
	t.Parallel()

	plan := schedulerTestPlan()
	plan.Dependencies[1].Requires = []string{plan.Commands[0].WorkUnitID}
	assignPlanID(&plan)
	started := make(chan string, len(plan.Commands))
	releaseFirst := make(chan struct{})
	finished := make(chan error, 1)
	wantErr := errors.New("producer failed")
	go func() {
		_, err := schedulePlan(t.Context(), plan, time.Now, func(ctx context.Context, command PlanCommandBinding) (struct{}, error) {
			started <- command.WorkUnitID
			if command.WorkUnitID == plan.Commands[0].WorkUnitID {
				<-releaseFirst
				return struct{}{}, wantErr
			}
			<-ctx.Done()
			return struct{}{}, context.Cause(ctx)
		})
		finished <- err
	}()
	first := <-started
	if first != plan.Commands[0].WorkUnitID {
		t.Fatalf("first work unit = %q, want dependency %q", first, plan.Commands[0].WorkUnitID)
	}
	select {
	case unexpected := <-started:
		t.Fatalf("scheduler launched dependent work unit %q before its dependency completed", unexpected)
	default:
	}
	close(releaseFirst)
	err := <-finished
	if !errors.Is(err, wantErr) {
		t.Fatalf("schedulePlan() error = %v, want %v", err, wantErr)
	}
	select {
	case unexpected := <-started:
		t.Fatalf("scheduler launched dependent work unit %q after fail-fast cancellation", unexpected)
	default:
	}
}

func TestSchedulePlanUsesDeterministicResourcePriority(t *testing.T) {
	t.Parallel()

	plan := schedulerTestPlan()
	plan.Resources.MaximumWorkers = 1
	plan.Commands[0].ReservedResources.CPUUnits = 1
	plan.Commands[0].ReservedResources.MemoryBytes = 512
	plan.Commands[1].ReservedResources.CPUUnits = 2
	plan.Commands[1].ReservedResources.MemoryBytes = 1024
	assignPlanID(&plan)
	var order []string
	_, err := schedulePlan(t.Context(), plan, time.Now, func(_ context.Context, command PlanCommandBinding) (struct{}, error) {
		order = append(order, command.WorkUnitID)
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatalf("schedulePlan() error = %v", err)
	}
	if got, want := order[0], plan.Commands[1].WorkUnitID; got != want {
		t.Fatalf("first execution = %q, want larger reservation %q", got, want)
	}
	if got, want := order[1], plan.Commands[0].WorkUnitID; got != want {
		t.Fatalf("second execution = %q, want %q", got, want)
	}
}

func TestSchedulePlanUsesMemoryThenCanonicalIDPriority(t *testing.T) {
	t.Parallel()

	plan := schedulerTestPlan()
	plan.Resources.MaximumWorkers = 1
	plan.Commands[0].ReservedResources.CPUUnits = 1
	plan.Commands[1].ReservedResources.CPUUnits = 1
	plan.Commands[0].ReservedResources.MemoryBytes = 512
	plan.Commands[1].ReservedResources.MemoryBytes = 1024
	assignPlanID(&plan)
	var order []string
	_, err := schedulePlan(t.Context(), plan, time.Now, func(_ context.Context, command PlanCommandBinding) (struct{}, error) {
		order = append(order, command.WorkUnitID)
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatalf("schedulePlan() error = %v", err)
	}
	if got, want := order[0], plan.Commands[1].WorkUnitID; got != want {
		t.Fatalf("memory-priority execution = %q, want %q", got, want)
	}

	plan.Commands[0].ReservedResources.MemoryBytes = 512
	plan.Commands[1].ReservedResources.MemoryBytes = 512
	assignPlanID(&plan)
	order = nil
	_, err = schedulePlan(t.Context(), plan, time.Now, func(_ context.Context, command PlanCommandBinding) (struct{}, error) {
		order = append(order, command.WorkUnitID)
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatalf("schedulePlan() canonical tie error = %v", err)
	}
	wantFirst := min(plan.Commands[0].WorkUnitID, plan.Commands[1].WorkUnitID)
	if order[0] != wantFirst {
		t.Fatalf("canonical tie execution = %q, want %q", order[0], wantFirst)
	}
}

func TestSchedulePlanSelectsCanonicalDirectFailure(t *testing.T) {
	t.Parallel()

	plan := schedulerTestPlan()
	lowerID := min(plan.Commands[0].WorkUnitID, plan.Commands[1].WorkUnitID)
	higherID := max(plan.Commands[0].WorkUnitID, plan.Commands[1].WorkUnitID)
	lowerErr := errors.New("lower failure")
	higherErr := errors.New("higher failure")
	results, err := schedulePlan(t.Context(), plan, time.Now, func(ctx context.Context, command PlanCommandBinding) (struct{}, error) {
		if command.WorkUnitID == lowerID {
			<-ctx.Done()
			return struct{}{}, lowerErr
		}
		return struct{}{}, higherErr
	})
	if !errors.Is(err, lowerErr) || errors.Is(err, higherErr) {
		t.Fatalf("schedulePlan() error = %v, want canonical %q error", err, lowerID)
	}
	if len(results) != len(plan.Commands) || results[0].WorkUnitID != lowerID || results[1].WorkUnitID != higherID {
		t.Fatalf("failure results are not canonically ordered: %#v", results)
	}
}

func TestSchedulePlanIgnoresFailFastCancellationFallout(t *testing.T) {
	t.Parallel()

	plan := schedulerTestPlan()
	lowerID := min(plan.Commands[0].WorkUnitID, plan.Commands[1].WorkUnitID)
	higherID := max(plan.Commands[0].WorkUnitID, plan.Commands[1].WorkUnitID)
	directErr := errors.New("direct failure")
	_, err := schedulePlan(t.Context(), plan, time.Now, func(ctx context.Context, command PlanCommandBinding) (struct{}, error) {
		if command.WorkUnitID == higherID {
			return struct{}{}, directErr
		}
		<-ctx.Done()
		return struct{}{}, context.Canceled
	})
	if !errors.Is(err, directErr) || strings.Contains(err.Error(), lowerID) {
		t.Fatalf("schedulePlan() error = %v, want direct failure from %q", err, higherID)
	}
}

func TestSchedulePlanCancellationWaitsForWorkerCleanup(t *testing.T) {
	t.Parallel()

	plan := schedulerTestPlan()
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{}, len(plan.Commands))
	cleaned := make(chan struct{}, len(plan.Commands))
	finished := make(chan error, 1)
	go func() {
		_, err := schedulePlan(ctx, plan, time.Now, func(workerCtx context.Context, _ PlanCommandBinding) (struct{}, error) {
			started <- struct{}{}
			<-workerCtx.Done()
			cleaned <- struct{}{}
			return struct{}{}, context.Cause(workerCtx)
		})
		finished <- err
	}()
	for range plan.Commands {
		<-started
	}
	cancel()
	for range plan.Commands {
		<-cleaned
	}
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("schedulePlan() error = %v, want context cancellation", err)
	}
}

func schedulerTestPlan() ExecutionPlan {
	plan := validExecutionPlan()
	plan.Dependencies[0].Requires = []string{}
	plan.Dependencies[1].Requires = []string{}
	assignPlanID(&plan)
	return plan
}
