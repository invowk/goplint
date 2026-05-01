// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// CountBaselineFindings returns the total number of collected baseline findings.
func CountBaselineFindings(findings map[string][]BaselineFinding) int {
	total := 0
	for _, entries := range findings {
		total += len(entries)
	}
	return total
}

// CollectBaselineFindingsFromAnalysisJSON parses go/analysis -json output and
// returns suppressible findings grouped by category.
func CollectBaselineFindingsFromAnalysisJSON(data []byte) (map[string][]BaselineFinding, error) {
	seen := make(map[string]map[string]BaselineFinding)

	if err := ForEachAnalysisResult(data, func(result AnalysisResult) error {
		for _, analyzers := range result {
			diags, ok := analyzers["goplint"]
			if !ok {
				continue
			}
			for _, d := range diags {
				if d.Category == "" || d.Message == "" {
					continue
				}
				if !IsKnownDiagnosticCategory(d.Category) {
					return fmt.Errorf("unknown goplint category %q", d.Category)
				}
				if !IsBaselineSuppressibleCategory(d.Category) {
					continue
				}
				findingID := FindingIDFromDiagnosticURL(d.URL)
				if findingID == "" {
					findingID = StableFindingID(d.Category, d.Posn, d.Message)
				}

				if seen[d.Category] == nil {
					seen[d.Category] = make(map[string]BaselineFinding)
				}
				seen[d.Category][findingID] = BaselineFinding{
					ID:      findingID,
					Message: d.Message,
				}
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("decoding JSON object: %w", err)
	}

	return baselineFindingSets(seen), nil
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
