// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"crypto/sha256"
	"encoding/hex"
	"go/token"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
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

// PackageScopedFindingID derives a deterministic ID for a finding that must be
// unique across packages with the same leaf name. This keeps exception keys and
// human messages package-leaf-friendly while the ID preimage remains package-path
// precise.
func PackageScopedFindingID(pass *analysis.Pass, category string, parts ...string) string {
	pkgPath := ""
	if pass != nil && pass.Pkg != nil {
		pkgPath = pass.Pkg.Path()
	}
	scopedParts := make([]string, 0, len(parts)+1)
	scopedParts = append(scopedParts, pkgPath)
	scopedParts = append(scopedParts, parts...)
	return StableFindingID(category, scopedParts...)
}

// FallbackFindingID derives an ID from category + message for diagnostics
// that do not carry an explicit finding URL (legacy compatibility path).
func FallbackFindingID(category, message string) string {
	return StableFindingID(category, "legacy-message", message)
}

// FallbackFindingIDForDiagnostic derives an ID from category + diagnostic
// position + message for analysis outputs that omit explicit finding URLs.
//
// The position component keeps repeated same-message diagnostics distinct
// (for example, multiple discarded Validate() calls in one function).
func FallbackFindingIDForDiagnostic(category, posn, message string) string {
	if posn == "" {
		return FallbackFindingID(category, message)
	}
	return StableFindingID(category, "legacy-diagnostic", posn, message)
}

func stablePosKey(pass *analysis.Pass, pos token.Pos) string {
	if pass == nil || pass.Fset == nil || !pos.IsValid() {
		return "unknown-pos"
	}
	position := pass.Fset.Position(pos)
	if position.Filename == "" {
		return "unknown-pos"
	}
	return filepath.Base(position.Filename) + ":" +
		strconv.Itoa(position.Line) + ":" +
		strconv.Itoa(position.Column)
}

// DiagnosticURLForFinding formats a finding ID for analysis.Diagnostic.URL.
func DiagnosticURLForFinding(id string) string {
	if id == "" {
		return ""
	}
	return DiagnosticURLPrefix + id
}

// DiagnosticURLForFindingWithMeta formats a finding ID URL and appends encoded
// metadata query parameters when provided. The metadata path is used for
// machine-parsable diagnostic details that should not be extracted from human
// message text (for example, stale-exception pattern values).
func DiagnosticURLForFindingWithMeta(id string, meta map[string]string) string {
	base := DiagnosticURLForFinding(id)
	if base == "" || len(meta) == 0 {
		return base
	}

	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	values := url.Values{}
	for _, key := range keys {
		value := meta[key]
		if value == "" {
			continue
		}
		values.Set(key, value)
	}
	encoded := values.Encode()
	if encoded == "" {
		return base
	}
	return base + "?" + encoded
}

// FindingIDFromDiagnosticURL extracts a finding ID from analysis JSON URL
// values. Returns empty string when the URL is not a goplint finding URL.
func FindingIDFromDiagnosticURL(raw string) string {
	if !strings.HasPrefix(raw, DiagnosticURLPrefix) {
		return ""
	}
	rest := strings.TrimPrefix(raw, DiagnosticURLPrefix)
	id, _, _ := strings.Cut(rest, "?")
	return id
}

// FindingMetaFromDiagnosticURL extracts one metadata value from a goplint
// finding URL query string. Returns empty string when not present.
func FindingMetaFromDiagnosticURL(raw, key string) string {
	if key == "" || !strings.HasPrefix(raw, DiagnosticURLPrefix) {
		return ""
	}
	rest := strings.TrimPrefix(raw, DiagnosticURLPrefix)
	_, query, found := strings.Cut(rest, "?")
	if !found || query == "" {
		return ""
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return ""
	}
	return values.Get(key)
}

// reportDiagnostic emits a finding with category, message, and stable ID URL.
func reportDiagnostic(pass *analysis.Pass, pos token.Pos, category, findingID, message string) {
	reportDiagnosticWithMeta(pass, pos, category, findingID, message, nil)
}

// reportDiagnosticWithMeta emits a finding with category, message, stable ID
// URL, and optional machine-readable metadata query fields.
func reportDiagnosticWithMeta(
	pass *analysis.Pass,
	pos token.Pos,
	category, findingID, message string,
	meta map[string]string,
) {
	writeFindingToSink(pass, pos, category, findingID, message)
	pass.Report(analysis.Diagnostic{
		Pos:      pos,
		Category: category,
		Message:  message,
		URL:      DiagnosticURLForFindingWithMeta(findingID, meta),
	})
}
