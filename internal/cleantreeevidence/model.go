// SPDX-License-Identifier: MPL-2.0

// Package cleantreeevidence records and verifies soundness evidence against an
// exact synthetic Git tree assembled from HEAD and an explicit path selection.
package cleantreeevidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

const FormatVersion = 3

var requiredTaskLedgers = []TaskLedgerPlan{
	{
		Name:            "complete-goplint-soundness-hardening",
		Path:            "openspec/changes/complete-goplint-soundness-hardening/tasks.md",
		ExpectedPending: []string{"12.10"},
	},
	{
		Name:            "close-goplint-soundness-review-gaps",
		Path:            "openspec/changes/close-goplint-soundness-review-gaps/tasks.md",
		ExpectedPending: []string{"10.8"},
	},
	{
		Name:            "close-residual-goplint-soundness-gaps",
		Path:            "openspec/changes/close-residual-goplint-soundness-gaps/tasks.md",
		ExpectedPending: []string{},
	},
}

// Plan declares every input and executed command required by a retained proof.
type Plan struct {
	FormatVersion   int                 `json:"format_version"`
	Inputs          []InputPlan         `json:"inputs"`
	Toolchain       []ToolPlan          `json:"toolchain"`
	TaskLedgers     []TaskLedgerPlan    `json:"task_ledgers"`
	DiffReview      DiffReviewPlan      `json:"diff_review"`
	Counterexamples CounterexamplePlan  `json:"counterexamples"`
	Commands        []CommandPlan       `json:"commands"`
	AggregateReport AggregateReportPlan `json:"aggregate_report"`
	MutationProofs  []MutationProofPlan `json:"mutation_proofs"`
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

// Record is the retained format-v3 proof record.
type Record struct {
	FormatVersion   int                     `json:"format_version"`
	Status          string                  `json:"status"`
	StartedAt       string                  `json:"started_at"`
	FinishedAt      string                  `json:"finished_at"`
	Repository      RepositoryIdentity      `json:"repository"`
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
type RepositoryIdentity struct {
	BaseCommit          string   `json:"base_commit"`
	SyntheticTree       string   `json:"synthetic_tree"`
	SyntheticCommit     string   `json:"synthetic_commit"`
	DiffSHA256          string   `json:"diff_sha256"`
	PathSelectionSHA256 string   `json:"path_selection_sha256"`
	PathSelection       []string `json:"path_selection"`
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

// LoadPlan decodes and validates one format-v3 plan.
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

// LoadRecord decodes one format-v3 retained proof record.
func LoadRecord(path string) (Record, error) {
	var record Record
	if err := decodeStrictJSONFile(path, &record); err != nil {
		return Record{}, fmt.Errorf("decode clean-tree record: %w", err)
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
	if !reflectTaskLedgerPlansEqual(p.TaskLedgers, requiredTaskLedgers) {
		return fmt.Errorf("task ledgers must bind the three active changes in dependency order: got %+v, want %+v", p.TaskLedgers, requiredTaskLedgers)
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
	}
	if !isCanonicalIdentifier(p.AggregateReport.CommandName) || p.AggregateReport.OutputFile == "" ||
		p.AggregateReport.ManifestPath == "" || p.AggregateReport.RegistryPath == "" {
		return errors.New("aggregate report requires command, output, manifest, and registry")
	}
	if p.AggregateReport.Profile != soundnessgate.ProfileCore {
		return fmt.Errorf(
			"clean-tree aggregate report profile = %q, want %q",
			p.AggregateReport.Profile,
			soundnessgate.ProfileCore,
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

func reflectTaskLedgerPlansEqual(left, right []TaskLedgerPlan) bool {
	return slices.EqualFunc(left, right, func(leftPlan, rightPlan TaskLedgerPlan) bool {
		return leftPlan.Name == rightPlan.Name && leftPlan.Path == rightPlan.Path &&
			slices.Equal(leftPlan.ExpectedPending, rightPlan.ExpectedPending)
	})
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

func decodeStrictJSONFile(path string, target any) error {
	if err := requireRegularFile(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read JSON file %q: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON file %q: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}
