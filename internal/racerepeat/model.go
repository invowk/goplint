// SPDX-License-Identifier: MPL-2.0

// Package racerepeat plans and validates exhaustive analyzer race/repeat work.
package racerepeat

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const (
	// TimingFormatVersion is the supported analyzer timing-manifest format.
	TimingFormatVersion = 1
	// DefaultShardCount is the reviewed analyzer shard population.
	DefaultShardCount = 16
	// ScheduledOracleEnvironment makes the complete listed analyzer census
	// runnable instead of silently skipping its profile-gated member.
	ScheduledOracleEnvironment = "GOPLINT_PROTOCOL_ORACLE_PROFILE=scheduled"
)

type (
	// TestKind classifies a top-level Go test-binary entry.
	TestKind string

	// TimingEntry records an upper-median top-level duration and nested-family
	// diagnostics from fresh go test -json samples.
	TimingEntry struct {
		ID                           string   `json:"id"`
		Kind                         TestKind `json:"kind"`
		DurationWeightNanoseconds    int64    `json:"duration_weight_nanoseconds"`
		SampleCount                  int      `json:"sample_count"`
		NestedCaseCount              int      `json:"nested_case_count"`
		MaximumNestedCaseNanoseconds int64    `json:"maximum_nested_case_nanoseconds"`
	}

	// TimingManifest is the reviewed deterministic weighting input.
	TimingManifest struct {
		FormatVersion            int           `json:"format_version"`
		Package                  string        `json:"package"`
		Toolchain                string        `json:"toolchain"`
		GeneratedAt              time.Time     `json:"generated_at"`
		DefaultWeightNanoseconds int64         `json:"default_weight_nanoseconds"`
		ReviewedInternalShardIDs []string      `json:"reviewed_internal_shard_ids"`
		Environment              []string      `json:"environment"`
		Entries                  []TimingEntry `json:"entries"`
	}

	// CensusEntry is one live top-level Test, Fuzz, or Example.
	CensusEntry struct {
		ID   string   `json:"id"`
		Kind TestKind `json:"kind"`
	}

	// ResolvedTiming binds every live census member to a visible weight.
	ResolvedTiming struct {
		Entries            []TimingEntry `json:"entries"`
		DefaultedMemberIDs []string      `json:"defaulted_member_ids"`
		CensusDigest       string        `json:"census_digest"`
	}

	// Shard is one deterministic LPT allocation.
	Shard struct {
		ID             string   `json:"id"`
		MemberIDs      []string `json:"member_ids"`
		TotalWeight    int64    `json:"total_weight"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
)

const (
	KindTest    TestKind = "test"
	KindFuzz    TestKind = "fuzz"
	KindExample TestKind = "example"
)

// Validate verifies canonical timing identity, weights, and nested metadata.
func (manifest TimingManifest) Validate() error {
	if manifest.FormatVersion != TimingFormatVersion {
		return fmt.Errorf("race/repeat timing format_version = %d, want %d", manifest.FormatVersion, TimingFormatVersion)
	}
	if strings.TrimSpace(manifest.Package) == "" || strings.TrimSpace(manifest.Toolchain) == "" || manifest.GeneratedAt.IsZero() {
		return errors.New("race/repeat timing manifest has incomplete package, toolchain, or generation metadata")
	}
	if manifest.DefaultWeightNanoseconds <= 0 || len(manifest.Entries) == 0 {
		return errors.New("race/repeat timing manifest has no positive default weight or entries")
	}
	if !slices.IsSorted(manifest.ReviewedInternalShardIDs) ||
		!slices.IsSorted(manifest.Environment) ||
		!slices.IsSortedFunc(manifest.Entries, func(left, right TimingEntry) int { return strings.Compare(left.ID, right.ID) }) {
		return errors.New("race/repeat timing manifest is not canonical")
	}
	if !slices.Equal(manifest.Environment, []string{ScheduledOracleEnvironment}) {
		return errors.New("race/repeat timing manifest does not bind the reviewed complete-census environment")
	}
	if len(manifest.ReviewedInternalShardIDs) != 0 {
		return errors.New("race/repeat internal case sharding is not implemented; nested families must be fully represented by their top-level weight")
	}
	seen := make(map[string]bool, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if err := validateCensusEntry(CensusEntry{ID: entry.ID, Kind: entry.Kind}); err != nil {
			return err
		}
		if seen[entry.ID] {
			return fmt.Errorf("race/repeat timing manifest contains duplicate member %q", entry.ID)
		}
		seen[entry.ID] = true
		if entry.DurationWeightNanoseconds <= 0 || entry.SampleCount <= 0 ||
			entry.NestedCaseCount < 0 || entry.MaximumNestedCaseNanoseconds < 0 {
			return fmt.Errorf("race/repeat timing entry %q has invalid measurements", entry.ID)
		}
		if entry.MaximumNestedCaseNanoseconds > entry.DurationWeightNanoseconds {
			return fmt.Errorf("race/repeat timing entry %q has a nested case weight %d above its top-level scheduling weight %d", entry.ID, entry.MaximumNestedCaseNanoseconds, entry.DurationWeightNanoseconds)
		}
	}
	return nil
}

// Resolve validates timing metadata against the exact live census, rejects
// unknown entries, and assigns conservative visible weights to new members.
func (manifest TimingManifest) Resolve(census []CensusEntry) (ResolvedTiming, error) {
	if err := manifest.Validate(); err != nil {
		return ResolvedTiming{}, err
	}
	canonicalCensus, err := canonicalCensus(census)
	if err != nil {
		return ResolvedTiming{}, err
	}
	live := make(map[string]CensusEntry, len(canonicalCensus))
	for _, entry := range canonicalCensus {
		live[entry.ID] = entry
	}
	byID := make(map[string]TimingEntry, len(manifest.Entries))
	maximumWeight := manifest.DefaultWeightNanoseconds
	for _, entry := range manifest.Entries {
		censusEntry, exists := live[entry.ID]
		if !exists {
			return ResolvedTiming{}, fmt.Errorf("race/repeat timing manifest contains unknown member %q", entry.ID)
		}
		if censusEntry.Kind != entry.Kind {
			return ResolvedTiming{}, fmt.Errorf("race/repeat timing member %q kind = %q, want %q", entry.ID, entry.Kind, censusEntry.Kind)
		}
		byID[entry.ID] = entry
		maximumWeight = max(maximumWeight, entry.DurationWeightNanoseconds)
	}
	resolved := ResolvedTiming{Entries: make([]TimingEntry, 0, len(canonicalCensus)), DefaultedMemberIDs: []string{}}
	for _, entry := range canonicalCensus {
		timing, exists := byID[entry.ID]
		if !exists {
			timing = TimingEntry{
				ID: entry.ID, Kind: entry.Kind, DurationWeightNanoseconds: maximumWeight, SampleCount: 1,
			}
			resolved.DefaultedMemberIDs = append(resolved.DefaultedMemberIDs, entry.ID)
		}
		resolved.Entries = append(resolved.Entries, timing)
	}
	censusDigest, err := digestCensus(canonicalCensus)
	if err != nil {
		return ResolvedTiming{}, err
	}
	resolved.CensusDigest = censusDigest
	return resolved, nil
}

func canonicalCensus(census []CensusEntry) ([]CensusEntry, error) {
	result := slices.Clone(census)
	slices.SortFunc(result, func(left, right CensusEntry) int { return strings.Compare(left.ID, right.ID) })
	for index, entry := range result {
		if err := validateCensusEntry(entry); err != nil {
			return nil, err
		}
		if index > 0 && result[index-1].ID == entry.ID {
			return nil, fmt.Errorf("race/repeat census contains duplicate member %q", entry.ID)
		}
	}
	return result, nil
}

func validateCensusEntry(entry CensusEntry) error {
	if entry.ID == "" || strings.ContainsAny(entry.ID, "\x00\r\n") {
		return errors.New("race/repeat census member has an unsafe empty or control-character identity")
	}
	wantKind, err := kindForName(entry.ID)
	if err != nil {
		return err
	}
	if entry.Kind != wantKind {
		return fmt.Errorf("race/repeat census member %q kind = %q, want %q", entry.ID, entry.Kind, wantKind)
	}
	return nil
}

func kindForName(name string) (TestKind, error) {
	switch {
	case strings.HasPrefix(name, "Test"):
		return KindTest, nil
	case strings.HasPrefix(name, "Fuzz"):
		return KindFuzz, nil
	case strings.HasPrefix(name, "Example"):
		return KindExample, nil
	default:
		return "", fmt.Errorf("race/repeat member %q is not a Test, Fuzz, or Example", name)
	}
}

func digestCensus(census []CensusEntry) (string, error) {
	data, err := json.Marshal(census)
	if err != nil {
		return "", fmt.Errorf("encode race/repeat census digest: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}
