// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const (
	// PlanFormatVersion is the supported race/repeat execution-plan format.
	PlanFormatVersion = 1
	// ResultFormatVersion is the supported race/repeat work-result format.
	ResultFormatVersion = 1
)

type (
	// ArtifactBinding binds a reviewed or generated input by digest.
	ArtifactBinding struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
	}

	// BinaryBinding binds one build-once analyzer test binary.
	BinaryBinding struct {
		Mode     string `json:"mode"`
		FileName string `json:"file_name"`
		Digest   string `json:"digest"`
	}

	// WorkUnit assigns one exact test population to one mode and iteration.
	WorkUnit struct {
		ID             string   `json:"id"`
		Mode           string   `json:"mode"`
		Iteration      int      `json:"iteration"`
		MemberIDs      []string `json:"member_ids"`
		TotalWeight    int64    `json:"total_weight"`
		TimeoutSeconds int      `json:"timeout_seconds"`
		BinaryDigest   string   `json:"binary_digest"`
	}

	// Plan is the immutable analyzer race/repeat execution contract.
	Plan struct {
		FormatVersion   int             `json:"format_version"`
		PlanID          string          `json:"plan_id"`
		WorkspaceDigest string          `json:"workspace_digest"`
		Toolchain       string          `json:"toolchain"`
		Package         string          `json:"package"`
		Timing          ArtifactBinding `json:"timing"`
		CensusDigest    string          `json:"census_digest"`
		Census          []CensusEntry   `json:"census"`
		Environment     []string        `json:"environment"`
		Binaries        []BinaryBinding `json:"binaries"`
		RepeatCount     int             `json:"repeat_count"`
		ShardCount      int             `json:"shard_count"`
		WorkUnits       []WorkUnit      `json:"work_units"`
		DefaultedIDs    []string        `json:"defaulted_member_ids"`
	}
)

// NewPlan constructs a canonical race/repeat plan from exact reviewed inputs.
func NewPlan(
	workspaceDigest, packagePath string,
	timing ArtifactBinding,
	manifest TimingManifest,
	census []CensusEntry,
	binaries []BinaryBinding,
	shardCount, repeatCount int,
) (Plan, error) {
	if shardCount <= 0 || repeatCount <= 0 {
		return Plan{}, errors.New("race/repeat plan requires positive shard and repeat counts")
	}
	if manifest.Package != packagePath {
		return Plan{}, fmt.Errorf("race/repeat timing package = %q, want %q", manifest.Package, packagePath)
	}
	resolved, err := manifest.Resolve(census)
	if err != nil {
		return Plan{}, err
	}
	if err := ValidateNestedFamilies(manifest, resolved, shardCount); err != nil {
		return Plan{}, err
	}
	canonical, err := canonicalCensus(census)
	if err != nil {
		return Plan{}, err
	}
	binaryByMode := make(map[string]BinaryBinding, len(binaries))
	for _, binary := range binaries {
		binaryByMode[binary.Mode] = binary
	}
	plan := Plan{
		FormatVersion: PlanFormatVersion, WorkspaceDigest: workspaceDigest,
		Toolchain: manifest.Toolchain, Package: packagePath, Timing: timing,
		CensusDigest: resolved.CensusDigest, Census: canonical,
		Environment: slices.Clone(manifest.Environment),
		Binaries:    slices.Clone(binaries), RepeatCount: repeatCount, ShardCount: shardCount,
		WorkUnits: []WorkUnit{}, DefaultedIDs: slices.Clone(resolved.DefaultedMemberIDs),
	}
	for _, mode := range []string{"normal", "race"} {
		binary, exists := binaryByMode[mode]
		if !exists {
			return Plan{}, fmt.Errorf("race/repeat plan has no %s binary", mode)
		}
		shards, err := AllocateLPT(resolved.Entries, shardCount, mode)
		if err != nil {
			return Plan{}, err
		}
		iterations := repeatCount
		if mode == "race" {
			iterations = 1
		}
		for iteration := 1; iteration <= iterations; iteration++ {
			for shardIndex, shard := range shards {
				plan.WorkUnits = append(plan.WorkUnits, WorkUnit{
					ID:   fmt.Sprintf("%s-%02d-%02d", mode, iteration, shardIndex+1),
					Mode: mode, Iteration: iteration, MemberIDs: slices.Clone(shard.MemberIDs),
					TotalWeight: shard.TotalWeight, TimeoutSeconds: shard.TimeoutSeconds,
					BinaryDigest: binary.Digest,
				})
			}
		}
	}
	return normalizePlan(plan)
}

// Validate verifies plan identity, bindings, and exact census coverage for
// every mode and iteration.
func (plan Plan) Validate() error {
	if plan.FormatVersion != PlanFormatVersion || plan.PlanID == "" {
		return errors.New("race/repeat plan has an invalid version or empty identity")
	}
	if err := soundnessevidence.ValidateDigest("race/repeat plan id", plan.PlanID); err != nil {
		return fmt.Errorf("validate race/repeat plan id: %w", err)
	}
	if err := soundnessevidence.ValidateDigest("race/repeat workspace", plan.WorkspaceDigest); err != nil {
		return fmt.Errorf("validate race/repeat workspace: %w", err)
	}
	if plan.Toolchain == "" || plan.Package == "" || plan.RepeatCount <= 0 || plan.ShardCount <= 0 {
		return errors.New("race/repeat plan has incomplete toolchain, package, or execution bounds")
	}
	if !slices.Equal(plan.Environment, []string{ScheduledOracleEnvironment}) {
		return errors.New("race/repeat plan has an invalid complete-census environment")
	}
	if plan.Timing.Name == "" {
		return errors.New("race/repeat plan timing name is empty")
	}
	if err := soundnessevidence.ValidateDigest("race/repeat timing", plan.Timing.Digest); err != nil {
		return fmt.Errorf("validate race/repeat timing digest: %w", err)
	}
	if !slices.IsSorted(plan.DefaultedIDs) {
		return errors.New("race/repeat defaulted member identities are non-canonical")
	}
	for index, id := range plan.DefaultedIDs {
		if index > 0 && plan.DefaultedIDs[index-1] == id {
			return fmt.Errorf("race/repeat defaulted member identity %q is duplicated", id)
		}
		if !slices.ContainsFunc(plan.Census, func(entry CensusEntry) bool { return entry.ID == id }) {
			return fmt.Errorf("race/repeat defaulted member identity %q is outside the census", id)
		}
	}
	canonical, err := canonicalCensus(plan.Census)
	if err != nil || !slices.Equal(canonical, plan.Census) {
		return errors.New("race/repeat plan census is invalid or non-canonical")
	}
	censusDigest, err := digestCensus(canonical)
	if err != nil || censusDigest != plan.CensusDigest {
		return errors.New("race/repeat plan census digest does not match its members")
	}
	binaryByMode, err := validateBinaries(plan.Binaries)
	if err != nil {
		return err
	}
	if err := validateWorkUnits(plan, binaryByMode); err != nil {
		return err
	}
	computed, err := plan.calculateID()
	if err != nil {
		return err
	}
	if computed != plan.PlanID {
		return errors.New("race/repeat plan id does not match its canonical content")
	}
	return nil
}

func validateBinaries(binaries []BinaryBinding) (map[string]BinaryBinding, error) {
	if len(binaries) != 2 || binaries[0].Mode != "normal" || binaries[1].Mode != "race" {
		return nil, errors.New("race/repeat plan requires canonical normal and race binaries")
	}
	result := make(map[string]BinaryBinding, len(binaries))
	for _, binary := range binaries {
		if binary.FileName == "" || strings.ContainsAny(binary.FileName, "/\\\x00\r\n") {
			return nil, fmt.Errorf("race/repeat %s binary file name is unsafe", binary.Mode)
		}
		if err := soundnessevidence.ValidateDigest("race/repeat "+binary.Mode+" binary", binary.Digest); err != nil {
			return nil, fmt.Errorf("validate race/repeat %s binary digest: %w", binary.Mode, err)
		}
		result[binary.Mode] = binary
	}
	return result, nil
}

func validateWorkUnits(plan Plan, binaries map[string]BinaryBinding) error {
	expectedIterations := map[string]int{"normal": plan.RepeatCount, "race": 1}
	seenIDs := make(map[string]bool, len(plan.WorkUnits))
	coverage := make(map[string]map[string]bool)
	shardCounts := make(map[string]int)
	previousID := ""
	for _, unit := range plan.WorkUnits {
		if unit.ID == "" || unit.ID < previousID || seenIDs[unit.ID] {
			return errors.New("race/repeat work-unit identities are empty, duplicate, or non-canonical")
		}
		previousID = unit.ID
		seenIDs[unit.ID] = true
		binary, exists := binaries[unit.Mode]
		if !exists || unit.BinaryDigest != binary.Digest {
			return fmt.Errorf("race/repeat work unit %q has an invalid binary binding", unit.ID)
		}
		if unit.Iteration <= 0 || unit.Iteration > expectedIterations[unit.Mode] ||
			unit.TotalWeight <= 0 || unit.TimeoutSeconds <= 0 || len(unit.MemberIDs) == 0 ||
			!slices.IsSorted(unit.MemberIDs) {
			return fmt.Errorf("race/repeat work unit %q has invalid bounds or members", unit.ID)
		}
		key := fmt.Sprintf("%s:%d", unit.Mode, unit.Iteration)
		shardCounts[key]++
		if coverage[key] == nil {
			coverage[key] = make(map[string]bool, len(plan.Census))
		}
		for _, memberID := range unit.MemberIDs {
			if !slices.ContainsFunc(plan.Census, func(entry CensusEntry) bool { return entry.ID == memberID }) || coverage[key][memberID] {
				return fmt.Errorf("race/repeat work unit %q has an unknown or overlapping member %q", unit.ID, memberID)
			}
			coverage[key][memberID] = true
		}
	}
	for mode, iterations := range expectedIterations {
		for iteration := 1; iteration <= iterations; iteration++ {
			key := fmt.Sprintf("%s:%d", mode, iteration)
			if len(coverage[key]) != len(plan.Census) {
				return fmt.Errorf("race/repeat plan incompletely covers %s iteration %d", mode, iteration)
			}
			if shardCounts[key] != plan.ShardCount {
				return fmt.Errorf("race/repeat plan has %d %s iteration %d shards, want %d", shardCounts[key], mode, iteration, plan.ShardCount)
			}
		}
	}
	return nil
}

func normalizePlan(plan Plan) (Plan, error) {
	plan.Census = slices.Clone(plan.Census)
	slices.SortFunc(plan.Census, func(left, right CensusEntry) int { return strings.Compare(left.ID, right.ID) })
	plan.Binaries = slices.Clone(plan.Binaries)
	plan.Environment = slices.Clone(plan.Environment)
	slices.SortFunc(plan.Binaries, func(left, right BinaryBinding) int { return strings.Compare(left.Mode, right.Mode) })
	plan.WorkUnits = slices.Clone(plan.WorkUnits)
	for index := range plan.WorkUnits {
		plan.WorkUnits[index].MemberIDs = slices.Clone(plan.WorkUnits[index].MemberIDs)
		slices.Sort(plan.WorkUnits[index].MemberIDs)
	}
	slices.SortFunc(plan.WorkUnits, func(left, right WorkUnit) int { return strings.Compare(left.ID, right.ID) })
	plan.DefaultedIDs = slices.Clone(plan.DefaultedIDs)
	slices.Sort(plan.DefaultedIDs)
	plan.PlanID = ""
	planID, err := plan.calculateID()
	if err != nil {
		return Plan{}, err
	}
	plan.PlanID = planID
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (plan Plan) calculateID() (string, error) {
	plan.PlanID = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode race/repeat plan identity: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}

// CanonicalPlanJSON returns the canonical retained race/repeat plan bytes.
func CanonicalPlanJSON(plan Plan) ([]byte, error) {
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("encode canonical race/repeat plan: %w", err)
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode canonical race/repeat plan: %w", err)
	}
	return append(data, '\n'), nil
}
