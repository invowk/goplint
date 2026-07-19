// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestStableFindingID(t *testing.T) {
	t.Parallel()

	id1 := StableFindingID(CategoryPrimitive, "struct-field", "pkg.Type.Field", "string")
	id2 := StableFindingID(CategoryPrimitive, "struct-field", "pkg.Type.Field", "string")
	if id1 == "" {
		t.Fatal("StableFindingID returned empty ID")
	}
	if id1 != id2 {
		t.Fatalf("StableFindingID not deterministic: %q != %q", id1, id2)
	}

	id3 := StableFindingID(CategoryPrimitive, "struct-field", "pkg.Type.Other", "string")
	if id1 == id3 {
		t.Fatalf("expected different semantic inputs to produce different IDs: %q", id1)
	}
}

func TestDiagnosticURLRoundTrip(t *testing.T) {
	t.Parallel()

	id := StableFindingID(CategoryMissingConstructor, "pkg.Config", "NewConfig")
	url := DiagnosticURLForFinding(id)
	got := FindingIDFromDiagnosticURL(url)
	if got != id {
		t.Fatalf("FindingIDFromDiagnosticURL(%q) = %q, want %q", url, got, id)
	}

	if other := FindingIDFromDiagnosticURL("https://example.com/not-goplint"); other != "" {
		t.Fatalf("expected non-goplint URL to return empty ID, got %q", other)
	}
}

func TestDiagnosticURLWithMeta(t *testing.T) {
	t.Parallel()

	id := StableFindingID(CategoryStaleException, "pkg.Type.Field")
	url := DiagnosticURLForFindingWithMeta(id, map[string]string{
		"pattern": "pkg.Type.Field",
		"reason":  "legacy",
	})
	if got := FindingIDFromDiagnosticURL(url); got != id {
		t.Fatalf("FindingIDFromDiagnosticURL(%q) = %q, want %q", url, got, id)
	}
	if got := FindingMetaFromDiagnosticURL(url, "pattern"); got != "pkg.Type.Field" {
		t.Fatalf("FindingMetaFromDiagnosticURL(..., pattern) = %q, want %q", got, "pkg.Type.Field")
	}
}

func TestSemanticNodeKeyIgnoresUnrelatedSourceLayoutDrift(t *testing.T) {
	t.Parallel()

	const probeSource = `package probe
type Value string
func Probe(raw string) { value := Value(raw); _ = value }
`
	const unrelatedSource = `package probe
const unrelated = 1
`
	anchor := semanticProbeCastKeyFromFiles(t, []semanticProbeFile{
		{name: "unrelated.go", source: unrelatedSource},
		{name: "probe.go", source: probeSource},
	})

	variants := map[string][]semanticProbeFile{
		"unrelated file insertion": {
			{name: "inserted.go", source: "package probe\nconst inserted = 2\n"},
			{name: "unrelated.go", source: unrelatedSource},
			{name: "probe.go", source: probeSource},
		},
		"unrelated file deletion": {
			{name: "probe.go", source: probeSource},
		},
		"unrelated file reorder": {
			{name: "probe.go", source: probeSource},
			{name: "unrelated.go", source: unrelatedSource},
		},
		"formatting": {
			{name: "unrelated.go", source: unrelatedSource},
			{name: "probe.go", source: `package probe

type Value string

func Probe(raw string) {
	value := Value(raw)
	_ = value
}
`},
		},
		"preceding declaration length": {
			{name: "unrelated.go", source: unrelatedSource},
			{name: "probe.go", source: `package probe

const unrelatedDeclarationWithDifferentLength = "layout drift"

type Value string

func Probe(raw string) {
	value := Value(raw)
	_ = value
}
	`},
		},
	}

	for name, files := range variants {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := semanticProbeCastKeyFromFiles(t, files)
			if got != anchor {
				t.Fatalf("semantic node key changed under %s:\nanchor=%q\nvariant=%q", name, anchor, got)
			}
		})
	}
}

func TestPackageScopedFindingIDSeparatesEqualPackageLeafNames(t *testing.T) {
	t.Parallel()

	left := &analysis.Pass{Pkg: types.NewPackage("example.com/left/shared", "shared")}
	right := &analysis.Pass{Pkg: types.NewPackage("example.com/right/shared", "shared")}
	leftID := PackageScopedFindingID(left, CategoryPrimitive, "shared.Value.Name", "string")
	rightID := PackageScopedFindingID(right, CategoryPrimitive, "shared.Value.Name", "string")
	if leftID == rightID {
		t.Fatalf("equal package leaf names collided: %q", leftID)
	}

	baseline := &BaselineConfig{
		Primitive: BaselineCategory{Entries: []BaselineFinding{{
			ID:      leftID,
			Message: "struct field shared.Value.Name uses primitive type string",
		}}},
	}
	baseline.buildLookup()
	if !baseline.ContainsFinding(CategoryPrimitive, leftID, "same human message") {
		t.Fatal("baseline did not match the exact full-import-path finding ID")
	}
	if baseline.ContainsFinding(CategoryPrimitive, rightID, "same human message") {
		t.Fatal("baseline entry for left import path suppressed equal-looking right import path finding")
	}

	renamed := &analysis.Pass{Pkg: types.NewPackage("example.com/left/shared", "renamed")}
	renamedID := PackageScopedFindingID(renamed, CategoryPrimitive, "renamed.Value.Name", "string")
	if renamedID != leftID {
		t.Fatalf("package leaf rename changed full-import-path finding ID: %q != %q", renamedID, leftID)
	}
}

func TestSemanticNodeKeySeparatesSitesWithinOneStatement(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func Probe(raw string) {
	left, right := Value(raw), Value(raw)
	_ = []func(){func() { _ = left }, func() { _ = right }}
}
`
	pass, file := semanticProbePass(t, []semanticProbeFile{{name: "probe.go", source: source}})
	var calls []*ast.CallExpr
	var literals []*ast.FuncLit
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CallExpr:
			calls = append(calls, typed)
		case *ast.FuncLit:
			literals = append(literals, typed)
		}
		return true
	})
	if len(calls) != 2 || len(literals) != 2 {
		t.Fatalf("fixture calls/literals = %d/%d, want 2/2", len(calls), len(literals))
	}
	if left, right := semanticNodeKey(pass, calls[0].Pos()), semanticNodeKey(pass, calls[1].Pos()); left == right {
		t.Fatalf("same-statement call sites collided: %q", left)
	}
	if left, right := semanticNodeKey(pass, literals[0].Pos()), semanticNodeKey(pass, literals[1].Pos()); left == right {
		t.Fatalf("same-statement function literals collided: %q", left)
	}
}

type semanticProbeFile struct {
	name   string
	source string
}

func semanticProbeCastKeyFromFiles(t testing.TB, sources []semanticProbeFile) string {
	t.Helper()
	pass, _ := semanticProbePass(t, sources)

	var probe *ast.FuncDecl
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			candidate, ok := decl.(*ast.FuncDecl)
			if ok && candidate.Name.Name == "Probe" {
				probe = candidate
				break
			}
		}
		if probe != nil {
			break
		}
	}
	if probe == nil {
		t.Fatal("function Probe not found")
		return ""
	}
	assignment, ok := probe.Body.List[0].(*ast.AssignStmt)
	if !ok || len(assignment.Rhs) != 1 {
		t.Fatalf("Probe first statement = %T, want one-RHS assignment", probe.Body.List[0])
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	if !ok {
		t.Fatalf("Probe assignment RHS = %T, want call", assignment.Rhs[0])
	}
	return semanticNodeKey(pass, call.Pos())
}

func semanticProbePass(t testing.TB, sources []semanticProbeFile) (*analysis.Pass, *ast.File) {
	t.Helper()

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(sources))
	for _, source := range sources {
		file, err := parser.ParseFile(fset, source.name, source.source, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", source.name, err)
		}
		files = append(files, file)
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Instances:  make(map[*ast.Ident]types.Instance),
	}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check(
		"example.com/stable/probe",
		fset,
		files,
		info,
	)
	if err != nil {
		t.Fatalf("type-check semantic probe: %v", err)
	}
	pass := &analysis.Pass{Fset: fset, Files: files, Pkg: pkg, TypesInfo: info}
	return pass, files[0]
}

func TestDiagnosticURLForFinding_EmptyID(t *testing.T) {
	t.Parallel()

	if got := DiagnosticURLForFinding(""); got != "" {
		t.Fatalf("DiagnosticURLForFinding(empty) = %q, want empty", got)
	}
}

func TestFindingMetaFromDiagnosticURL(t *testing.T) {
	t.Parallel()

	id := StableFindingID(CategoryStaleException, "pkg.Type.Field")
	url := DiagnosticURLForFindingWithMeta(id, map[string]string{
		"pattern": "pkg.Type.Field",
		"reason":  "legacy",
	})

	tests := []struct {
		name string
		raw  string
		key  string
		want string
	}{
		{
			name: "extracts existing key",
			raw:  url,
			key:  "pattern",
			want: "pkg.Type.Field",
		},
		{
			name: "missing key returns empty",
			raw:  url,
			key:  "missing",
			want: "",
		},
		{
			name: "empty key returns empty",
			raw:  url,
			key:  "",
			want: "",
		},
		{
			name: "non goplint url returns empty",
			raw:  "https://example.com?id=1",
			key:  "pattern",
			want: "",
		},
		{
			name: "no query returns empty",
			raw:  DiagnosticURLForFinding(id),
			key:  "pattern",
			want: "",
		},
		{
			name: "invalid query returns empty",
			raw:  DiagnosticURLForFinding(id) + "?pattern=%zz",
			key:  "pattern",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FindingMetaFromDiagnosticURL(tt.raw, tt.key); got != tt.want {
				t.Fatalf("FindingMetaFromDiagnosticURL(%q, %q) = %q, want %q", tt.raw, tt.key, got, tt.want)
			}
		})
	}
}
