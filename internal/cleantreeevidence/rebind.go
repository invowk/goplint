// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/invowk/goplint/internal/profileownership"
)

// Rebind refreshes the prose identity of a retained record without executing
// any assurance profile. It recomputes both class digests, revalidates the
// task ledgers and diff census against the reviewed plan, carries the retained
// aggregate report forward untouched, and records the carried-forward
// provenance. Any semantic-content drift fails closed with the drifted paths
// named; the retained record is never modified on failure.
func Rebind(ctx context.Context, options CaptureOptions) (record Record, resultErr error) {
	if options.ReuseAggregateReportPath != "" {
		return Record{}, errors.New(
			"re-binding never executes the aggregate command, so a reused aggregate report selection is not applicable",
		)
	}
	root, pathsPath, planPath, evidencePath, err := resolveVerifyOptions(ctx, options.verifyOptions())
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
		return Record{}, fmt.Errorf("evidence record %q may not be part of the synthetic tree", evidencePath)
	}
	retained, err := LoadRecord(resolveFromRoot(root, evidencePath))
	if err != nil {
		return Record{}, err
	}
	if err := validateRecordHeader(retained); err != nil {
		return Record{}, err
	}
	// Re-binding carries the retained aggregate report forward under the
	// "re-bound" kind. Carrying a reused record forward would launder a
	// never-executed aggregate into a kind that verification accepts by
	// default, so it is refused outright.
	if retained.Provenance.Kind == ProvenanceReused {
		return Record{}, fmt.Errorf(
			"retained record provenance kind %q: a reused aggregate report may not be re-bound, because re-binding "+
				"would present a never-executed aggregate as %q; regenerate with fresh gate execution",
			ProvenanceReused,
			ProvenanceRebound,
		)
	}
	if err := requireRetainedBaseAncestor(ctx, root, retained.Repository.BaseCommit); err != nil {
		return Record{}, err
	}
	exclusions := []string{evidencePath, evidencePath + ".tmp"}
	before, err := SnapshotCallerState(ctx, root, exclusions...)
	if err != nil {
		return Record{}, err
	}
	record = retained
	record.Status = "failed"
	record.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	materialization, err := Materialize(ctx, root, pathsPath, plan.OwnershipManifestPath, true)
	if err != nil {
		return Record{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, materialization.Close(ctx))
	}()
	if materialization.Identity.SemanticTreeDigest != retained.Repository.SemanticTreeDigest {
		drifted, driftErr := driftedSemanticPaths(
			ctx,
			root,
			plan.OwnershipManifestPath,
			retained.Repository.SyntheticTree,
			materialization.Identity.SyntheticTree,
		)
		if driftErr != nil {
			return Record{}, errors.Join(
				errors.New("semantic content drifted since the retained record; re-binding is refused"),
				driftErr,
			)
		}
		return Record{}, fmt.Errorf(
			"semantic content drifted since the retained record; re-binding is refused and full regeneration with "+
				"fresh gate execution is required: drifted semantic paths: %s",
			strings.Join(drifted, ", "),
		)
	}
	record.Repository = materialization.Identity
	record.DiffCensus, err = collectDiffCensus(ctx, root, record.Repository.BaseCommit, paths, plan.DiffReview, exclusions)
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
	if !reflect.DeepEqual(record.Toolchain, retained.Toolchain) {
		return Record{}, errors.New(
			"toolchain identity drifted since the retained record; re-binding is refused because the retained " +
				"evidence binds the producing toolchain",
		)
	}
	record.TaskLedgers, err = collectTaskLedgers(materialization.Worktree, plan)
	if err != nil {
		return Record{}, err
	}
	record.Counterexamples, err = collectCounterexamples(materialization.Worktree, plan)
	if err != nil {
		return Record{}, err
	}
	reportDigest, err := digestJSON(retained.AggregateReport.Report)
	if err != nil {
		return Record{}, err
	}
	if reportDigest != retained.AggregateReport.SHA256 {
		return Record{}, errors.New("retained aggregate report bytes do not match their recorded digest")
	}
	aggregateIdentity, err := validateAggregateReport(
		ctx,
		materialization.Worktree,
		plan.AggregateReport,
		retained.AggregateReport.Report,
		retained.Provenance.AggregateWorkspaceDigest,
	)
	if err != nil {
		return Record{}, err
	}
	if !reflect.DeepEqual(aggregateIdentity, retained.AggregateReport) {
		return Record{}, errors.New("retained aggregate report identity drifted; re-binding is refused")
	}
	record.AggregateReport = retained.AggregateReport
	record.MutationProofs, err = collectMutationProofs(plan, record.AggregateReport.Report)
	if err != nil {
		return Record{}, err
	}
	record.Provenance = ProvenanceIdentity{
		Kind:                        ProvenanceRebound,
		AggregateSemanticTreeDigest: retained.Provenance.AggregateSemanticTreeDigest,
		AggregateWorkspaceDigest:    retained.Provenance.AggregateWorkspaceDigest,
		CarriedReportSHA256:         retained.AggregateReport.SHA256,
		PreviousProseDigest:         retained.Repository.ProseTreeDigest,
	}
	if record.Provenance.AggregateSemanticTreeDigest != record.Repository.SemanticTreeDigest {
		return Record{}, errors.New(
			"retained provenance does not attribute the aggregate report to the retained semantic content",
		)
	}
	if closeErr := materialization.Close(ctx); closeErr != nil {
		return Record{}, closeErr
	}
	cleanupCtx, cancelCleanup := detachedCleanupContext(ctx)
	after, snapshotErr := SnapshotCallerState(cleanupCtx, root, exclusions...)
	cancelCleanup()
	if snapshotErr != nil {
		return Record{}, snapshotErr
	}
	record.Preservation = PreservationIdentity{
		IndexSHA256Before:    before.IndexSHA256,
		IndexSHA256After:     after.IndexSHA256,
		WorktreeSHA256Before: before.WorktreeSHA256,
		WorktreeSHA256After:  after.WorktreeSHA256,
	}
	if before != after {
		return Record{}, errors.New("clean-tree re-binding mutated caller index or worktree")
	}
	record.Status = "passed"
	record.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := WriteRecord(resolveFromRoot(root, evidencePath), record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// driftedSemanticPaths names every non-documentation path that differs
// between the retained and current synthetic trees, classified through the
// ownership manifest of the current tree.
func driftedSemanticPaths(
	ctx context.Context,
	root, ownershipManifestPath, retainedTree, currentTree string,
) ([]string, error) {
	manifestData, err := runCommand(ctx, root, nil, nil, "git", "show", currentTree+":"+ownershipManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read ownership manifest from current synthetic tree: %w", err)
	}
	manifest, err := profileownership.Decode(manifestData)
	if err != nil {
		return nil, fmt.Errorf("decode current ownership manifest: %w", err)
	}
	output, err := runCommand(ctx, root, nil, nil, "git", "diff", "--name-only", "-z", retainedTree, currentTree)
	if err != nil {
		return nil, fmt.Errorf("enumerate drifted synthetic paths: %w", err)
	}
	var drifted []string
	for rawPath := range strings.SplitSeq(string(output), "\x00") {
		path := filepath.ToSlash(strings.TrimSpace(rawPath))
		if path == "" {
			continue
		}
		if class, matched := manifest.ClassForPath(path); !matched || class != profileownership.ClassDocumentation {
			drifted = append(drifted, path)
		}
	}
	slices.Sort(drifted)
	return slices.Compact(drifted), nil
}
