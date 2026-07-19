// SPDX-License-Identifier: MPL-2.0

// Package stableidmigration builds deterministic old-to-new finding-ID reports
// from canonical goplint finding streams.
package stableidmigration

import (
	"bufio"
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const SchemaVersion = 2

type findingRecord struct {
	Kind     string            `json:"kind,omitempty"`
	Package  string            `json:"package,omitempty"`
	Category string            `json:"category,omitempty"`
	ID       string            `json:"id,omitempty"`
	Message  string            `json:"message,omitempty"`
	Posn     string            `json:"posn,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

type semanticFinding struct {
	Package  string `json:"package"`
	Category string `json:"category"`
	Position string `json:"position"`
	Message  string `json:"message"`
}

type ScanSummary struct {
	Population      int    `json:"population"`
	CanonicalSHA256 string `json:"canonical_sha256"`
}

type Counts struct {
	Retained int `json:"retained"`
	Changed  int `json:"changed"`
	Added    int `json:"added"`
	Removed  int `json:"removed"`
}

type Migration struct {
	SemanticFinding semanticFinding `json:"semantic_finding"`
	OldID           string          `json:"old_id,omitempty"`
	NewID           string          `json:"new_id,omitempty"`
	Status          string          `json:"status"`
}

type Collision struct {
	Scan     string            `json:"scan"`
	ID       string            `json:"id"`
	Findings []semanticFinding `json:"findings"`
}

type Duplicate struct {
	Scan            string          `json:"scan"`
	SemanticFinding semanticFinding `json:"semantic_finding"`
	ID              string          `json:"id"`
	Count           int             `json:"count"`
}

// PopulationChangeReview binds an intentionally changed scan population to
// its exact semantic-finding set. Counts alone are insufficient: the digest
// prevents a same-sized but different addition/removal set from inheriting a
// prior review.
type PopulationChangeReview struct {
	Status          string `json:"status"`
	Category        string `json:"category"`
	Population      int    `json:"population"`
	CanonicalSHA256 string `json:"canonical_sha256"`
	Reason          string `json:"reason"`
	Evidence        string `json:"evidence"`
}

type PopulationChange struct {
	Status          string `json:"status"`
	Category        string `json:"category"`
	Population      int    `json:"population"`
	CanonicalSHA256 string `json:"canonical_sha256"`
	Reviewed        bool   `json:"reviewed"`
	Reason          string `json:"reason,omitempty"`
	Evidence        string `json:"evidence,omitempty"`
}

type Report struct {
	SchemaVersion     int                `json:"schema_version"`
	Deterministic     bool               `json:"deterministic"`
	Accepted          bool               `json:"accepted"`
	OldScan           ScanSummary        `json:"old_scan"`
	NewScan           ScanSummary        `json:"new_scan"`
	RepeatScan        ScanSummary        `json:"repeat_scan"`
	Counts            Counts             `json:"counts"`
	Migrations        []Migration        `json:"migrations"`
	PopulationChanges []PopulationChange `json:"population_changes"`
	Collisions        []Collision        `json:"collisions"`
	Duplicates        []Duplicate        `json:"duplicates"`
	Failures          []string           `json:"failures"`
}

// Build parses an old scan and two independently executed new scans. It
// accepts a migration only when the repeated new scans are byte-identical
// after canonical sorting, every shared semantic finding maps one-to-one, no
// ID collision or duplicate report remains, and every added/removed population
// is bound to an exact reviewed digest.
func Build(oldData, newData, repeatData []byte) (Report, error) {
	return BuildReviewed(oldData, newData, repeatData, nil)
}

// BuildReviewed is Build with exact reviewed population changes. Reviews do
// not weaken collision, duplicate, determinism, or ambiguous-mapping checks.
func BuildReviewed(
	oldData,
	newData,
	repeatData []byte,
	reviews []PopulationChangeReview,
) (Report, error) {
	oldRecords, err := parseFindings("old", oldData)
	if err != nil {
		return Report{}, err
	}
	newRecords, err := parseFindings("new", newData)
	if err != nil {
		return Report{}, err
	}
	repeatRecords, err := parseFindings("repeat", repeatData)
	if err != nil {
		return Report{}, err
	}

	oldCanonical, err := canonicalRecords(oldRecords)
	if err != nil {
		return Report{}, fmt.Errorf("canonicalize old scan: %w", err)
	}
	newCanonical, err := canonicalRecords(newRecords)
	if err != nil {
		return Report{}, fmt.Errorf("canonicalize new scan: %w", err)
	}
	repeatCanonical, err := canonicalRecords(repeatRecords)
	if err != nil {
		return Report{}, fmt.Errorf("canonicalize repeat scan: %w", err)
	}
	report := Report{
		SchemaVersion: SchemaVersion,
		Deterministic: bytes.Equal(newCanonical, repeatCanonical),
		OldScan:       summarize(oldRecords, oldCanonical),
		NewScan:       summarize(newRecords, newCanonical),
		RepeatScan:    summarize(repeatRecords, repeatCanonical),
	}

	oldByFinding := recordsBySemanticFinding(oldRecords)
	newByFinding := recordsBySemanticFinding(newRecords)
	report.Migrations, report.Counts = buildMigrations(oldByFinding, newByFinding)
	report.PopulationChanges = buildPopulationChanges(report.Migrations)
	reviewFailures := applyPopulationChangeReviews(report.PopulationChanges, reviews)
	report.Collisions = append(report.Collisions, findCollisions("old", oldRecords)...)
	report.Collisions = append(report.Collisions, findCollisions("new", newRecords)...)
	report.Collisions = append(report.Collisions, findCollisions("repeat", repeatRecords)...)
	report.Duplicates = append(report.Duplicates, findDuplicates("old", oldRecords)...)
	report.Duplicates = append(report.Duplicates, findDuplicates("new", newRecords)...)
	report.Duplicates = append(report.Duplicates, findDuplicates("repeat", repeatRecords)...)

	if !report.Deterministic {
		report.Failures = append(report.Failures, "repeated new scans are not byte-identical after canonical sorting")
	}
	if len(report.Collisions) > 0 {
		report.Failures = append(report.Failures, "one or more stable IDs identify multiple semantic findings")
	}
	if len(report.Duplicates) > 0 {
		report.Failures = append(report.Failures, "one or more semantic findings were emitted more than once")
	}
	report.Failures = append(report.Failures, reviewFailures...)
	for _, migration := range report.Migrations {
		if migration.Status == "ambiguous" {
			report.Failures = append(report.Failures, "one or more semantic findings map to multiple IDs within a scan")
			break
		}
	}
	report.Accepted = len(report.Failures) == 0
	return report, nil
}

func buildPopulationChanges(migrations []Migration) []PopulationChange {
	type populationKey struct {
		Status   string
		Category string
	}
	groups := make(map[populationKey][]semanticFinding)
	for _, migration := range migrations {
		if migration.Status != "added" && migration.Status != "removed" {
			continue
		}
		key := populationKey{Status: migration.Status, Category: migration.SemanticFinding.Category}
		groups[key] = append(groups[key], migration.SemanticFinding)
	}
	changes := make([]PopulationChange, 0, len(groups))
	for key, findings := range groups {
		slices.SortFunc(findings, compareSemanticFindings)
		encoded, err := json.Marshal(findings)
		if err != nil {
			panic("marshal semantic population: " + err.Error())
		}
		digest := sha256.Sum256(encoded)
		changes = append(changes, PopulationChange{
			Status:          key.Status,
			Category:        key.Category,
			Population:      len(findings),
			CanonicalSHA256: hex.EncodeToString(digest[:]),
		})
	}
	slices.SortFunc(changes, func(left, right PopulationChange) int {
		if result := cmp.Compare(left.Status, right.Status); result != 0 {
			return result
		}
		return cmp.Compare(left.Category, right.Category)
	})
	return changes
}

func applyPopulationChangeReviews(
	changes []PopulationChange,
	reviews []PopulationChangeReview,
) []string {
	type reviewKey struct {
		Status   string
		Category string
	}
	reviewByKey := make(map[reviewKey]PopulationChangeReview, len(reviews))
	failures := make([]string, 0)
	for _, review := range reviews {
		key := reviewKey{Status: review.Status, Category: review.Category}
		if review.Status != "added" && review.Status != "removed" {
			failures = append(failures, fmt.Sprintf(
				"population review for category %q has invalid status %q",
				review.Category,
				review.Status,
			))
			continue
		}
		if review.Category == "" || review.Population <= 0 || review.CanonicalSHA256 == "" ||
			strings.TrimSpace(review.Reason) == "" || strings.TrimSpace(review.Evidence) == "" {
			failures = append(failures, fmt.Sprintf(
				"population review %s/%s is incomplete",
				review.Status,
				review.Category,
			))
			continue
		}
		if _, exists := reviewByKey[key]; exists {
			failures = append(failures, fmt.Sprintf(
				"population review %s/%s is duplicated",
				review.Status,
				review.Category,
			))
			continue
		}
		reviewByKey[key] = review
	}
	for index := range changes {
		change := &changes[index]
		key := reviewKey{Status: change.Status, Category: change.Category}
		review, exists := reviewByKey[key]
		if !exists {
			failures = append(failures, fmt.Sprintf(
				"unexplained %s population for category %q (population=%d sha256=%s)",
				change.Status,
				change.Category,
				change.Population,
				change.CanonicalSHA256,
			))
			continue
		}
		delete(reviewByKey, key)
		if review.Population != change.Population || review.CanonicalSHA256 != change.CanonicalSHA256 {
			failures = append(failures, fmt.Sprintf(
				"population review %s/%s does not match current population digest",
				change.Status,
				change.Category,
			))
			continue
		}
		change.Reviewed = true
		change.Reason = review.Reason
		change.Evidence = review.Evidence
	}
	for key := range reviewByKey {
		failures = append(failures, fmt.Sprintf(
			"population review %s/%s is stale because no matching population change exists",
			key.Status,
			key.Category,
		))
	}
	slices.Sort(failures)
	return failures
}

func Marshal(report Report) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal stable-ID migration report: %w", err)
	}
	return append(data, '\n'), nil
}

func parseFindings(scan string, data []byte) ([]findingRecord, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	records := make([]findingRecord, 0)
	line := 0
	for scanner.Scan() {
		line++
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var record findingRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("decode %s scan line %d: %w", scan, line, err)
		}
		if record.Kind != "" && record.Kind != "finding" {
			continue
		}
		if record.Package == "" || record.Category == "" || record.ID == "" || record.Message == "" {
			return nil, fmt.Errorf("decode %s scan line %d: finding is missing package, category, ID, or message", scan, line)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s scan: %w", scan, err)
	}
	return records, nil
}

func canonicalRecords(records []findingRecord) ([]byte, error) {
	ordered := slices.Clone(records)
	slices.SortFunc(ordered, compareRecords)
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	for _, record := range ordered {
		record.Posn = normalizePosition(record.Posn)
		if err := encoder.Encode(record); err != nil {
			return nil, fmt.Errorf("encode canonical finding: %w", err)
		}
	}
	return out.Bytes(), nil
}

func compareRecords(left, right findingRecord) int {
	for _, values := range [][2]string{
		{left.Package, right.Package},
		{left.Category, right.Category},
		{normalizePosition(left.Posn), normalizePosition(right.Posn)},
		{left.Message, right.Message},
		{left.ID, right.ID},
		{canonicalMeta(left.Meta), canonicalMeta(right.Meta)},
	} {
		if result := cmp.Compare(values[0], values[1]); result != 0 {
			return result
		}
	}
	return 0
}

func canonicalMeta(meta map[string]string) string {
	encoded, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func summarize(records []findingRecord, canonical []byte) ScanSummary {
	digest := sha256.Sum256(canonical)
	return ScanSummary{
		Population:      len(records),
		CanonicalSHA256: hex.EncodeToString(digest[:]),
	}
}

func recordsBySemanticFinding(records []findingRecord) map[semanticFinding]map[string]struct{} {
	result := make(map[semanticFinding]map[string]struct{}, len(records))
	for _, record := range records {
		finding := semanticFindingForRecord(record)
		if result[finding] == nil {
			result[finding] = make(map[string]struct{})
		}
		result[finding][record.ID] = struct{}{}
	}
	return result
}

func buildMigrations(
	oldByFinding map[semanticFinding]map[string]struct{},
	newByFinding map[semanticFinding]map[string]struct{},
) ([]Migration, Counts) {
	keys := make([]semanticFinding, 0, len(oldByFinding)+len(newByFinding))
	seen := make(map[semanticFinding]struct{}, cap(keys))
	for finding := range oldByFinding {
		keys = append(keys, finding)
		seen[finding] = struct{}{}
	}
	for finding := range newByFinding {
		if _, ok := seen[finding]; !ok {
			keys = append(keys, finding)
		}
	}
	slices.SortFunc(keys, compareSemanticFindings)

	migrations := make([]Migration, 0, len(keys))
	var counts Counts
	for _, finding := range keys {
		oldIDs := sortedSet(oldByFinding[finding])
		newIDs := sortedSet(newByFinding[finding])
		migration := Migration{SemanticFinding: finding}
		switch {
		case len(oldIDs) > 1 || len(newIDs) > 1:
			migration.Status = "ambiguous"
			migration.OldID = strings.Join(oldIDs, ",")
			migration.NewID = strings.Join(newIDs, ",")
		case len(oldIDs) == 0:
			migration.Status = "added"
			migration.NewID = newIDs[0]
			counts.Added++
		case len(newIDs) == 0:
			migration.Status = "removed"
			migration.OldID = oldIDs[0]
			counts.Removed++
		case oldIDs[0] == newIDs[0]:
			migration.Status = "retained"
			migration.OldID = oldIDs[0]
			migration.NewID = newIDs[0]
			counts.Retained++
		default:
			migration.Status = "changed"
			migration.OldID = oldIDs[0]
			migration.NewID = newIDs[0]
			counts.Changed++
		}
		migrations = append(migrations, migration)
	}
	return migrations, counts
}

func findCollisions(scan string, records []findingRecord) []Collision {
	byID := make(map[string]map[semanticFinding]struct{}, len(records))
	for _, record := range records {
		if byID[record.ID] == nil {
			byID[record.ID] = make(map[semanticFinding]struct{})
		}
		byID[record.ID][semanticFindingForRecord(record)] = struct{}{}
	}
	collisions := make([]Collision, 0)
	for id, findingSet := range byID {
		if len(findingSet) < 2 {
			continue
		}
		findings := make([]semanticFinding, 0, len(findingSet))
		for finding := range findingSet {
			findings = append(findings, finding)
		}
		slices.SortFunc(findings, compareSemanticFindings)
		collisions = append(collisions, Collision{Scan: scan, ID: id, Findings: findings})
	}
	slices.SortFunc(collisions, func(left, right Collision) int {
		if result := cmp.Compare(left.Scan, right.Scan); result != 0 {
			return result
		}
		return cmp.Compare(left.ID, right.ID)
	})
	return collisions
}

func findDuplicates(scan string, records []findingRecord) []Duplicate {
	type duplicateIdentity struct {
		SemanticFinding semanticFinding
		ID              string
	}
	counts := make(map[duplicateIdentity]int, len(records))
	for _, record := range records {
		counts[duplicateIdentity{
			SemanticFinding: semanticFindingForRecord(record),
			ID:              record.ID,
		}]++
	}
	duplicates := make([]Duplicate, 0)
	for identity, count := range counts {
		if count < 2 {
			continue
		}
		duplicates = append(duplicates, Duplicate{
			Scan:            scan,
			SemanticFinding: identity.SemanticFinding,
			ID:              identity.ID,
			Count:           count,
		})
	}
	slices.SortFunc(duplicates, func(left, right Duplicate) int {
		if result := cmp.Compare(left.Scan, right.Scan); result != 0 {
			return result
		}
		if result := compareSemanticFindings(left.SemanticFinding, right.SemanticFinding); result != 0 {
			return result
		}
		return cmp.Compare(left.ID, right.ID)
	})
	return duplicates
}

func semanticFindingForRecord(record findingRecord) semanticFinding {
	return semanticFinding{
		Package:  record.Package,
		Category: record.Category,
		Position: normalizePosition(record.Posn),
		Message:  record.Message,
	}
}

func compareSemanticFindings(left, right semanticFinding) int {
	for _, values := range [][2]string{
		{left.Package, right.Package},
		{left.Category, right.Category},
		{left.Position, right.Position},
		{left.Message, right.Message},
	} {
		if result := cmp.Compare(values[0], values[1]); result != 0 {
			return result
		}
	}
	return 0
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func normalizePosition(position string) string {
	position = strings.ReplaceAll(position, "\\", "/")
	lastSlash := strings.LastIndexByte(position, '/')
	if lastSlash >= 0 {
		return position[lastSlash+1:]
	}
	return position
}
