// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	// findingIDVersion is part of the canonical ID preimage. Bump only
	// for intentional, incompatible ID schema changes.
	findingIDVersion = "4"

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
	pkgName := ""
	if pass != nil && pass.Pkg != nil {
		pkgPath = pass.Pkg.Path()
		pkgName = pass.Pkg.Name()
	}
	scopedParts := make([]string, 0, len(parts)+1)
	scopedParts = append(scopedParts, pkgPath)
	for _, part := range parts {
		// Human-facing analyzer names commonly begin with the package leaf
		// (for example, "goplint.Type.Method"). The full import path above is
		// the package identity; retaining the leaf as a second identity input
		// makes harmless package-clause renames churn otherwise identical IDs.
		scopedParts = append(scopedParts, strings.TrimPrefix(part, pkgName+"."))
	}
	return StableFindingID(category, scopedParts...)
}

func semanticNodeKey(pass *analysis.Pass, pos token.Pos) string {
	if pass == nil || pass.Fset == nil || !pos.IsValid() {
		return "unknown-semantic-node"
	}
	owner, ownerKey, candidate := semanticFindingNodes(pass, pos)
	if candidate == nil {
		return ownerKey + "|unknown-node"
	}
	return ownerKey + "|" + semanticASTNodeKey(owner, candidate)
}

func semanticASTNodeKey(owner, candidate ast.Node) string {
	if candidate == nil {
		return "unknown-node"
	}
	candidateText := canonicalASTNode(nil, candidate)
	digest := sha256.Sum256([]byte(candidateText))
	ordinal := semanticNodeOrdinal(nil, owner, candidate, candidateText)
	return strings.Join([]string{
		reflect.TypeOf(candidate).String(),
		hex.EncodeToString(digest[:]),
		strconv.Itoa(ordinal),
	}, "|")
}

func semanticFindingNodes(pass *analysis.Pass, pos token.Pos) (ast.Node, string, ast.Node) {
	packagePath := "<unknown-package>"
	if pass.Pkg != nil {
		packagePath = pass.Pkg.Path()
	}
	var owner ast.Node
	ownerKey := packagePath + "|package"
	ownerSpan := int(^uint(0) >> 1)
	var site ast.Node
	siteSpan := ownerSpan
	var statement ast.Node
	statementSpan := ownerSpan
	var fallback ast.Node
	fallbackSpan := ownerSpan
	for _, file := range pass.Files {
		if file == nil || pos < file.Pos() || pos >= file.End() {
			continue
		}
		if owner == nil {
			owner = file
			filename := pass.Fset.Position(file.Pos()).Filename
			ownerKey = packagePath + "|file:" + filepath.Base(filename)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil || pos < node.Pos() || pos >= node.End() {
				return false
			}
			span := int(node.End() - node.Pos())
			switch typed := node.(type) {
			case *ast.FuncDecl:
				if span < ownerSpan {
					owner = typed
					ownerSpan = span
					if object := pass.TypesInfo.Defs[typed.Name]; object != nil {
						ownerKey = semanticObjectKey(object)
					} else {
						ownerKey = packagePath + "|func:" + typed.Name.Name
					}
				}
			case *ast.TypeSpec:
				if ownerSpan == int(^uint(0)>>1) && span < ownerSpan {
					owner = typed
					ownerSpan = span
					if object := pass.TypesInfo.Defs[typed.Name]; object != nil {
						ownerKey = semanticObjectKey(object)
					} else {
						ownerKey = packagePath + "|type:" + typed.Name.Name
					}
				}
			}
			if isSemanticSiteNode(node) && span < siteSpan {
				site = node
				siteSpan = span
			}
			if _, ok := node.(ast.Stmt); ok && span < statementSpan {
				statement = node
				statementSpan = span
			}
			switch node.(type) {
			case ast.Expr, *ast.Field, ast.Spec, ast.Decl:
				if span < fallbackSpan {
					fallback = node
					fallbackSpan = span
				}
			}
			return true
		})
		break
	}
	if site != nil {
		return owner, ownerKey, site
	}
	if statement != nil {
		return owner, ownerKey, statement
	}
	if fallback != nil {
		return owner, ownerKey, fallback
	}
	return owner, ownerKey, owner
}

func isSemanticSiteNode(node ast.Node) bool {
	switch node.(type) {
	case *ast.Ident, *ast.BasicLit, *ast.FuncType:
		return false
	case ast.Expr, *ast.Field, ast.Spec:
		return true
	default:
		return false
	}
}

func semanticObjectKey(object types.Object) string {
	if object == nil {
		return "unknown-object"
	}
	packagePath := ""
	if object.Pkg() != nil {
		packagePath = object.Pkg().Path()
	}
	parts := []string{"object", objectKind(object), packagePath, object.Name()}
	if function, ok := object.(*types.Func); ok {
		if signature, signatureOK := function.Type().(*types.Signature); signatureOK && signature.Recv() != nil {
			parts = append(parts, types.TypeString(signature.Recv().Type(), func(pkg *types.Package) string {
				if pkg == nil {
					return ""
				}
				return pkg.Path()
			}))
		}
	}
	return strings.Join(parts, "|")
}

func semanticNodeOrdinal(pass *analysis.Pass, owner, candidate ast.Node, candidateText string) int {
	if owner == nil || candidate == nil {
		return 0
	}
	wantType := reflect.TypeOf(candidate)
	ordinal := 0
	found := false
	ast.Inspect(owner, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if reflect.TypeOf(node) != wantType || canonicalASTNode(pass, node) != candidateText {
			return true
		}
		if node == candidate {
			found = true
			return false
		}
		ordinal++
		return true
	})
	return ordinal
}

func canonicalASTNode(pass *analysis.Pass, node ast.Node) string {
	if node == nil {
		return "<nil>"
	}
	var buffer bytes.Buffer
	// Deliberately use a fresh empty file set. The AST structure carries the
	// semantic syntax, while the analysis file set also carries original line
	// layout that can make go/format preserve irrelevant one-line/multiline
	// choices. Stable IDs must not inherit that layout.
	if err := format.Node(&buffer, token.NewFileSet(), node); err == nil {
		return buffer.String()
	}
	_ = pass
	return fmt.Sprintf("%T", node)
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
	values := findingMetaValuesFromDiagnosticURL(raw)
	if len(values) == 0 {
		return ""
	}
	return values.Get(key)
}

func findingMetaFromDiagnosticURL(raw string) map[string]string {
	values := findingMetaValuesFromDiagnosticURL(raw)
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if key == "" || len(value) == 0 || value[0] == "" {
			continue
		}
		out[key] = value[0]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func findingMetaValuesFromDiagnosticURL(raw string) url.Values {
	if !strings.HasPrefix(raw, DiagnosticURLPrefix) {
		return nil
	}
	rest := strings.TrimPrefix(raw, DiagnosticURLPrefix)
	_, query, found := strings.Cut(rest, "?")
	if !found || query == "" {
		return nil
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return nil
	}
	return values
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
	if pass == nil || pass.Report == nil {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos:      pos,
		Category: category,
		Message:  message,
		URL:      DiagnosticURLForFindingWithMeta(findingID, meta),
	})
}
