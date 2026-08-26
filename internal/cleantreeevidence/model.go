// SPDX-License-Identifier: MPL-2.0

// Package cleantreeevidence records and verifies soundness evidence against an
// exact synthetic Git tree assembled from HEAD and an explicit path selection.
package cleantreeevidence

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/invowk/goplint/internal/soundnessgate"
)

const FormatVersion = 4

// legacyFormatVersion is the retired single-digest record format. It is
// rejected with an explicit migration notice rather than silently accepted or
// reinterpreted.
const legacyFormatVersion = 3

const (
	// ProvenanceGenerated marks a record produced by fresh gate execution.
	ProvenanceGenerated = "generated"
	// ProvenanceRebound marks a record whose prose identity was re-bound while
	// the retained aggregate report was carried forward untouched.
	ProvenanceRebound = "re-bound"
)

var (
	taskLedgerArchivedPattern = regexp.MustCompile(`^(?:openspec/changes/archive|docs/ledgers/archive)/([0-9]{4}-[0-9]{2}-[0-9]{2})-([a-z0-9][a-z0-9-]*)/tasks\.md$`)
	taskLedgerActivePattern   = regexp.MustCompile(`^(?:openspec/changes|docs/ledgers)/([a-z0-9][a-z0-9-]*)/tasks\.md$`)
	pendingTaskIDPattern      = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

// Plan declares every input and executed command required by a retained proof.
// The reviewed plan is the single source of truth for expected task-ledger
// names, paths, order, and permitted pending identifiers; code and schema
// validate structure only.
type Plan struct {
	FormatVersion         int                 `json:"format_version"`
	OwnershipManifestPath string              `json:"ownership_manifest_path"`
	Inputs                []InputPlan         `json:"inputs"`
	Toolchain             []ToolPlan          `json:"toolchain"`
	TaskLedgers           []TaskLedgerPlan    `json:"task_ledgers"`
	DiffReview            DiffReviewPlan      `json:"diff_review"`
	Counterexamples       CounterexamplePlan  `json:"counterexamples"`
	Commands              []CommandPlan       `json:"commands"`
	AggregateReport       AggregateReportPlan `json:"aggregate_report"`
	MutationProofs        []MutationProofPlan `json:"mutation_proofs"`
}

// InputPlan identifies a proof input whose byte digest is retained.
type InputPlan struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ToolPlan identifies a version command and the required version expression.
type ToolPlan struct {
	Name              string   `json:"name"`
	Command           []string `json:"command"`
	RequiredVersionRE string   `json:"required_version_re"`
}

// TaskLedgerPlan identifies a task ledger and the only task IDs that may still
// be pending when the retained proof is accepted.
type TaskLedgerPlan struct {
	Name            string   `json:"name"`
	Path            string   `json:"path"`
	ExpectedPending []string `json:"expected_pending"`
}

// DiffReviewPlan declares every changed path intentionally excluded from the
// combined proof tree. Paths selected for the proof may not also be excluded.
type DiffReviewPlan struct {
	ReviewedExclusions []ReviewedExclusion `json:"reviewed_exclusions"`
}

// ReviewedExclusion records the exact path and human review rationale for one
// unrelated repository change.
type ReviewedExclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// CounterexamplePlan identifies the reviewed counterexample inventory.
type CounterexamplePlan struct {
	Path     string                          `json:"path"`
	Required []CounterexampleObservationPlan `json:"required"`
}

// CounterexampleObservationPlan binds one reviewed counterexample to its exact
// required production observation.
type CounterexampleObservationPlan struct {
	ID          string `json:"id"`
	Observation string `json:"observation"`
}

// CommandPlan declares one exact command vector.
type CommandPlan struct {
	Name           string   `json:"name"`
	Directory      string   `json:"directory,omitempty"`
	Args           []string `json:"args"`
	TimeoutMinutes int      `json:"timeout_minutes"`
}

// AggregateReportPlan binds the retained aggregate report to its producing
// command and reviewed manifest/registry.
type AggregateReportPlan struct {
	CommandName  string                  `json:"command_name"`
	OutputFile   string                  `json:"output_file"`
	ManifestPath string                  `json:"manifest_path"`
	RegistryPath string                  `json:"registry_path"`
	Profile      soundnessgate.ProfileID `json:"profile"`
}

// MutationProofPlan names an observation that must contain a causal mutation
// sequence rather than a generic failing command.
type MutationProofPlan struct {
	Name        string `json:"name"`
	Observation string `json:"observation"`
}

// Record is the retained format-v4 proof record.
type Record struct {
	FormatVersion   int                     `json:"format_version"`
	Status          string                  `json:"status"`
	StartedAt       string                  `json:"started_at"`
	FinishedAt      string                  `json:"finished_at"`
	Repository      RepositoryIdentity      `json:"repository"`
	Provenance      ProvenanceIdentity      `json:"provenance"`
	DiffCensus      DiffCensusIdentity      `json:"diff_census"`
	Inputs          []InputIdentity         `json:"inputs"`
	Toolchain       []ToolIdentity          `json:"toolchain"`
	TaskLedgers     []TaskLedgerIdentity    `json:"task_ledgers"`
	Counterexamples CounterexampleIdentity  `json:"counterexamples"`
	Commands        []CommandOutcome        `json:"commands"`
	AggregateReport AggregateReportIdentity `json:"aggregate_report"`
	MutationProofs  []MutationProof         `json:"mutation_proofs"`
	Preservation    PreservationIdentity    `json:"preservation"`
}

// RepositoryIdentity binds a proof to the exact selected repository content.
// The tree binding is split by reviewed ownership class: the semantic-content
// digest covers every path class that any gate executes or reads, and the
// prose digest covers documentation-class paths only.
type RepositoryIdentity struct {
	BaseCommit          string   `json:"base_commit"`
	SyntheticTree       string   `json:"synthetic_tree"`
	SyntheticCommit     string   `json:"synthetic_commit"`
	DiffSHA256          string   `json:"diff_sha256"`
	SemanticTreeDigest  string   `json:"semantic_tree_digest"`
	ProseTreeDigest     string   `json:"prose_tree_digest"`
	PathSelectionSHA256 string   `json:"path_selection_sha256"`
	PathSelection       []string `json:"path_selection"`
}

// ProvenanceIdentity attributes the retained aggregate report to the exact
// semantic content that produced it. Re-binding rewrites the prose identity
// but never this attribution: the aggregate semantic digest and workspace
// digest stay fixed across every re-bind of the same evidence.
type ProvenanceIdentity struct {
	Kind                        string `json:"kind"`
	AggregateSemanticTreeDigest string `json:"aggregate_semantic_tree_digest"`
	AggregateWorkspaceDigest    string `json:"aggregate_workspace_digest"`
	CarriedReportSHA256         string `json:"carried_report_sha256"`
	PreviousProseDigest         string `json:"previous_prose_digest,omitempty"`
}

// DiffCensusIdentity binds every selected or explicitly excluded changed path
// relative to the retained base. Recorder outputs are listed separately because
// publishing the record necessarily changes those paths after census capture.
type DiffCensusIdentity struct {
	BaseCommit         string                `json:"base_commit"`
	Changes            []ChangedPathIdentity `json:"changes"`
	ReviewedExclusions []ReviewedExclusion   `json:"reviewed_exclusions"`
	AuthorizedOutputs  []string              `json:"authorized_outputs"`
	CanonicalSHA256    string                `json:"canonical_sha256"`
}

// ChangedPathIdentity records one complete-diff member and how review disposed
// of it. ContentSHA256 is empty only for a deletion.
type ChangedPathIdentity struct {
	Path          string `json:"path"`
	GitStatus     string `json:"git_status"`
	Kind          string `json:"kind"`
	ContentSHA256 string `json:"content_sha256,omitempty"`
	Disposition   string `json:"disposition"`
}

// InputIdentity records the digest of one declared proof input.
type InputIdentity struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ToolIdentity records the executed version command and its exact result.
type ToolIdentity struct {
	Name              string   `json:"name"`
	Command           []string `json:"command"`
	RequiredVersionRE string   `json:"required_version_re"`
	Version           string   `json:"version"`
}

// TaskLedgerIdentity records exact checkbox state from one OpenSpec task file.
type TaskLedgerIdentity struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	SHA256     string   `json:"sha256"`
	Total      int      `json:"total"`
	Completed  int      `json:"completed"`
	PendingIDs []string `json:"pending_ids"`
}

// CounterexampleIdentity records the exact inventory and observed IDs.
type CounterexampleIdentity struct {
	Path         string                          `json:"path"`
	SHA256       string                          `json:"sha256"`
	Observations []CounterexampleObservationPlan `json:"observations"`
}

// CommandOutcome records one command and its retained log identity.
type CommandOutcome struct {
	Name         string   `json:"name"`
	Directory    string   `json:"directory"`
	Args         []string `json:"args"`
	VectorSHA256 string   `json:"vector_sha256"`
	ExitCode     int      `json:"exit_code"`
	DurationMS   int64    `json:"duration_ms"`
	Log          string   `json:"log"`
	LogSHA256    string   `json:"log_sha256"`
	Passed       bool     `json:"passed"`
}

// AggregateReportIdentity embeds the fully validated report and the exact
// manifest and registry bytes that define its meaning.
type AggregateReportIdentity struct {
	OutputFile     string                  `json:"output_file"`
	SHA256         string                  `json:"sha256"`
	ManifestPath   string                  `json:"manifest_path"`
	ManifestSHA256 string                  `json:"manifest_sha256"`
	RegistryPath   string                  `json:"registry_path"`
	RegistrySHA256 string                  `json:"registry_sha256"`
	Report         soundnessgate.RunReport `json:"report"`
}

// MutationProof records the required causal control/mutation/restoration chain.
type MutationProof struct {
	Name                 string `json:"name"`
	Observation          string `json:"observation"`
	CleanControlPassed   bool   `json:"clean_control_passed"`
	MutantSelected       bool   `json:"mutant_selected"`
	IntendedMismatchSeen bool   `json:"intended_mismatch_seen"`
	Restored             bool   `json:"restored"`
	PostControlPassed    bool   `json:"post_control_passed"`
}

// PreservationIdentity proves the recorder left the caller state unchanged.
type PreservationIdentity struct {
	IndexSHA256Before    string `json:"index_sha256_before"`
	IndexSHA256After     string `json:"index_sha256_after"`
	WorktreeSHA256Before string `json:"worktree_sha256_before"`
	WorktreeSHA256After  string `json:"worktree_sha256_after"`
}

// LoadPlan decodes and validates one format-v4 plan.
func LoadPlan(path string) (Plan, error) {
	var plan Plan
	if err := decodeStrictJSONFile(path, &plan); err != nil {
		return Plan{}, fmt.Errorf("decode clean-tree plan: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// LoadRecord decodes one format-v4 retained proof record. The retired v3
// single-digest format is rejected with an explicit migration notice.
func LoadRecord(path string) (Record, error) {
	var record Record
	if err := decodeStrictJSONFile(path, &record); err != nil {
		return Record{}, fmt.Errorf("decode clean-tree record: %w", err)
	}
	if record.FormatVersion == legacyFormatVersion {
		return Record{}, fmt.Errorf(
			"clean-tree record format %d is the retired single-digest format: regenerate the retained record with "+
				"'make generate-goplint-clean-tree-evidence' to migrate to format %d with semantic and prose tree digests",
			legacyFormatVersion,
			FormatVersion,
		)
	}
	if record.FormatVersion != FormatVersion {
		return Record{}, fmt.Errorf("unsupported clean-tree record format %d", record.FormatVersion)
	}
	return record, nil
}

// Validate rejects incomplete, ambiguous, or internally disconnected plans.
func (p Plan) Validate() error {
	if p.FormatVersion != FormatVersion {
		return fmt.Errorf("unsupported clean-tree plan format %d", p.FormatVersion)
	}
	if len(p.Inputs) == 0 || len(p.Toolchain) == 0 || len(p.TaskLedgers) == 0 || len(p.Commands) == 0 {
		return errors.New("clean-tree plan requires inputs, toolchain, task ledgers, and commands")
	}
	if err := validateRepoPath(p.OwnershipManifestPath); err != nil {
		return fmt.Errorf("ownership manifest: %w", err)
	}
	if err := validateNamedPaths("input", p.Inputs, func(input InputPlan) (string, string) {
		return input.Name, input.Path
	}); err != nil {
		return err
	}
	if err := validateNamedPaths("task ledger", p.TaskLedgers, func(ledger TaskLedgerPlan) (string, string) {
		return ledger.Name, ledger.Path
	}); err != nil {
		return err
	}
	if err := validateTaskLedgerStructure(p.TaskLedgers); err != nil {
		return err
	}
	if err := validateReviewedExclusions(p.DiffReview.ReviewedExclusions); err != nil {
		return err
	}
	if err := validateRepoPath(p.Counterexamples.Path); err != nil {
		return fmt.Errorf("counterexample inventory: %w", err)
	}
	if len(p.Counterexamples.Required) == 0 {
		return errors.New("counterexample inventory requires at least one ID")
	}
	previousCounterexample := ""
	for _, counterexample := range p.Counterexamples.Required {
		if !isCanonicalIdentifier(counterexample.ID) || strings.TrimSpace(counterexample.Observation) == "" ||
			counterexample.Observation != strings.TrimSpace(counterexample.Observation) {
			return fmt.Errorf("incomplete counterexample expectation %q", counterexample.ID)
		}
		if previousCounterexample != "" && counterexample.ID <= previousCounterexample {
			return errors.New("counterexample expectations must have unique IDs in lexical order")
		}
		previousCounterexample = counterexample.ID
	}
	toolNames := make(map[string]bool, len(p.Toolchain))
	for _, tool := range p.Toolchain {
		if !isCanonicalIdentifier(tool.Name) || toolNames[tool.Name] || len(tool.Command) == 0 || tool.RequiredVersionRE == "" {
			return fmt.Errorf("incomplete or duplicate toolchain entry %q", tool.Name)
		}
		for index, argument := range tool.Command {
			if strings.TrimSpace(argument) == "" {
				return fmt.Errorf("toolchain entry %q command[%d] is empty", tool.Name, index)
			}
		}
		if _, err := regexp.Compile(tool.RequiredVersionRE); err != nil {
			return fmt.Errorf("toolchain entry %q has invalid required_version_re: %w", tool.Name, err)
		}
		toolNames[tool.Name] = true
	}
	for _, ledger := range p.TaskLedgers {
		if err := validateUniqueNonempty("expected pending task", ledger.ExpectedPending); err != nil {
			return fmt.Errorf("task ledger %q: %w", ledger.Name, err)
		}
		for _, pendingID := range ledger.ExpectedPending {
			if !pendingTaskIDPattern.MatchString(pendingID) {
				return fmt.Errorf("task ledger %q expected pending id %q is not a task number", ledger.Name, pendingID)
			}
		}
	}
	if !isCanonicalIdentifier(p.AggregateReport.CommandName) || p.AggregateReport.OutputFile == "" ||
		p.AggregateReport.ManifestPath == "" || p.AggregateReport.RegistryPath == "" {
		return errors.New("aggregate report requires command, output, manifest, and registry")
	}
	if p.AggregateReport.Profile != soundnessgate.ProfileSemantic {
		return fmt.Errorf(
			"clean-tree aggregate report profile = %q, want %q",
			p.AggregateReport.Profile,
			soundnessgate.ProfileSemantic,
		)
	}
	outputFile := filepath.FromSlash(p.AggregateReport.OutputFile)
	if filepath.Base(outputFile) != outputFile || filepath.Ext(outputFile) != ".json" {
		return fmt.Errorf("aggregate report output_file %q must be one JSON file name", p.AggregateReport.OutputFile)
	}
	paths := []struct {
		name string
		path string
	}{
		{name: "manifest", path: p.AggregateReport.ManifestPath},
		{name: "registry", path: p.AggregateReport.RegistryPath},
	}
	for _, path := range paths {
		if err := validateRepoPath(path.path); err != nil {
			return fmt.Errorf("aggregate report %s: %w", path.name, err)
		}
	}
	commandNames := make(map[string]bool, len(p.Commands))
	for _, command := range p.Commands {
		if !isCanonicalIdentifier(command.Name) || commandNames[command.Name] || len(command.Args) == 0 || command.TimeoutMinutes <= 0 {
			return fmt.Errorf("incomplete or duplicate command %q", command.Name)
		}
		for index, argument := range command.Args {
			if strings.TrimSpace(argument) == "" {
				return fmt.Errorf("command %q args[%d] is empty", command.Name, index)
			}
		}
		if command.Directory != "" {
			if err := validateRepoPath(command.Directory); err != nil {
				return fmt.Errorf("command %q directory: %w", command.Name, err)
			}
		}
		commandNames[command.Name] = true
	}
	if !commandNames[p.AggregateReport.CommandName] {
		return fmt.Errorf("aggregate report references unknown command %q", p.AggregateReport.CommandName)
	}
	mutationNames := make(map[string]bool, len(p.MutationProofs))
	for _, mutation := range p.MutationProofs {
		if !isCanonicalIdentifier(mutation.Name) || !isCanonicalIdentifier(mutation.Observation) || mutationNames[mutation.Name] {
			return fmt.Errorf("incomplete or duplicate mutation proof %q", mutation.Name)
		}
		mutationNames[mutation.Name] = true
	}
	if len(p.MutationProofs) == 0 {
		return errors.New("core clean-tree plan requires at least one causal mutation proof")
	}
	return nil
}

// validateTaskLedgerStructure enforces structural ledger policy without
// embedding expected values: every ledger path must be an OpenSpec change or
// archived change tasks file whose change name matches the ledger name,
// archived predecessors must precede every active change, and archive dates
// must not regress. The reviewed plan alone owns the expected values.
func validateTaskLedgerStructure(ledgers []TaskLedgerPlan) error {
	seenActive := false
	previousArchiveDate := ""
	for _, ledger := range ledgers {
		if match := taskLedgerArchivedPattern.FindStringSubmatch(ledger.Path); match != nil {
			if ledger.Name != match[2] {
				return fmt.Errorf("task ledger %q does not match its archived change name %q", ledger.Name, match[2])
			}
			if seenActive {
				return fmt.Errorf("archived task ledger %q follows an active change ledger", ledger.Name)
			}
			if match[1] < previousArchiveDate {
				return fmt.Errorf("archived task ledger %q regresses the archive date order", ledger.Name)
			}
			previousArchiveDate = match[1]
			continue
		}
		if match := taskLedgerActivePattern.FindStringSubmatch(ledger.Path); match != nil {
			if ledger.Name != match[1] {
				return fmt.Errorf("task ledger %q does not match its change name %q", ledger.Name, match[1])
			}
			seenActive = true
			continue
		}
		return fmt.Errorf("task ledger %q path %q is not an OpenSpec change tasks ledger", ledger.Name, ledger.Path)
	}
	return nil
}

func validateReviewedExclusions(exclusions []ReviewedExclusion) error {
	previousPath := ""
	for _, exclusion := range exclusions {
		if err := validateRepoPath(exclusion.Path); err != nil {
			return fmt.Errorf("reviewed exclusion: %w", err)
		}
		if exclusion.Reason == "" || exclusion.Reason != strings.TrimSpace(exclusion.Reason) {
			return fmt.Errorf("reviewed exclusion %q requires a trimmed nonempty reason", exclusion.Path)
		}
		if previousPath != "" && exclusion.Path <= previousPath {
			return errors.New("reviewed exclusions must have unique paths in lexical order")
		}
		previousPath = exclusion.Path
	}
	return nil
}

func validateNamedPaths[T any](kind string, values []T, fields func(T) (string, string)) error {
	seenNames := make(map[string]bool, len(values))
	seenPaths := make(map[string]bool, len(values))
	for _, value := range values {
		name, path := fields(value)
		if !isCanonicalIdentifier(name) || seenNames[name] {
			return fmt.Errorf("empty or duplicate %s name %q", kind, name)
		}
		if seenPaths[path] {
			return fmt.Errorf("duplicate %s path %q", kind, path)
		}
		if err := validateRepoPath(path); err != nil {
			return fmt.Errorf("%s %q: %w", kind, name, err)
		}
		seenNames[name] = true
		seenPaths[path] = true
	}
	return nil
}

func validateRepoPath(path string) error {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") {
		return fmt.Errorf("path %q must be a nonempty repository-relative slash path", path)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean != path || path == "." || path == ".." || strings.HasPrefix(path, "../") || path == ".git" || strings.HasPrefix(path, ".git/") {
		return fmt.Errorf("path %q is not a clean repository path", path)
	}
	return nil
}

func validateUniqueNonempty(kind string, values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !isCanonicalIdentifier(value) || seen[value] {
			return fmt.Errorf("empty or duplicate %s %q", kind, value)
		}
		seen[value] = true
	}
	if !slices.IsSorted(values) {
		return fmt.Errorf("%s values must be sorted", kind)
	}
	return nil
}

func isCanonicalIdentifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value)
}

// decodeStrictJSONFile reads an integrity-sensitive record with json/v2
// semantics: duplicate object members and invalid UTF-8 are rejected by
// default, field-name matching is case-sensitive, unknown members are
// rejected, and trailing content after the top-level value is rejected. All
// records read here are produced by writers in this package (json.Marshal
// output is stable across v1/v2), so case-sensitive matching aligns with the
// emitted snake_case tags.
func decodeStrictJSONFile(path string, target any) error {
	if err := requireRegularFile(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read JSON file %q: %w", path, err)
	}
	if err := jsonv2.Unmarshal(data, target, jsonv2.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("decode JSON file %q: %w", path, err)
	}
	return nil
}
