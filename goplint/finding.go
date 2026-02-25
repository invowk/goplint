// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"crypto/sha256"
	"encoding/hex"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	// findingIDVersion is part of the canonical ID preimage. Bump only
	// for intentional, incompatible ID schema changes.
	findingIDVersion = "1"

	// DiagnosticURLPrefix is the prefix used in analysis.Diagnostic.URL to
	// encode stable finding IDs in -json output.
	DiagnosticURLPrefix = "goplint://finding/"
)

// StableFindingID returns a deterministic ID for a semantic finding identity.
// The ID is derived from category + semantic parts, not human message text.
func StableFindingID(category string, parts ...string) string {
	preimageParts := make([]string, 0, 2+len(parts))
	preimageParts = append(preimageParts, findingIDVersion, category)
	preimageParts = append(preimageParts, parts...)
	preimage := strings.Join(preimageParts, "\x1f")

	sum := sha256.Sum256([]byte(preimage))
	return "gpl" + findingIDVersion + "_" + hex.EncodeToString(sum[:])
}

// FallbackFindingID derives an ID from category + message for diagnostics
// that do not carry an explicit finding URL (legacy compatibility path).
func FallbackFindingID(category, message string) string {
	return StableFindingID(category, "legacy-message", message)
}

// DiagnosticURLForFinding formats a finding ID for analysis.Diagnostic.URL.
func DiagnosticURLForFinding(id string) string {
	if id == "" {
		return ""
	}
	return DiagnosticURLPrefix + id
}

// FindingIDFromDiagnosticURL extracts a finding ID from analysis JSON URL
// values. Returns empty string when the URL is not a goplint finding URL.
func FindingIDFromDiagnosticURL(raw string) string {
	if !strings.HasPrefix(raw, DiagnosticURLPrefix) {
		return ""
	}
	return strings.TrimPrefix(raw, DiagnosticURLPrefix)
}

// reportDiagnostic emits a finding with category, message, and stable ID URL.
func reportDiagnostic(pass *analysis.Pass, pos token.Pos, category, findingID, message string) {
	pass.Report(analysis.Diagnostic{
		Pos:      pos,
		Category: category,
		Message:  message,
		URL:      DiagnosticURLForFinding(findingID),
	})
}
