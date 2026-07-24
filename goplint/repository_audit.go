// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
)

// ParseFindingStream validates and returns every canonical diagnostic finding
// emitted by one analyzer traversal. Non-finding evidence records are ignored.
func ParseFindingStream(data []byte) ([]FindingStreamRecord, error) {
	records := make([]FindingStreamRecord, 0)
	seen := make(map[string]findingStreamIdentity)
	if err := forEachFindingsRecord(data, func(record FindingStreamRecord) error {
		if record.Kind != "" && record.Kind != "finding" {
			return nil
		}
		if record.Package == "" || record.Category == "" || record.ID == "" || record.Message == "" {
			return errors.New("decoding findings record: missing required package, category, id, or message")
		}
		if !IsKnownDiagnosticCategory(record.Category) {
			return fmt.Errorf("unknown goplint category %q", record.Category)
		}
		if err := recordFindingStreamIdentity(seen, record); err != nil {
			return err
		}
		records = append(records, record)
		return nil
	}); err != nil {
		return nil, err
	}
	slices.SortFunc(records, func(left, right FindingStreamRecord) int {
		for _, compared := range []int{
			cmp.Compare(left.Package, right.Package), cmp.Compare(left.Category, right.Category),
			cmp.Compare(left.ID, right.ID), cmp.Compare(left.Message, right.Message), cmp.Compare(left.Posn, right.Posn),
		} {
			if compared != 0 {
				return compared
			}
		}
		return 0
	})
	return records, nil
}

// AnalysisPackageCensus returns every package key present in go/analysis JSON,
// including packages with no diagnostics.
func AnalysisPackageCensus(data []byte) ([]string, error) {
	seen := make(map[string]bool)
	if err := ForEachAnalysisResult(data, func(result AnalysisResult) error {
		for packageID := range result {
			seen[packageID] = true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	packages := make([]string, 0, len(seen))
	for packageID := range seen {
		packages = append(packages, packageID)
	}
	slices.Sort(packages)
	return packages, nil
}

// ValidateFindingStreamCoverage proves exact all-category correspondence
// between go/analysis JSON diagnostics and canonical-ID stream records.
func ValidateFindingStreamCoverage(findingsData, analysisData []byte) error {
	type diagnosticKey struct {
		pkg, category, position, message string
	}
	analysisCounts := make(map[diagnosticKey]int)
	if err := ForEachAnalysisResult(analysisData, func(result AnalysisResult) error {
		for packageID, analyzers := range result {
			for _, diagnostic := range analyzers["goplint"] {
				analysisCounts[diagnosticKey{
					pkg: packageID, category: diagnostic.Category,
					position: diagnostic.Posn, message: diagnostic.Message,
				}]++
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("decode analysis JSON: %w", err)
	}
	records, err := ParseFindingStream(findingsData)
	if err != nil {
		return fmt.Errorf("decode findings stream: %w", err)
	}
	streamCounts := make(map[diagnosticKey]int, len(records))
	for _, record := range records {
		streamCounts[diagnosticKey{
			pkg: record.Package, category: record.Category,
			position: record.Posn, message: record.Message,
		}]++
	}
	for key, count := range analysisCounts {
		if streamCounts[key] != count {
			return fmt.Errorf("findings stream count for %s diagnostic %q in package %q = %d, want %d", key.category, key.message, key.pkg, streamCounts[key], count)
		}
	}
	for key, count := range streamCounts {
		if analysisCounts[key] != count {
			return fmt.Errorf("analysis JSON count for %s finding %q in package %q = %d, want %d", key.category, key.message, key.pkg, analysisCounts[key], count)
		}
	}
	return nil
}
