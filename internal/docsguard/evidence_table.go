// SPDX-License-Identifier: MPL-2.0

package docsguard

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var (
	claimTableHeaderPattern    = regexp.MustCompile(`^\|\s*Guarantee\s*\|`)
	claimTableSeparatorPattern = regexp.MustCompile(`^\|[\s:|-]+\|?$`)
)

// checkEvidenceTables validates every claim-to-evidence table in the
// document. A claim table starts with a `| Guarantee | ... |` header row;
// each data row must anchor to at least one executable artifact through the
// backticked tokens and identifier-shaped words of its second and third
// cells.
func checkEvidenceTables(index *artifactIndex, document string, lines []string, report *Report) {
	docDir := path.Dir(document)
	inTable := false
	for lineIndex, text := range lines {
		lineNumber := lineIndex + 1
		switch {
		case claimTableHeaderPattern.MatchString(text):
			inTable = true
		case !inTable:
		case claimTableSeparatorPattern.MatchString(text):
		case !strings.HasPrefix(strings.TrimSpace(text), "|"):
			inTable = false
		default:
			checkClaimRow(index, document, docDir, lineNumber, text, report)
		}
	}
}

// checkClaimRow validates one claim table data row. Path-shaped backticked
// tokens that do not exist are hard failures; other unresolved tokens simply
// do not count as anchors. A row with zero resolved anchors is a dangling
// claim.
func checkClaimRow(index *artifactIndex, document, docDir string, lineNumber int, text string, report *Report) {
	cells := splitTableRow(text)
	if len(cells) < 3 {
		report.Findings = append(report.Findings, Finding{
			Document:  document,
			Line:      lineNumber,
			Reference: strings.TrimSpace(text),
			Message:   fmt.Sprintf("claim row %q does not have guarantee, boundary, and proof cells", strings.TrimSpace(text)),
		})
		return
	}
	guarantee := cells[0]
	claimText := cells[1] + " " + cells[2]
	anchors := 0
	for _, match := range inlineCodePattern.FindAllStringSubmatch(claimText, -1) {
		reference := match[1]
		resolved, finding := resolveClaimToken(index, document, docDir, lineNumber, reference)
		if resolved {
			anchors++
		}
		if finding != nil {
			report.Findings = append(report.Findings, *finding)
		}
	}
	anchors += countPlainWordAnchors(index, claimText)
	if anchors == 0 {
		report.Findings = append(report.Findings, Finding{
			Document:  document,
			Line:      lineNumber,
			Reference: guarantee,
			Message:   fmt.Sprintf("dangling claim %q: no referenced test, gate, path, or identifier exists in the current tree", guarantee),
		})
		return
	}
	report.AnchorsResolved += anchors
}

// resolveClaimToken resolves one backticked claim reference in anchor order:
// declared Make target, known manifest identifier, existing repository path,
// then Go identifier occurrence. A reference that looks like a repository path
// but does not exist is a hard failure; any other unresolved reference is
// silently unanchored.
func resolveClaimToken(index *artifactIndex, document, docDir string, lineNumber int, reference string) (bool, *Finding) {
	for _, match := range makeReferencePattern.FindAllStringSubmatch(reference, -1) {
		if index.hasMakeTarget(match[1]) {
			return true, nil
		}
	}
	normalized, pathEligible := normalizePathToken(reference)
	if pathEligible {
		if index.hasMakeTarget(normalized) || index.hasManifestIdentifier(normalized) {
			return true, nil
		}
		if strings.ContainsAny(normalized, "/.") && index.resolveClaimPath(docDir, normalized) {
			return true, nil
		}
		if looksLikeRepoPath(normalized) &&
			!index.isPackageSymbolReference(docDir, normalized) &&
			hasDottedSegment(normalized) {
			return false, &Finding{
				Document:  document,
				Line:      lineNumber,
				Reference: reference,
				Message:   fmt.Sprintf("claim references repository path `%s` that does not exist in the current tree", reference),
			}
		}
	}
	if index.hasGoIdentifierPath(strings.TrimSpace(reference)) {
		return true, nil
	}
	return false, nil
}

// countPlainWordAnchors counts identifier-shaped plain words in a claim row
// that name known artifacts: exact manifest identifiers (any shape, since the
// registry is finite and curated), or camel-case/underscore words occurring
// in the module Go sources. Plain lowercase prose never anchors
// through the Go identifier scan.
func countPlainWordAnchors(index *artifactIndex, claimText string) int {
	anchors := 0
	for _, word := range plainWordPattern.FindAllString(claimText, -1) {
		trimmed := strings.Trim(word, ".-")
		if trimmed == "" {
			continue
		}
		if index.hasManifestIdentifier(trimmed) {
			anchors++
			continue
		}
		if isIdentifierShaped(trimmed) && index.hasGoIdentifierWord(trimmed) {
			anchors++
		}
	}
	return anchors
}

// isIdentifierShaped reports whether a plain word is shaped like a Go
// identifier rather than prose: identifier syntax with an uppercase letter or
// underscore.
func isIdentifierShaped(word string) bool {
	if !wholeIdentifierPattern.MatchString(word) {
		return false
	}
	return word != strings.ToLower(word) || strings.Contains(word, "_")
}

// splitTableRow splits a markdown table row into trimmed cell texts.
func splitTableRow(text string) []string {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	cells := strings.Split(trimmed, "|")
	for cellIndex := range cells {
		cells[cellIndex] = strings.TrimSpace(cells[cellIndex])
	}
	return cells
}
