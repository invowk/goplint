// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

// CaptureOptions selects the exact tree and destination for a retained proof.
type CaptureOptions struct {
	Root         string
	PathsPath    string
	PlanPath     string
	EvidencePath string
}

// Capture materializes the selected tree, executes every planned command, and
// publishes a format-v3 record. A failed command still produces a failed record.
func Capture(ctx context.Context, options CaptureOptions) (record Record, resultErr error) {
	root, pathsPath, planPath, evidencePath, err := resolveVerifyOptions(ctx, VerifyOptions(options))
	if err != nil {
		return Record{}, err
	}
	plan, err := LoadPlan(resolveFromRoot(root, planPath))
	if err != nil {
		return Record{}, err
	}
	paths, err := LoadPathSelection(resolveFromRoot(root, pathsPath))
	if err != nil {
		return Record{}, err
	}
	if err := validateProofInputsSelected(root, paths, planPath, pathsPath, plan); err != nil {
		return Record{}, err
	}
	if pathCoveredBySelection(root, paths, evidencePath) {
		return Record{}, fmt.Errorf("evidence output %q may not be part of the synthetic tree", evidencePath)
	}
	if err := os.MkdirAll(filepath.Dir(resolveFromRoot(root, evidencePath)), 0o755); err != nil {
		return Record{}, fmt.Errorf("create clean-tree evidence directory: %w", err)
	}
	exclusions := []string{evidencePath, evidencePath + ".tmp"}
	before, err := SnapshotCallerState(ctx, root, exclusions...)
	if err != nil {
		return Record{}, err
	}
	record = Record{
		FormatVersion: FormatVersion,
		Status:        "failed",
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	materialization, err := Materialize(ctx, root, pathsPath, true)
	if err != nil {
		return Record{}, err
	}
	closed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, materialization.Close(ctx))
		}
	}()
	record.Repository = materialization.Identity
	record.DiffCensus, err = collectDiffCensus(
		ctx,
		root,
		record.Repository.BaseCommit,
		paths,
		plan.DiffReview,
		exclusions,
	)
	if err != nil {
		return Record{}, err
	}
	syntheticPlan, err := LoadPlan(resolveFromRoot(materialization.Worktree, planPath))
	if err != nil {
		return Record{}, err
	}
	if !plansEqual(plan, syntheticPlan) {
		return Record{}, errors.New("selected synthetic plan differs from caller plan")
	}
	record.Inputs, err = collectInputs(materialization.Worktree, planPath, pathsPath, plan)
	if err != nil {
		return Record{}, err
	}
	record.Toolchain, err = collectToolchain(ctx, materialization.Worktree, plan)
	if err != nil {
		return Record{}, err
	}
	record.TaskLedgers, err = collectTaskLedgers(materialization.Worktree, plan)
	if err != nil {
		return Record{}, err
	}
	record.Counterexamples, err = collectCounterexamples(materialization.Worktree, plan)
	if err != nil {
		return Record{}, err
	}
	observationRoot := filepath.Join(materialization.tempRoot, "observations")
	if err := os.MkdirAll(observationRoot, 0o755); err != nil {
		return Record{}, fmt.Errorf("create clean-tree observation directory: %w", err)
	}
	commandEnv := cleanTreeCommandEnvironment(materialization.tempRoot)
	reportPath := filepath.Join(observationRoot, filepath.FromSlash(plan.AggregateReport.OutputFile))
	allPassed := true
	for _, command := range plan.Commands {
		environment := commandEnv
		if command.Name == plan.AggregateReport.CommandName {
			environment = replaceEnvironmentVariable(environment, soundnessgate.EnvReportPath, reportPath)
		}
		outcome := executePlannedCommand(ctx, materialization.Worktree, environment, command)
		record.Commands = append(record.Commands, outcome)
		if !outcome.Passed {
			allPassed = false
		}
	}
	record.AggregateReport, err = collectAggregateReport(
		ctx,
		materialization.Worktree,
		reportPath,
		plan.AggregateReport,
	)
	if err != nil {
		allPassed = false
	}
	var mutationErr error
	record.MutationProofs, mutationErr = collectMutationProofs(plan, record.AggregateReport.Report)
	if mutationErr != nil {
		allPassed = false
		err = errors.Join(err, mutationErr)
	}
	status, statusErr := gitOutput(ctx, materialization.Worktree, nil, nil, "status", "--porcelain=v1")
	if statusErr != nil {
		allPassed = false
		err = errors.Join(err, statusErr)
	} else if status != "" {
		allPassed = false
		err = errors.Join(err, fmt.Errorf("proof commands modified synthetic worktree: %s", status))
	}
	if closeErr := materialization.Close(ctx); closeErr != nil {
		allPassed = false
		err = errors.Join(err, closeErr)
	}
	closed = true
	cleanupCtx, cancelCleanup := detachedCleanupContext(ctx)
	after, snapshotErr := SnapshotCallerState(cleanupCtx, root, exclusions...)
	cancelCleanup()
	if snapshotErr != nil {
		allPassed = false
		err = errors.Join(err, snapshotErr)
	} else {
		record.Preservation = PreservationIdentity{
			IndexSHA256Before:    before.IndexSHA256,
			IndexSHA256After:     after.IndexSHA256,
			WorktreeSHA256Before: before.WorktreeSHA256,
			WorktreeSHA256After:  after.WorktreeSHA256,
		}
		if before != after {
			allPassed = false
			err = errors.Join(err, errors.New("clean-tree capture mutated caller index or worktree"))
		}
	}
	if allPassed {
		record.Status = "passed"
	}
	record.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if writeErr := WriteRecord(resolveFromRoot(root, evidencePath), record); writeErr != nil {
		return record, errors.Join(err, writeErr)
	}
	if !allPassed {
		if err == nil {
			err = errors.New("one or more clean-tree commands failed")
		}
		return record, err
	}
	return record, nil
}

// WriteRecord durably publishes a clean-tree record through a sibling file.
func WriteRecord(path string, record Record) (resultErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create evidence directory: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode clean-tree evidence: %w", err)
	}
	data = append(data, '\n')
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create temporary clean-tree evidence: %w", err)
	}
	fileOpen := true
	removeTemporary := true
	defer func() {
		if fileOpen {
			if closeErr := file.Close(); closeErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("close temporary clean-tree evidence: %w", closeErr))
			}
		}
		if removeTemporary {
			if removeErr := os.Remove(temporary); removeErr != nil && !os.IsNotExist(removeErr) {
				resultErr = errors.Join(resultErr, fmt.Errorf("remove temporary clean-tree evidence: %w", removeErr))
			}
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write clean-tree evidence: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync clean-tree evidence: %w", err)
	}
	closeErr := file.Close()
	fileOpen = false
	if closeErr != nil {
		return fmt.Errorf("close clean-tree evidence: %w", closeErr)
	}
	if err := os.Rename(temporary, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("replace old clean-tree evidence: %w", removeErr)
		}
		if retryErr := os.Rename(temporary, path); retryErr != nil {
			return fmt.Errorf("publish clean-tree evidence: %w", retryErr)
		}
	}
	removeTemporary = false
	return nil
}

func executePlannedCommand(
	parent context.Context,
	worktree string,
	environment []string,
	planned CommandPlan,
) CommandOutcome {
	ctx, cancel := context.WithTimeout(parent, time.Duration(planned.TimeoutMinutes)*time.Minute)
	defer cancel()
	directory := resolveFromRoot(worktree, planned.Directory)
	started := time.Now()
	output, err := runCommand(ctx, directory, environment, nil, planned.Args[0], planned.Args[1:]...)
	outcome := CommandOutcome{
		Name:         planned.Name,
		Directory:    planned.Directory,
		Args:         planned.Args,
		VectorSHA256: commandVectorDigest(planned.Directory, planned.Args),
		DurationMS:   time.Since(started).Milliseconds(),
		Log:          string(output),
		LogSHA256:    digestBytes(output),
		Passed:       err == nil,
	}
	if err != nil {
		outcome.ExitCode = -1
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			outcome.ExitCode = exitErr.ExitCode()
		}
	}
	return outcome
}

func cleanTreeCommandEnvironment(tempRoot string) []string {
	environment := os.Environ()
	environment = replaceEnvironmentVariable(environment, "GOLANGCI_LINT_CACHE", filepath.Join(tempRoot, "golangci-lint-cache"))
	for _, key := range []string{
		soundnessevidence.EnvRunID,
		soundnessevidence.EnvWorkspaceDigest,
		soundnessevidence.EnvManifestDigest,
		soundnessevidence.EnvCommandDigest,
		soundnessevidence.EnvSubgateID,
		soundnessevidence.EnvEvidenceDir,
		soundnessgate.EnvReportPath,
		soundnessgate.EnvSubgateReportPath,
	} {
		environment = removeEnvironmentVariable(environment, key)
	}
	return environment
}

func plansEqual(left, right Plan) bool {
	return reflect.DeepEqual(left, right)
}

func removeEnvironmentVariable(environment []string, key string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return result
}
