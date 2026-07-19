// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
)

// AnalysisResult represents the go/analysis -json output structure.
// The JSON is a map from package path to per-analyzer results.
type AnalysisResult map[string]map[string][]AnalysisDiagnostic

// AnalysisDiagnostic is a single diagnostic in the -json output.
//
//goplint:ignore -- go/analysis JSON DTO fields are wire-format primitives.
type AnalysisDiagnostic struct {
	Posn     string `json:"posn"`
	Message  string `json:"message"`
	Category string `json:"category"`
	URL      string `json:"url"`
}

// CollectBaselineFindingsFromStream parses analyzer finding JSONL records into
// baseline findings grouped by category.
func CollectBaselineFindingsFromStream(data []byte) (map[string][]BaselineFinding, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string][]BaselineFinding{}, nil
	}

	seen := make(map[string]map[string]BaselineFinding)
	seenIDs := make(map[string]findingStreamIdentity)
	if err := forEachFindingsRecord(data, func(record FindingStreamRecord) error {
		if record.Kind != "" && record.Kind != "finding" {
			return nil
		}
		if record.Category == "" || record.Message == "" || record.ID == "" {
			return errors.New("decoding findings record: missing required fields")
		}
		if !IsKnownDiagnosticCategory(record.Category) {
			return fmt.Errorf("unknown goplint category %q", record.Category)
		}
		if err := recordFindingStreamIdentity(seenIDs, record); err != nil {
			return err
		}
		if isProtocolInconclusiveFinding(record.Category, record.Meta) {
			return fmt.Errorf("protocol inconclusive category %q is always visible and cannot be baselined", record.Category)
		}
		if !IsBaselineSuppressibleCategory(record.Category) {
			return nil
		}
		if seen[record.Category] == nil {
			seen[record.Category] = make(map[string]BaselineFinding)
		}
		seen[record.Category][record.ID] = BaselineFinding{
			ID:      record.ID,
			Message: record.Message,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return baselineFindingSets(seen), nil
}

type findingStreamIdentity struct {
	packagePath string
	category    string
	message     string
	position    string
}

func recordFindingStreamIdentity(seen map[string]findingStreamIdentity, record FindingStreamRecord) error {
	identity := findingStreamIdentity{
		packagePath: record.Package,
		category:    record.Category,
		message:     record.Message,
		position:    record.Posn,
	}
	previous, ok := seen[record.ID]
	if !ok {
		seen[record.ID] = identity
		return nil
	}
	if previous == identity {
		return fmt.Errorf(
			"duplicate finding ID %q for %s finding at %q",
			record.ID,
			record.Category,
			record.Posn,
		)
	}
	return fmt.Errorf(
		"collided finding ID %q identifies both %s %q at %q and %s %q at %q",
		record.ID,
		previous.category,
		previous.message,
		previous.position,
		record.Category,
		record.Message,
		record.Posn,
	)
}

// CountBaselineFindings returns the total number of collected baseline findings.
func CountBaselineFindings(findings map[string][]BaselineFinding) int {
	total := 0
	for _, entries := range findings {
		total += len(entries)
	}
	return total
}

// CountBaselineDiagnosticsFromAnalysisJSON validates go/analysis -json output
// and returns the number of suppressible diagnostics. The analysis driver does
// not preserve analysis.Diagnostic.URL, so this parser deliberately does not
// require canonical finding IDs.
func CountBaselineDiagnosticsFromAnalysisJSON(data []byte) (int, error) {
	counts, err := baselineAnalysisDiagnosticCounts(data)
	if err != nil {
		return 0, err
	}

	total := 0
	for _, count := range counts {
		total += count
	}
	return total, nil
}

// ValidateBaselineFindingsCoverage verifies that every suppressible diagnostic
// in go/analysis -json output has a matching canonical-ID record in the
// internal findings stream. Extra stream records are allowed because callers
// may supply a filtered or empty analysis stream in tests and integrations.
func ValidateBaselineFindingsCoverage(findingsData, analysisData []byte) (int, int, error) {
	streamCounts, err := baselineStreamDiagnosticCounts(findingsData)
	if err != nil {
		return 0, 0, err
	}
	analysisCounts, err := baselineAnalysisDiagnosticCounts(analysisData)
	if err != nil {
		return 0, 0, err
	}

	streamTotal := diagnosticCountTotal(streamCounts)
	analysisTotal := diagnosticCountTotal(analysisCounts)
	missing := make([]baselineDiagnosticKey, 0)
	for key, count := range analysisCounts {
		if streamCounts[key] < count {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return streamTotal, analysisTotal, nil
	}

	slices.SortFunc(missing, func(a, b baselineDiagnosticKey) int {
		if result := cmp.Compare(a.category, b.category); result != 0 {
			return result
		}
		if result := cmp.Compare(a.posn, b.posn); result != 0 {
			return result
		}
		return cmp.Compare(a.message, b.message)
	})
	key := missing[0]
	return streamTotal, analysisTotal, fmt.Errorf(
		"findings stream is missing %d occurrence(s) of %s diagnostic at %q: %s",
		analysisCounts[key]-streamCounts[key],
		key.category,
		key.posn,
		key.message,
	)
}

// CollectBaselineFindingsFromAnalysisJSON parses go/analysis -json output and
// returns suppressible findings grouped by category.
func CollectBaselineFindingsFromAnalysisJSON(data []byte) (map[string][]BaselineFinding, error) {
	seen := make(map[string]map[string]BaselineFinding)
	var records []FindingStreamRecord

	if err := ForEachAnalysisResult(data, func(result AnalysisResult) error {
		for packagePath, analyzers := range result {
			diags, ok := analyzers["goplint"]
			if !ok {
				continue
			}
			for _, d := range diags {
				if d.Category == "" || d.Message == "" {
					continue
				}
				records = append(records, FindingStreamRecord{
					Package:  packagePath,
					Category: d.Category,
					ID:       FindingIDFromDiagnosticURL(d.URL),
					Message:  d.Message,
					Posn:     d.Posn,
					Meta:     diagnosticMeta(d),
				})
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("decoding JSON object: %w", err)
	}

	slices.SortFunc(records, func(left, right FindingStreamRecord) int {
		for _, values := range [][2]string{
			{left.Package, right.Package},
			{left.Category, right.Category},
			{left.Posn, right.Posn},
			{left.Message, right.Message},
			{left.ID, right.ID},
		} {
			if result := cmp.Compare(values[0], values[1]); result != 0 {
				return result
			}
		}
		return 0
	})
	seenIDs := make(map[string]findingStreamIdentity)
	for _, record := range records {
		if !IsKnownDiagnosticCategory(record.Category) {
			return nil, fmt.Errorf("decoding JSON object: unknown goplint category %q", record.Category)
		}
		if isProtocolInconclusiveFinding(record.Category, record.Meta) {
			return nil, fmt.Errorf(
				"decoding JSON object: protocol inconclusive category %q is always visible and cannot be baselined",
				record.Category,
			)
		}
		if record.ID == "" {
			return nil, fmt.Errorf(
				"decoding JSON object: goplint diagnostic category %q at %q is missing canonical finding ID metadata",
				record.Category,
				record.Posn,
			)
		}
		if err := recordFindingStreamIdentity(seenIDs, record); err != nil {
			return nil, fmt.Errorf("decoding JSON object: %w", err)
		}
		if !IsBaselineSuppressibleCategory(record.Category) {
			continue
		}
		if seen[record.Category] == nil {
			seen[record.Category] = make(map[string]BaselineFinding)
		}
		seen[record.Category][record.ID] = BaselineFinding{
			ID:      record.ID,
			Message: record.Message,
		}
	}

	return baselineFindingSets(seen), nil
}

func diagnosticMeta(d AnalysisDiagnostic) map[string]string {
	meta := make(map[string]string)
	parsed, err := url.Parse(d.URL)
	if err != nil {
		return nil
	}
	for key, values := range parsed.Query() {
		if len(values) > 0 && values[0] != "" {
			meta[key] = values[0]
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

// ForEachAnalysisResult decodes each concatenated go/analysis JSON object.
func ForEachAnalysisResult(data []byte, fn func(result AnalysisResult) error) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var result AnalysisResult
		if err := decoder.Decode(&result); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decoding analysis stream: %w", err)
		}
		if fn == nil {
			continue
		}
		if err := fn(result); err != nil {
			return err
		}
	}
}

type baselineDiagnosticKey struct {
	category string
	message  string
	posn     string
}

func baselineStreamDiagnosticCounts(data []byte) (map[baselineDiagnosticKey]int, error) {
	counts := make(map[baselineDiagnosticKey]int)
	if err := forEachFindingsRecord(data, func(record FindingStreamRecord) error {
		if record.Kind != "" && record.Kind != "finding" {
			return nil
		}
		if record.Category == "" || record.Message == "" || record.ID == "" {
			return errors.New("decoding findings record: missing required fields")
		}
		if !IsKnownDiagnosticCategory(record.Category) {
			return fmt.Errorf("unknown goplint category %q", record.Category)
		}
		if isProtocolInconclusiveFinding(record.Category, record.Meta) {
			return fmt.Errorf("protocol inconclusive category %q is always visible and cannot be baselined", record.Category)
		}
		if !IsBaselineSuppressibleCategory(record.Category) {
			return nil
		}
		counts[baselineDiagnosticKey{
			category: record.Category,
			message:  record.Message,
			posn:     record.Posn,
		}]++
		return nil
	}); err != nil {
		return nil, err
	}
	return counts, nil
}

func baselineAnalysisDiagnosticCounts(data []byte) (map[baselineDiagnosticKey]int, error) {
	counts := make(map[baselineDiagnosticKey]int)
	if err := ForEachAnalysisResult(data, func(result AnalysisResult) error {
		for _, analyzers := range result {
			for _, diagnostic := range analyzers["goplint"] {
				if diagnostic.Category == "" || diagnostic.Message == "" {
					continue
				}
				if !IsKnownDiagnosticCategory(diagnostic.Category) {
					return fmt.Errorf("unknown goplint category %q", diagnostic.Category)
				}
				if isProtocolInconclusiveDiagnostic(diagnostic) {
					return fmt.Errorf(
						"protocol inconclusive category %q is always visible and cannot be baselined",
						diagnostic.Category,
					)
				}
				if !IsBaselineSuppressibleCategory(diagnostic.Category) {
					continue
				}
				counts[baselineDiagnosticKey{
					category: diagnostic.Category,
					message:  diagnostic.Message,
					posn:     diagnostic.Posn,
				}]++
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("decoding JSON object: %w", err)
	}
	return counts, nil
}

func isProtocolInconclusiveFinding(category string, meta map[string]string) bool {
	return IsProtocolInconclusiveCategory(category) ||
		meta["cfg_outcome_status"] == cfgRefinementStatusInconclusive
}

func isProtocolInconclusiveDiagnostic(diagnostic AnalysisDiagnostic) bool {
	return isProtocolInconclusiveFinding(
		diagnostic.Category,
		findingMetaFromDiagnosticURL(diagnostic.URL),
	)
}

func diagnosticCountTotal(counts map[baselineDiagnosticKey]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func forEachFindingsRecord(data []byte, fn func(record FindingStreamRecord) error) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}

	scanner := bytes.NewBuffer(data)
	for {
		line, err := scanner.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("reading findings stream: %w", err)
		}
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			var record FindingStreamRecord
			if unmarshalErr := json.Unmarshal(line, &record); unmarshalErr != nil {
				return fmt.Errorf("decoding findings record: %w", unmarshalErr)
			}
			if fn != nil {
				if callbackErr := fn(record); callbackErr != nil {
					return callbackErr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func baselineFindingSets(seen map[string]map[string]BaselineFinding) map[string][]BaselineFinding {
	findings := make(map[string][]BaselineFinding, len(seen))
	for category, entries := range seen {
		out := make([]BaselineFinding, 0, len(entries))
		for _, entry := range entries {
			out = append(out, entry)
		}
		findings[category] = out
	}
	return findings
}
