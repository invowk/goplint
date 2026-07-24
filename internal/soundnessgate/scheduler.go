// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

var errSchedulerFailFast = errors.New("soundness scheduler fail-fast cancellation")

type (
	scheduledWorkResult[T any] struct {
		WorkUnitID string
		QueuedAt   time.Time
		StartedAt  time.Time
		FinishedAt time.Time
		Value      T
		Err        error
	}

	schedulerCompletion[T any] struct {
		command    PlanCommandBinding
		queuedAt   time.Time
		startedAt  time.Time
		finishedAt time.Time
		value      T
		err        error
	}

	schedulerState struct {
		availableCPU    int
		availableMemory int64
		running         int
		exclusive       map[string]bool
	}
)

func schedulePlan[T any](
	ctx context.Context,
	plan ExecutionPlan,
	now func() time.Time,
	execute func(context.Context, PlanCommandBinding) (T, error),
) ([]scheduledWorkResult[T], error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	queuedAt := now().UTC()
	commands := slices.Clone(plan.Commands)
	slices.SortFunc(commands, compareSchedulerPriority)
	dependencies := make(map[string][]string, len(plan.Dependencies))
	for _, dependency := range plan.Dependencies {
		dependencies[dependency.WorkUnitID] = dependency.Requires
	}
	pending := make(map[string]PlanCommandBinding, len(commands))
	for _, command := range commands {
		pending[command.WorkUnitID] = command
	}
	completed := make(map[string]bool, len(commands))
	state := schedulerState{
		availableCPU:    plan.Resources.CPUUnits,
		availableMemory: plan.Resources.MemoryBytes,
		exclusive:       make(map[string]bool),
	}
	completionChannel := make(chan schedulerCompletion[T], len(commands))
	results := make([]scheduledWorkResult[T], 0, len(commands))
	directFailures := make(map[string]error)
	internalFailure := false
	for len(pending) != 0 || state.running != 0 {
		launched := false
		if !internalFailure && ctx.Err() == nil {
			for _, command := range commands {
				if _, exists := pending[command.WorkUnitID]; !exists {
					continue
				}
				if !dependenciesComplete(dependencies[command.WorkUnitID], completed) || !state.canAdmit(plan, command) {
					continue
				}
				delete(pending, command.WorkUnitID)
				state.reserve(command)
				launched = true
				startedAt := now().UTC()
				go func() {
					value, err := execute(ctx, command)
					completionChannel <- schedulerCompletion[T]{
						command: command, queuedAt: queuedAt, startedAt: startedAt,
						finishedAt: now().UTC(), value: value, err: err,
					}
				}()
			}
		}
		if state.running == 0 {
			if internalFailure {
				break
			}
			if ctx.Err() != nil {
				return nil, fmt.Errorf("soundness scheduler context ended: %w", context.Cause(ctx))
			}
			if len(pending) != 0 && !launched {
				return nil, errors.New("soundness scheduler cannot admit any remaining dependency-ready work unit")
			}
			continue
		}
		completion := <-completionChannel
		state.release(completion.command)
		results = append(results, scheduledWorkResult[T]{
			WorkUnitID: completion.command.WorkUnitID,
			QueuedAt:   completion.queuedAt, StartedAt: completion.startedAt,
			FinishedAt: completion.finishedAt, Value: completion.value, Err: completion.err,
		})
		if completion.err != nil {
			cancellationFallout := internalFailure &&
				(errors.Is(completion.err, context.Canceled) || errors.Is(completion.err, errSchedulerFailFast))
			if !cancellationFallout {
				directFailures[completion.command.WorkUnitID] = completion.err
			}
			if !internalFailure && ctx.Err() == nil {
				internalFailure = true
				cancel(errSchedulerFailFast)
			}
		}
		if completion.err == nil {
			completed[completion.command.WorkUnitID] = true
		}
	}
	slices.SortFunc(results, func(left, right scheduledWorkResult[T]) int {
		return comparePlanWorkUnitIDs(left.WorkUnitID, right.WorkUnitID)
	})
	if len(directFailures) != 0 {
		failureIDs := make([]string, 0, len(directFailures))
		for workUnitID := range directFailures {
			failureIDs = append(failureIDs, workUnitID)
		}
		slices.Sort(failureIDs)
		workUnitID := failureIDs[0]
		return results, fmt.Errorf("soundness work unit %q failed: %w", workUnitID, directFailures[workUnitID])
	}
	if ctx.Err() != nil {
		return results, fmt.Errorf("soundness scheduler context ended: %w", context.Cause(ctx))
	}
	if len(completed) != len(commands) {
		return results, fmt.Errorf("soundness scheduler completed %d of %d required work units", len(completed), len(commands))
	}
	return results, nil
}

func compareSchedulerPriority(left, right PlanCommandBinding) int {
	if left.ReservedResources.CPUUnits != right.ReservedResources.CPUUnits {
		return right.ReservedResources.CPUUnits - left.ReservedResources.CPUUnits
	}
	if left.ReservedResources.MemoryBytes != right.ReservedResources.MemoryBytes {
		if left.ReservedResources.MemoryBytes > right.ReservedResources.MemoryBytes {
			return -1
		}
		return 1
	}
	return comparePlanWorkUnitIDs(left.WorkUnitID, right.WorkUnitID)
}

func (state schedulerState) canAdmit(plan ExecutionPlan, command PlanCommandBinding) bool {
	if state.running >= plan.Resources.MaximumWorkers ||
		command.ReservedResources.CPUUnits > state.availableCPU ||
		command.ReservedResources.MemoryBytes > state.availableMemory {
		return false
	}
	for _, group := range command.ExclusivityGroups {
		if state.exclusive[group] {
			return false
		}
	}
	return true
}

func (state *schedulerState) reserve(command PlanCommandBinding) {
	state.availableCPU -= command.ReservedResources.CPUUnits
	state.availableMemory -= command.ReservedResources.MemoryBytes
	state.running++
	for _, group := range command.ExclusivityGroups {
		state.exclusive[group] = true
	}
}

func (state *schedulerState) release(command PlanCommandBinding) {
	state.availableCPU += command.ReservedResources.CPUUnits
	state.availableMemory += command.ReservedResources.MemoryBytes
	state.running--
	for _, group := range command.ExclusivityGroups {
		delete(state.exclusive, group)
	}
}

func dependenciesComplete(required []string, completed map[string]bool) bool {
	for _, dependencyID := range required {
		if !completed[dependencyID] {
			return false
		}
	}
	return true
}

func comparePlanWorkUnitIDs(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
