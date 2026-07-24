// SPDX-License-Identifier: MPL-2.0

// Package docsguard statically anchors goplint documentation to executable
// artifacts. Using plain text and JSON parsing only — no Go package loading —
// it verifies that every referenced Make target is declared in the repository
// Makefile, that every referenced repository path exists, and that every
// claim-to-evidence row in the soundness evidence index names at least one
// test, gate, or observation identifier that exists in the current tree.
// Every finding carries the exact document, 1-based line number, and offending
// reference; there is no baseline, exception, or inline-ignore surface.
package docsguard

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// goplintModuleDir is the analyzer module directory relative to the
	// repository root. Documentation frequently uses module-relative paths
	// (for example `spec/soundness-gate.v1.json`), so unresolved repository
	// paths are retried against this directory.
	goplintModuleDir = "tools/goplint"

	// gateManifestRelPath locates the aggregate soundness-gate manifest whose
	// profile and subgate identifiers anchor documentation claims.
	gateManifestRelPath = "tools/goplint/spec/soundness-gate.v1.json"

	// evidenceRegistryRelPath locates the semantic evidence registry whose
	// registration and observation identifiers anchor documentation claims.
	evidenceRegistryRelPath = "tools/goplint/spec/semantic-evidence.v2.json"

	// specDirRelPath is an extra resolution root for slash-less manifest file
	// names cited inside evidence claim rows (for example
	// `semantic-evidence.v2.json`).
	specDirRelPath = "tools/goplint/spec"
)

// DefaultDocuments is the governed goplint documentation set, relative to the
// repository root.
var DefaultDocuments = []string{
	"docs/goplint/evidence-index.md",
	"docs/goplint/soundness-gate-execution.md",
	"docs/goplint/soundness-gate-performance.md",
	"docs/goplint/current-techniques-and-semantics.md",
	"tools/goplint/CLAUDE.md",
	"tools/goplint/AGENTS.md",
	"tools/goplint/README.md",
}

// repoPathFirstSegments are the leading path segments that mark a backticked
// reference as a repository path reference.
var repoPathFirstSegments = map[string]struct{}{
	".agents":  {},
	".github":  {},
	"cmd":      {},
	"docs":     {},
	"internal": {},
	"pkg":      {},
	"scripts":  {},
	"tests":    {},
	"tools":    {},
	"openspec": {},
	"samples":  {},
	"spec":     {},
	"testdata": {},
	"bench":    {},
	"goplint":  {},
}

var (
	makeReferencePattern   = regexp.MustCompile(`\bmake ([a-z][a-z0-9-]*)`)
	makeDeclarationPattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9._-]*):(?:[^=]|$)`)
	inlineCodePattern      = regexp.MustCompile("`([^`]+)`")
	fencedOpeningPattern   = regexp.MustCompile("^\\s*```")
	pathCharsetPattern     = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	packageSymbolPattern   = regexp.MustCompile(`\.[A-Z][A-Za-z0-9_]*$`)
	goIdentifierPattern    = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	wholeIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	plainWordPattern       = regexp.MustCompile(`[A-Za-z][A-Za-z0-9._-]*`)
)

type (
	// Finding is one stale documentation reference. It names the document, the
	// 1-based line, and the exact offending reference or claim row.
	Finding struct {
		Document  string
		Line      int
		Reference string
		Message   string
	}

	// Report summarizes one validation run over a documentation set.
	Report struct {
		DocumentsChecked int
		AnchorsResolved  int
		Findings         []Finding
	}

	// documentLine pairs validated code text with its 1-based source line.
	documentLine struct {
		line int
		text string
	}
)

// String renders the finding as one diagnostic line.
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s", f.Document, f.Line, f.Message)
}

// Validate checks the default goplint documentation set under root, the
// repository root directory.
func Validate(root string) (Report, error) {
	return ValidateDocuments(root, DefaultDocuments)
}

// ValidateDocuments checks the given repository-relative documents under root.
func ValidateDocuments(root string, documents []string) (Report, error) {
	index, err := newArtifactIndex(root)
	if err != nil {
		return Report{}, err
	}
	report := Report{}
	for _, document := range documents {
		data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(document)))
		if readErr != nil {
			return Report{}, fmt.Errorf("read governed document %s: %w", document, readErr)
		}
		lines := strings.Split(string(data), "\n")
		checkDocument(index, document, lines, &report)
	}
	return report, nil
}

// checkDocument validates one document's make targets, repository paths, and
// evidence claim tables, appending findings and anchor counts to the report.
func checkDocument(index *artifactIndex, document string, lines []string, report *Report) {
	report.DocumentsChecked++
	codeLines, inlineTokens := collectCodeRegions(lines)
	checkMakeTargets(index, document, codeLines, report)
	checkRepositoryPaths(index, document, inlineTokens, report)
	checkEvidenceTables(index, document, lines, report)
}

// collectCodeRegions splits a document into fenced code-block lines and inline
// backticked tokens. Make targets are validated in both regions; repository
// path tokens are validated for inline backticked tokens.
func collectCodeRegions(lines []string) (codeLines, inlineTokens []documentLine) {
	fenced := false
	for lineIndex, text := range lines {
		lineNumber := lineIndex + 1
		if fencedOpeningPattern.MatchString(text) {
			fenced = !fenced
			continue
		}
		if fenced {
			codeLines = append(codeLines, documentLine{line: lineNumber, text: text})
			continue
		}
		for _, match := range inlineCodePattern.FindAllStringSubmatch(text, -1) {
			span := documentLine{line: lineNumber, text: match[1]}
			codeLines = append(codeLines, span)
			inlineTokens = append(inlineTokens, span)
		}
	}
	return codeLines, inlineTokens
}

// checkMakeTargets verifies every `make <target>` reference inside code
// regions against the declared repository Makefile targets.
func checkMakeTargets(index *artifactIndex, document string, codeLines []documentLine, report *Report) {
	for _, code := range codeLines {
		for _, match := range makeReferencePattern.FindAllStringSubmatch(code.text, -1) {
			target := match[1]
			if index.hasMakeTarget(target) {
				report.AnchorsResolved++
				continue
			}
			report.Findings = append(report.Findings, Finding{
				Document:  document,
				Line:      code.line,
				Reference: target,
				Message:   fmt.Sprintf("make target `%s` is not declared in the repository Makefile", target),
			})
		}
	}
}

// checkRepositoryPaths verifies inline backticked tokens that look like
// repository paths. Unknown-shaped tokens are skipped, never failed.
func checkRepositoryPaths(index *artifactIndex, document string, inlineTokens []documentLine, report *Report) {
	docDir := path.Dir(document)
	for _, reference := range inlineTokens {
		normalized, ok := normalizePathToken(reference.text)
		if !ok || !looksLikeRepoPath(normalized) {
			continue
		}
		if index.resolveRepoPath(docDir, normalized) {
			report.AnchorsResolved++
			continue
		}
		if index.isPackageSymbolReference(docDir, normalized) || !hasDottedSegment(normalized) {
			continue
		}
		report.Findings = append(report.Findings, Finding{
			Document:  document,
			Line:      reference.line,
			Reference: reference.text,
			Message:   fmt.Sprintf("repository path `%s` does not exist in the current tree", reference.text),
		})
	}
}

// normalizePathToken strips a trailing `#fragment` and trailing slash and
// reports whether the remaining reference is eligible for path interpretation.
func normalizePathToken(reference string) (string, bool) {
	trimmed, _, _ := strings.Cut(reference, "#")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" || strings.Contains(trimmed, "*") {
		return "", false
	}
	if !pathCharsetPattern.MatchString(trimmed) {
		return "", false
	}
	return trimmed, true
}

// looksLikeRepoPath reports whether a normalized reference has a repository path
// shape: it contains a separator and starts with a governed first segment.
func looksLikeRepoPath(normalized string) bool {
	first, rest, found := strings.Cut(normalized, "/")
	if !found || rest == "" {
		return false
	}
	_, ok := repoPathFirstSegments[first]
	return ok
}

// hasDottedSegment reports whether any `/`-separated segment contains a dot.
// Tokens without one (for example `race-repeat-1/6`) are unknown-shaped and
// skipped when they do not resolve to an existing directory.
func hasDottedSegment(normalized string) bool {
	for segment := range strings.SplitSeq(normalized, "/") {
		if strings.Contains(segment, ".") {
			return true
		}
	}
	return false
}
