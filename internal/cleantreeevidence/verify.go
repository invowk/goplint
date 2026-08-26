// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/invowk/goplint/internal/soundnessevidence"
)

// AllowReusedAggregateFlag is the exact opt-in flag name that makes the
// freshness verifier accept a reused-aggregate record. It is exported so the
// verifier command and the completion-containment test bind one string: the
// whole containment argument is that no reviewed gate command passes it.
const AllowReusedAggregateFlag = "allow-reused-aggregate"

// VerifyOptions selects the repository, reviewed paths, plan, and retained
// record checked by Verify. AllowReusedAggregate is an explicit local-iteration
// opt-in: without it, a record whose aggregate command was reused instead of
// executed is refused, so completion and release claims always rest on fresh
// aggregate execution.
type VerifyOptions struct {
	Root                 string
	PathsPath            string
	PlanPath             string
	EvidencePath         string
	AllowReusedAggregate bool
}

// Verify recomputes every freshness identity without modifying the caller's
// real index or worktree.
func Verify(ctx context.Context, options VerifyOptions) (resultErr error) {
	root, pathsPath, planPath, evidencePath, err := resolveVerifyOptions(ctx, options)
	if err != nil {
		return err
	}
	before, err := SnapshotCallerState(ctx, root)
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		defer cancel()
		after, snapshotErr := SnapshotCallerState(cleanupCtx, root)
		if snapshotErr != nil {
			resultErr = errors.Join(resultErr, snapshotErr)
			return
		}
		if before != after {
			resultErr = errors.Join(resultErr, fmt.Errorf(
				"clean-tree verification mutated caller state: before=%+v after=%+v",
				before,
				after,
			))
		}
	}()

	plan, err := LoadPlan(resolveFromRoot(root, planPath))
	if err != nil {
		return err
	}
	paths, err := LoadPathSelection(resolveFromRoot(root, pathsPath))
	if err != nil {
		return err
	}
	if err := validateProofInputsSelected(root, paths, planPath, pathsPath, plan); err != nil {
		return err
	}
	if pathCoveredBySelection(root, paths, evidencePath) {
		return fmt.Errorf("evidence record %q may not be part of the synthetic tree", evidencePath)
	}
	record, err := LoadRecord(resolveFromRoot(root, evidencePath))
	if err != nil {
		return err
	}
	if err := validateRecordHeader(record); err != nil {
		return err
	}
	if err := requireFreshAggregateExecution(record, options.AllowReusedAggregate); err != nil {
		return err
	}
	if err := requireRetainedBaseAncestor(ctx, root, record.Repository.BaseCommit); err != nil {
		return err
	}
	diffCensus, err := collectDiffCensus(
		ctx,
		root,
		record.Repository.BaseCommit,
		paths,
		plan.DiffReview,
		[]string{evidencePath, evidencePath + ".tmp"},
	)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(record.DiffCensus, diffCensus) {
		return fmt.Errorf("retained complete-diff census is stale: got %+v, current %+v", record.DiffCensus, diffCensus)
	}
	materialization, err := materializeFromBase(
		ctx,
		root,
		pathsPath,
		plan.OwnershipManifestPath,
		record.Repository.BaseCommit,
		true,
	)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, materialization.Close(ctx))
	}()
	if !reflect.DeepEqual(record.Repository, materialization.Identity) {
		if record.Repository.SemanticTreeDigest == materialization.Identity.SemanticTreeDigest {
			return fmt.Errorf(
				"repository identity is prose-stale while semantic content is unchanged: re-bind the retained record "+
					"with 'make rebind-goplint-clean-tree-evidence': got %+v, current %+v",
				record.Repository,
				materialization.Identity,
			)
		}
		return fmt.Errorf("repository identity is stale: got %+v, current %+v", record.Repository, materialization.Identity)
	}
	if err := validateProvenance(record); err != nil {
		return err
	}
	inputs, err := collectInputs(root, planPath, pathsPath, plan)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(record.Inputs, inputs) {
		return errors.New("retained input identities do not match current proof inputs")
	}
	toolchain, err := collectToolchain(ctx, root, plan)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(record.Toolchain, toolchain) {
		return errors.New("retained toolchain identities do not match current tools or constraints")
	}
	taskLedgers, err := collectTaskLedgers(root, plan)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(record.TaskLedgers, taskLedgers) {
		return errors.New("retained task-ledger identities do not match exact current checkbox state")
	}
	counterexamples, err := collectCounterexamples(root, plan)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(record.Counterexamples, counterexamples) {
		return errors.New("retained counterexample inventory does not match current reviewed inventory")
	}
	if err := verifyCommands(plan, record.Commands); err != nil {
		return err
	}
	if err := requireProvenanceMatchesAggregateLog(plan, record); err != nil {
		return err
	}
	expectedWorkspaceDigest := ""
	if record.Provenance.Kind == ProvenanceRebound {
		expectedWorkspaceDigest = record.Provenance.AggregateWorkspaceDigest
	}
	aggregateReport, err := validateAggregateReport(
		ctx,
		materialization.Worktree,
		plan.AggregateReport,
		record.AggregateReport.Report,
		expectedWorkspaceDigest,
	)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(record.AggregateReport, aggregateReport) {
		return errors.New("retained aggregate report identity is stale")
	}
	mutationProofs, err := collectMutationProofs(plan, record.AggregateReport.Report)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(record.MutationProofs, mutationProofs) {
		return errors.New("retained causal mutation proof does not match validated observations")
	}
	if err := verifyMutationProofs(plan, record); err != nil {
		return err
	}
	return nil
}

// requireFreshAggregateExecution refuses a reused-aggregate record unless the
// caller explicitly opted in. The refusal is deliberately early and independent
// of every content identity: a reused record can be perfectly fresh with
// respect to the tree and still not be a completion claim, because its
// aggregate populations were never re-executed under this verifier's eyes.
func requireFreshAggregateExecution(record Record, allowReused bool) error {
	if record.Provenance.Kind != ProvenanceReused || allowReused {
		return nil
	}
	return fmt.Errorf(
		"clean-tree record provenance kind %q: its aggregate command was reused from a caller-provided report "+
			"instead of executed, so it cannot support a completion or release claim; regenerate with "+
			"'make generate-goplint-clean-tree-evidence', or pass -allow-reused-aggregate to accept it for local "+
			"iteration only",
		record.Provenance.Kind,
	)
}

// requireProvenanceMatchesAggregateLog requires the reused provenance kind and
// the reuse marker in the aggregate command log to agree in both directions.
// The retained log and its digest are the only record members that witness how
// the aggregate outcome was obtained, so without this equivalence a single
// string edit to provenance.kind would silently downgrade a reused record into
// one the verifier accepts by default, and a marker-carrying log could claim
// execution.
func requireProvenanceMatchesAggregateLog(plan Plan, record Record) error {
	marked := false
	found := false
	for _, outcome := range record.Commands {
		if outcome.Name != plan.AggregateReport.CommandName {
			continue
		}
		found = true
		marked = strings.HasPrefix(outcome.Log, reusedAggregateReportPrefix)
	}
	if !found {
		return fmt.Errorf("record has no outcome for aggregate command %q", plan.AggregateReport.CommandName)
	}
	reused := record.Provenance.Kind == ProvenanceReused
	if reused == marked {
		return nil
	}
	if reused {
		return fmt.Errorf(
			"clean-tree provenance kind %q, but the aggregate command log does not record reuse",
			record.Provenance.Kind,
		)
	}
	return fmt.Errorf(
		"clean-tree provenance kind %q, but the aggregate command log records a reused report: a reused record may "+
			"not be presented as executed",
		record.Provenance.Kind,
	)
}

// validateProvenance requires the retained aggregate report to be
// attributable to the exact semantic content of the verified tree. The
// attribution chain is: the report binds the workspace that produced it, the
// provenance binds that workspace to a semantic-content digest, and the
// verified repository identity must carry the same semantic digest.
func validateProvenance(record Record) error {
	provenance := record.Provenance
	if provenance.Kind != ProvenanceGenerated && provenance.Kind != ProvenanceRebound &&
		provenance.Kind != ProvenanceReused {
		return fmt.Errorf("clean-tree provenance kind %q is unknown", provenance.Kind)
	}
	digests := []struct {
		name  string
		value string
	}{
		{name: "aggregate_semantic_tree_digest", value: provenance.AggregateSemanticTreeDigest},
		{name: "aggregate_workspace_digest", value: provenance.AggregateWorkspaceDigest},
		{name: "carried_report_sha256", value: provenance.CarriedReportSHA256},
	}
	for _, digest := range digests {
		if err := soundnessevidence.ValidateDigest("clean-tree provenance "+digest.name, digest.value); err != nil {
			return fmt.Errorf("validate clean-tree provenance digest %q: %w", digest.name, err)
		}
	}
	if provenance.AggregateSemanticTreeDigest != record.Repository.SemanticTreeDigest {
		return fmt.Errorf(
			"retained aggregate report is bound to semantic content %s, but the verified tree has %s: "+
				"regenerate the record with fresh gate execution",
			provenance.AggregateSemanticTreeDigest,
			record.Repository.SemanticTreeDigest,
		)
	}
	if provenance.AggregateWorkspaceDigest != record.AggregateReport.Report.WorkspaceDigest {
		return errors.New("clean-tree provenance workspace digest does not match the retained aggregate report")
	}
	if provenance.CarriedReportSHA256 != record.AggregateReport.SHA256 {
		return errors.New("clean-tree provenance report digest does not match the retained aggregate report")
	}
	switch provenance.Kind {
	case ProvenanceGenerated, ProvenanceReused:
		if provenance.PreviousProseDigest != "" {
			return fmt.Errorf("%s clean-tree provenance must not carry a previous prose digest", provenance.Kind)
		}
	case ProvenanceRebound:
		if err := soundnessevidence.ValidateDigest(
			"clean-tree provenance previous_prose_digest",
			provenance.PreviousProseDigest,
		); err != nil {
			return fmt.Errorf("validate clean-tree provenance digest %q: %w", "previous_prose_digest", err)
		}
	}
	return nil
}

func requireRetainedBaseAncestor(ctx context.Context, root, baseCommit string) error {
	if strings.TrimSpace(baseCommit) == "" {
		return errors.New("retained clean-tree base commit is empty")
	}
	if _, err := runCommand(ctx, root, nil, nil, "git", "merge-base", "--is-ancestor", baseCommit, "HEAD"); err != nil {
		return fmt.Errorf("retained clean-tree base %s is not an ancestor of HEAD: %w", baseCommit, err)
	}
	return nil
}

func resolveVerifyOptions(ctx context.Context, options VerifyOptions) (string, string, string, string, error) {
	root, err := repositoryRoot(ctx, options.Root)
	if err != nil {
		return "", "", "", "", err
	}
	pathsPath, err := relativeOptionPath(root, options.PathsPath, "paths")
	if err != nil {
		return "", "", "", "", err
	}
	planPath, err := relativeOptionPath(root, options.PlanPath, "plan")
	if err != nil {
		return "", "", "", "", err
	}
	evidencePath, err := relativeOptionPath(root, options.EvidencePath, "evidence")
	if err != nil {
		return "", "", "", "", err
	}
	return root, pathsPath, planPath, evidencePath, nil
}

func relativeOptionPath(root, path, label string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%s path is required", label)
	}
	if !filepath.IsAbs(path) {
		if err := validateRepoPath(path); err != nil {
			return "", fmt.Errorf("%s path: %w", label, err)
		}
		return path, nil
	}
	relative, err := relativeToRoot(root, path)
	if err != nil {
		return "", fmt.Errorf("%s path must be inside repository: %w", label, err)
	}
	return relative, nil
}

func validateRecordHeader(record Record) error {
	if record.Status != "passed" {
		return fmt.Errorf("clean-tree record status is %q, want passed", record.Status)
	}
	started, err := time.Parse(time.RFC3339Nano, record.StartedAt)
	if err != nil {
		return fmt.Errorf("invalid clean-tree start time: %w", err)
	}
	finished, err := time.Parse(time.RFC3339Nano, record.FinishedAt)
	if err != nil {
		return fmt.Errorf("invalid clean-tree finish time: %w", err)
	}
	if finished.Before(started) {
		return errors.New("clean-tree finish time precedes start time")
	}
	preservation := record.Preservation
	digests := []struct {
		name  string
		value string
	}{
		{name: "index_sha256_before", value: preservation.IndexSHA256Before},
		{name: "index_sha256_after", value: preservation.IndexSHA256After},
		{name: "worktree_sha256_before", value: preservation.WorktreeSHA256Before},
		{name: "worktree_sha256_after", value: preservation.WorktreeSHA256After},
	}
	for _, digest := range digests {
		if err := soundnessevidence.ValidateDigest("clean-tree preservation "+digest.name, digest.value); err != nil {
			return fmt.Errorf("validate clean-tree preservation digest %q: %w", digest.name, err)
		}
	}
	if preservation.IndexSHA256Before != preservation.IndexSHA256After || preservation.WorktreeSHA256Before != preservation.WorktreeSHA256After {
		return errors.New("clean-tree recorder mutated the caller index or worktree")
	}
	return nil
}

func validateProofInputsSelected(
	root string,
	selected []string,
	planPath string,
	pathsPath string,
	plan Plan,
) error {
	required := []string{planPath, pathsPath, plan.Counterexamples.Path, plan.OwnershipManifestPath}
	for _, input := range plan.Inputs {
		required = append(required, input.Path)
	}
	for _, ledger := range plan.TaskLedgers {
		required = append(required, ledger.Path)
	}
	required = append(required, plan.AggregateReport.ManifestPath, plan.AggregateReport.RegistryPath)
	for _, path := range required {
		if !pathCoveredBySelection(root, selected, path) {
			return fmt.Errorf("proof input %q is not covered by the reviewed path selection", path)
		}
	}
	return nil
}

func pathCoveredBySelection(root string, selected []string, path string) bool {
	for _, selection := range selected {
		if path == selection {
			return true
		}
		info, err := os.Stat(resolveFromRoot(root, selection))
		if err == nil && info.IsDir() && strings.HasPrefix(path, selection+"/") {
			return true
		}
	}
	return false
}

func verifyCommands(plan Plan, outcomes []CommandOutcome) error {
	if len(outcomes) != len(plan.Commands) {
		return fmt.Errorf("record has %d command outcomes, want %d", len(outcomes), len(plan.Commands))
	}
	seen := make(map[string]bool, len(outcomes))
	for index, planned := range plan.Commands {
		outcome := outcomes[index]
		if seen[outcome.Name] {
			return fmt.Errorf("duplicate command outcome %q", outcome.Name)
		}
		seen[outcome.Name] = true
		if outcome.Name != planned.Name || outcome.Directory != planned.Directory || !slices.Equal(outcome.Args, planned.Args) {
			return fmt.Errorf("command outcome %d does not match planned vector %q", index, planned.Name)
		}
		wantVector := commandVectorDigest(planned.Directory, planned.Args)
		if outcome.VectorSHA256 != wantVector {
			return fmt.Errorf("command %q vector digest is stale", outcome.Name)
		}
		if outcome.LogSHA256 != digestBytes([]byte(outcome.Log)) {
			return fmt.Errorf("command %q log digest is stale", outcome.Name)
		}
		if !outcome.Passed || outcome.ExitCode != 0 || outcome.DurationMS < 0 {
			return fmt.Errorf("command %q did not retain a successful execution", outcome.Name)
		}
	}
	return nil
}

func verifyMutationProofs(plan Plan, record Record) error {
	if len(record.MutationProofs) != len(plan.MutationProofs) {
		return fmt.Errorf("record has %d mutation proofs, want %d", len(record.MutationProofs), len(plan.MutationProofs))
	}
	for index, planned := range plan.MutationProofs {
		proof := record.MutationProofs[index]
		if proof.Name != planned.Name || proof.Observation != planned.Observation {
			return fmt.Errorf("mutation proof %d does not match plan", index)
		}
		if !proof.CleanControlPassed || !proof.MutantSelected || !proof.IntendedMismatchSeen || !proof.Restored || !proof.PostControlPassed {
			return fmt.Errorf("mutation proof %q is not causal and restored", proof.Name)
		}
	}
	return nil
}
