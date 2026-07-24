// SPDX-License-Identifier: MPL-2.0

// Package repositoryaudit models one canonical superset goplint traversal and
// the distinct blocking verdicts derived from its exact findings.
package repositoryaudit

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/goplint"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const (
	// FormatVersion is the supported repository-audit result format.
	FormatVersion    = 1
	canonicalPurpose = "canonical-superset"
)

type (
	// InputBinding identifies every exact input that can change audit meaning.
	InputBinding struct {
		WorkspaceDigest        string   `json:"workspace_digest"`
		AnalyzerDigest         string   `json:"analyzer_digest"`
		BaselineDigest         string   `json:"baseline_digest"`
		ExceptionsDigest       string   `json:"exceptions_digest"`
		SemanticManifestDigest string   `json:"semantic_manifest_digest"`
		ToolchainDigest        string   `json:"toolchain_digest"`
		CommandDigest          string   `json:"command_digest"`
		AnalyzerMode           string   `json:"analyzer_mode"`
		Flags                  []string `json:"flags"`
		PackagePatterns        []string `json:"package_patterns"`
		CachePolicy            string   `json:"cache_policy"`
		Purpose                string   `json:"purpose"`
	}

	// Finding is one normalized analyzer or governance diagnostic.
	Finding struct {
		Package     string            `json:"package"`
		Category    string            `json:"category"`
		ID          string            `json:"id"`
		Message     string            `json:"message"`
		Position    string            `json:"position"`
		Meta        map[string]string `json:"meta,omitempty"`
		Disposition string            `json:"disposition"`
	}

	// FindingReference is the stable subset needed for baseline verdicts.
	FindingReference struct {
		Category string `json:"category"`
		ID       string `json:"id"`
		Message  string `json:"message"`
	}

	// BaselineVerdict separates accepted, new, and stale baseline identities.
	BaselineVerdict struct {
		Matched []FindingReference `json:"matched"`
		New     []FindingReference `json:"new"`
		Stale   []FindingReference `json:"stale"`
	}

	// ExceptionVerdict separates patterns exercised by the scan from stale
	// patterns that matched no diagnostic in any package.
	ExceptionVerdict struct {
		MatchedPatterns []string `json:"matched_patterns"`
		StalePatterns   []string `json:"stale_patterns"`
	}

	// PackageCensus is the exact complete analyzed package population.
	PackageCensus struct {
		PackageIDs []string `json:"package_ids"`
		Digest     string   `json:"digest"`
	}

	// ScanMetadata records the single analyzer traversal and its resource-facing
	// execution outcome.
	ScanMetadata struct {
		StartedAt               time.Time `json:"started_at"`
		FinishedAt              time.Time `json:"finished_at"`
		WallDurationNanoseconds int64     `json:"wall_duration_nanoseconds"`
		PeakRSSBytes            int64     `json:"peak_rss_bytes"`
		ExitCode                int       `json:"exit_code"`
		FindingCount            int       `json:"finding_count"`
		PackageCount            int       `json:"package_count"`
	}

	// Result is the self-authenticating canonical repository-audit artifact.
	Result struct {
		FormatVersion int              `json:"format_version"`
		ResultID      string           `json:"result_id"`
		Inputs        InputBinding     `json:"inputs"`
		Findings      []Finding        `json:"findings"`
		Baseline      BaselineVerdict  `json:"baseline"`
		Exceptions    ExceptionVerdict `json:"exceptions"`
		Packages      PackageCensus    `json:"packages"`
		Scan          ScanMetadata     `json:"scan"`
	}

	// BuildOptions contains already captured output from exactly one analyzer
	// traversal plus its reviewed configuration inputs.
	BuildOptions struct {
		Inputs        InputBinding
		Records       []goplint.FindingStreamRecord
		Baseline      *goplint.BaselineConfig
		Exceptions    *goplint.ExceptionConfig
		StalePatterns []string
		PackageIDs    []string
		Scan          ScanMetadata
		WorkspaceRoot string
	}
)

// Build normalizes one superset traversal into distinct reusable verdicts.
func Build(options BuildOptions) (Result, error) {
	if options.Baseline == nil || options.Exceptions == nil {
		return Result{}, errors.New("repository audit requires baseline and exception configuration")
	}
	findings := make([]Finding, 0, len(options.Records))
	seenFindings := make(map[string]FindingReference, len(options.Records))
	seenBaselineIDs := make(map[string]bool)
	baselineVerdict := BaselineVerdict{Matched: []FindingReference{}, New: []FindingReference{}, Stale: []FindingReference{}}
	for _, record := range options.Records {
		if record.Kind != "" && record.Kind != "finding" {
			continue
		}
		if record.Package == "" || record.Category == "" || record.ID == "" || record.Message == "" {
			return Result{}, errors.New("repository audit finding is missing package, category, id, or message")
		}
		reference := FindingReference{Category: record.Category, ID: record.ID, Message: record.Message}
		if previous, exists := seenFindings[record.ID]; exists {
			if previous != reference {
				return Result{}, fmt.Errorf("repository audit finding ID %q identifies multiple semantic findings", record.ID)
			}
			return Result{}, fmt.Errorf("repository audit contains duplicate finding ID %q", record.ID)
		}
		seenFindings[record.ID] = reference
		disposition := "new"
		switch {
		case isGovernanceCategory(record.Category):
			disposition = "governance"
		case !goplint.IsBaselineSuppressibleCategory(record.Category):
			disposition = "always-visible"
			baselineVerdict.New = append(baselineVerdict.New, reference)
		case options.Baseline.ContainsFinding(record.Category, record.ID, record.Message):
			disposition = "baseline-match"
			seenBaselineIDs[record.Category+"\x00"+record.ID] = true
			baselineVerdict.Matched = append(baselineVerdict.Matched, reference)
		default:
			baselineVerdict.New = append(baselineVerdict.New, reference)
		}
		findings = append(findings, Finding{
			Package: record.Package, Category: record.Category, ID: record.ID,
			Message: record.Message, Position: normalizePosition(options.WorkspaceRoot, record.Posn), Meta: cloneMap(record.Meta),
			Disposition: disposition,
		})
	}
	for _, entry := range options.Baseline.Entries() {
		if !seenBaselineIDs[entry.Category+"\x00"+entry.ID] {
			baselineVerdict.Stale = append(baselineVerdict.Stale, FindingReference{
				Category: entry.Category, ID: entry.ID, Message: entry.Message,
			})
		}
	}
	stalePatterns := canonicalStrings(options.StalePatterns)
	staleSet := make(map[string]bool, len(stalePatterns))
	for _, pattern := range stalePatterns {
		staleSet[pattern] = true
	}
	matchedPatterns := make([]string, 0, len(options.Exceptions.Exceptions))
	seenExceptionPatterns := make(map[string]bool, len(options.Exceptions.Exceptions))
	for _, exception := range options.Exceptions.Exceptions {
		if seenExceptionPatterns[exception.Pattern] {
			return Result{}, fmt.Errorf("repository audit exception pattern %q is duplicated", exception.Pattern)
		}
		seenExceptionPatterns[exception.Pattern] = true
		if !staleSet[exception.Pattern] {
			matchedPatterns = append(matchedPatterns, exception.Pattern)
		}
	}
	for pattern := range staleSet {
		if !seenExceptionPatterns[pattern] {
			return Result{}, fmt.Errorf("repository audit stale exception pattern %q is not configured", pattern)
		}
	}
	packages := canonicalStrings(options.PackageIDs)
	packageDigest, err := digestValue(packages)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		FormatVersion: FormatVersion,
		Inputs:        normalizeInputs(options.Inputs),
		Findings:      findings,
		Baseline:      baselineVerdict,
		Exceptions: ExceptionVerdict{
			MatchedPatterns: canonicalStrings(matchedPatterns),
			StalePatterns:   stalePatterns,
		},
		Packages: PackageCensus{PackageIDs: packages, Digest: packageDigest},
		Scan:     options.Scan,
	}
	result.normalize()
	result.Scan.FindingCount = len(result.Findings)
	result.Scan.PackageCount = len(result.Packages.PackageIDs)
	resultID, err := result.CalculateID()
	if err != nil {
		return Result{}, err
	}
	result.ResultID = resultID
	if err := result.Validate(); err != nil {
		return Result{}, err
	}
	return result, nil
}

// CalculateID returns the digest of the complete result with self identity
// cleared.
func (result Result) CalculateID() (string, error) {
	result.ResultID = ""
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode repository audit identity: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}

// NormalizedJSON removes permitted execution timing variance while retaining
// every semantic finding, verdict, population, input, and exit status.
func (result Result) NormalizedJSON() ([]byte, error) {
	result.ResultID = ""
	result.Scan.StartedAt = time.Time{}
	result.Scan.FinishedAt = time.Time{}
	result.Scan.WallDurationNanoseconds = 0
	result.Scan.PeakRSSBytes = 0
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode normalized repository audit: %w", err)
	}
	return append(data, '\n'), nil
}

// Validate checks exact bindings, canonical populations, counts, and identity.
func (result Result) Validate() error {
	if result.FormatVersion != FormatVersion {
		return fmt.Errorf("repository audit format_version = %d, want %d", result.FormatVersion, FormatVersion)
	}
	if result.Inputs.Purpose != canonicalPurpose {
		return fmt.Errorf("repository audit purpose = %q, want %q", result.Inputs.Purpose, canonicalPurpose)
	}
	for name, digest := range map[string]string{
		"result": result.ResultID, "workspace": result.Inputs.WorkspaceDigest,
		"analyzer": result.Inputs.AnalyzerDigest, "baseline": result.Inputs.BaselineDigest,
		"exceptions": result.Inputs.ExceptionsDigest, "toolchain": result.Inputs.ToolchainDigest,
		"semantic manifest": result.Inputs.SemanticManifestDigest,
		"command":           result.Inputs.CommandDigest, "package census": result.Packages.Digest,
	} {
		if err := soundnessevidence.ValidateDigest("repository audit "+name+" digest", digest); err != nil {
			return fmt.Errorf("validate repository audit %s digest: %w", name, err)
		}
	}
	if result.Inputs.AnalyzerMode == "" || result.Inputs.CachePolicy == "" ||
		len(result.Inputs.PackagePatterns) == 0 || len(result.Inputs.Flags) == 0 {
		return errors.New("repository audit has incomplete analyzer mode, cache policy, or package patterns")
	}
	if !slices.Equal(result.Inputs.Flags, canonicalStrings(result.Inputs.Flags)) ||
		!slices.Equal(result.Inputs.PackagePatterns, canonicalStrings(result.Inputs.PackagePatterns)) {
		return errors.New("repository audit flags or package patterns are not canonical")
	}
	if result.Scan.StartedAt.IsZero() || result.Scan.FinishedAt.Before(result.Scan.StartedAt) ||
		result.Scan.WallDurationNanoseconds < 0 || result.Scan.PeakRSSBytes < 0 || result.Scan.FindingCount != len(result.Findings) ||
		result.Scan.PackageCount != len(result.Packages.PackageIDs) {
		return errors.New("repository audit has invalid scan metadata")
	}
	if !slices.IsSortedFunc(result.Findings, compareFindings) ||
		!slices.IsSortedFunc(result.Baseline.Matched, compareFindingReferences) ||
		!slices.IsSortedFunc(result.Baseline.New, compareFindingReferences) ||
		!slices.IsSortedFunc(result.Baseline.Stale, compareFindingReferences) ||
		!slices.IsSorted(result.Exceptions.MatchedPatterns) ||
		!slices.IsSorted(result.Exceptions.StalePatterns) ||
		!slices.IsSorted(result.Packages.PackageIDs) {
		return errors.New("repository audit collections are not canonical")
	}
	packageSet := make(map[string]bool, len(result.Packages.PackageIDs))
	for _, packageID := range result.Packages.PackageIDs {
		packageSet[packageID] = true
	}
	for _, finding := range result.Findings {
		if !packageSet[finding.Package] {
			return fmt.Errorf("repository audit finding %q belongs to package outside the census", finding.ID)
		}
	}
	if err := validateFindingPartitions(result); err != nil {
		return err
	}
	packageDigest, err := digestValue(result.Packages.PackageIDs)
	if err != nil || packageDigest != result.Packages.Digest {
		return errors.New("repository audit package census digest does not match its members")
	}
	calculated, err := result.CalculateID()
	if err != nil {
		return err
	}
	if calculated != result.ResultID {
		return errors.New("repository audit result identity does not match its content")
	}
	return nil
}

func validateFindingPartitions(result Result) error {
	seenFindingIDs := make(map[string]bool, len(result.Findings))
	findingsByReference := make(map[FindingReference]string, len(result.Findings))
	for _, finding := range result.Findings {
		if seenFindingIDs[finding.ID] {
			return fmt.Errorf("repository audit contains duplicate finding ID %q", finding.ID)
		}
		seenFindingIDs[finding.ID] = true
		findingsByReference[FindingReference{Category: finding.Category, ID: finding.ID, Message: finding.Message}] = finding.Disposition
	}
	seenBaseline := make(map[string]string)
	for name, references := range map[string][]FindingReference{
		"matched": result.Baseline.Matched,
		"new":     result.Baseline.New,
		"stale":   result.Baseline.Stale,
	} {
		for _, reference := range references {
			key := reference.Category + "\x00" + reference.ID
			if previous := seenBaseline[key]; previous != "" {
				return fmt.Errorf("repository audit baseline ID %q occurs in both %s and %s", reference.ID, previous, name)
			}
			seenBaseline[key] = name
			if name == "stale" {
				continue
			}
			disposition, exists := findingsByReference[reference]
			if !exists || (name == "matched" && disposition != "baseline-match") ||
				(name == "new" && disposition != "new" && disposition != "always-visible") {
				return fmt.Errorf("repository audit baseline %s reference %q has no matching finding disposition", name, reference.ID)
			}
		}
	}
	if len(result.Exceptions.MatchedPatterns)+len(result.Exceptions.StalePatterns) == 0 {
		return nil
	}
	seenPatterns := make(map[string]string)
	for name, patterns := range map[string][]string{
		"matched": result.Exceptions.MatchedPatterns, "stale": result.Exceptions.StalePatterns,
	} {
		for _, pattern := range patterns {
			if previous := seenPatterns[pattern]; previous != "" {
				return fmt.Errorf("repository audit exception pattern %q occurs in both %s and %s", pattern, previous, name)
			}
			seenPatterns[pattern] = name
		}
	}
	return nil
}

func (result *Result) normalize() {
	slices.SortFunc(result.Findings, compareFindings)
	slices.SortFunc(result.Baseline.Matched, compareFindingReferences)
	slices.SortFunc(result.Baseline.New, compareFindingReferences)
	slices.SortFunc(result.Baseline.Stale, compareFindingReferences)
}

func normalizeInputs(inputs InputBinding) InputBinding {
	inputs.PackagePatterns = canonicalStrings(inputs.PackagePatterns)
	inputs.Flags = canonicalStrings(inputs.Flags)
	if inputs.Purpose == "" {
		inputs.Purpose = canonicalPurpose
	}
	return inputs
}

func normalizePosition(root, position string) string {
	if root == "" || position == "" {
		return filepath.ToSlash(position)
	}
	cleanRoot := filepath.Clean(root)
	cleanPosition := filepath.Clean(position)
	if relative, exists := strings.CutPrefix(cleanPosition, cleanRoot+string(filepath.Separator)); exists {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(position)
}

func isGovernanceCategory(category string) bool {
	return category == goplint.CategoryStaleException || category == goplint.CategoryOverdueReview
}

func compareFindings(left, right Finding) int {
	for _, compared := range []int{
		strings.Compare(left.Package, right.Package), strings.Compare(left.Category, right.Category),
		strings.Compare(left.ID, right.ID), strings.Compare(left.Message, right.Message),
		strings.Compare(left.Position, right.Position),
	} {
		if compared != 0 {
			return compared
		}
	}
	return 0
}

func compareFindingReferences(left, right FindingReference) int {
	if compared := strings.Compare(left.Category, right.Category); compared != 0 {
		return compared
	}
	if compared := strings.Compare(left.ID, right.ID); compared != 0 {
		return compared
	}
	return strings.Compare(left.Message, right.Message)
}

func canonicalStrings(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return slices.Compact(result)
}

func digestValue(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode repository audit digest input: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}

func cloneMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	maps.Copy(result, source)
	return result
}
