// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestInspectDirectivesInFileRejectsEveryInvalidConfigurationClass(t *testing.T) {
	t.Parallel()

	const source = `//goplint:ignore
package directives

//goplint:enum-cu=#Thing
type Typo struct{}

//goplint:enum-cue
type MissingValue string

//goplint:path-domain=Host/Path
type InvalidValue string

//goplint:constant-only
//goplint:mutable
type Conflicting string

//goplint:nonzero
//goplint:nonzero
type Duplicate string

//goplint:no-delegate
func MisplacedFunction() {}

//goplint:trusted-boundary
type MisplacedType struct {
	//goplint:trusted-boundary
	Value Valid
}

func MisplacedParameter(
	//goplint:internal
	value Valid,
) {}

func MisplacedStatement() {
	//goplint:render
	_ = Valid("")
}

//goplint:nonzero
func (Valid) NotValidate() {}

type Valid string
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "directives.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse directive fixture: %v", err)
	}

	var diagnostics []analysis.Diagnostic
	pass := &analysis.Pass{
		Fset:      fset,
		Files:     []*ast.File{file},
		Pkg:       types.NewPackage("example.com/directives", "directives"),
		TypesInfo: &types.Info{Defs: make(map[*ast.Ident]types.Object)},
		Report: func(diagnostic analysis.Diagnostic) {
			diagnostics = append(diagnostics, diagnostic)
		},
	}

	inspectDirectivesInFile(pass, file)

	wantSubstrings := []string{
		`directive "ignore" is not allowed on file documentation`,
		`unknown directive key "enum-cu" in goplint directive`,
		`directive "enum-cue" requires a non-empty value`,
		`directive "path-domain" has invalid value "Host/Path"`,
		`conflicting goplint directives "constant-only" and "mutable"`,
		`duplicate goplint directive "nonzero"`,
		`directive "no-delegate" is not allowed on function documentation`,
		`directive "trusted-boundary" is not allowed on type documentation`,
		`directive "trusted-boundary" is not allowed on field documentation`,
		`directive "internal" is not allowed on parameter documentation`,
		`directive "render" is not allowed on statement documentation`,
		`directive "nonzero" is only allowed on a method named Validate`,
	}
	if len(diagnostics) != len(wantSubstrings) {
		messages := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			messages = append(messages, diagnostic.Message)
		}
		t.Fatalf("got %d directive diagnostics, want %d: %v", len(diagnostics), len(wantSubstrings), messages)
	}
	for _, want := range wantSubstrings {
		found := false
		for _, diagnostic := range diagnostics {
			if strings.Contains(diagnostic.Message, want) {
				found = true
				break
			}
		}
		if !found {
			messages := make([]string, 0, len(diagnostics))
			for _, diagnostic := range diagnostics {
				messages = append(messages, diagnostic.Message)
			}
			t.Errorf("missing directive diagnostic containing %q; got %v", want, messages)
		}
	}
}

func TestInspectDirectivesInFileAcceptsTypeDeclarationAndLooseMethodDirectives(t *testing.T) {
	t.Parallel()

	const source = `package directives

type (
	//goplint:ignore -- DTO wrapper is intentionally exempt.
	Ignored struct{ Value string }
)

//goplint:ignore -- generated declaration is intentionally exempt.
var generated = "value"

type NonZero string

//goplint:nonzero

// Validate checks NonZero.
func (NonZero) Validate() error { return nil }
`
	pass, file := buildTypedPassFromSource(t, source)
	var diagnostics []analysis.Diagnostic
	pass.Report = func(diagnostic analysis.Diagnostic) {
		diagnostics = append(diagnostics, diagnostic)
	}
	inspectDirectivesInFile(pass, file)
	if len(diagnostics) != 0 {
		messages := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			messages = append(messages, diagnostic.Message)
		}
		t.Fatalf("valid type/declaration/loose method directives produced diagnostics: %v", messages)
	}
}

func TestMethodDirectiveConsumersUseCentralLooseAttachment(t *testing.T) {
	t.Parallel()

	const source = `package directives

type Value string

//goplint:validate-all

// Validate checks Value.
func (Value) Validate() error { return nil }
`
	pass, file := buildTypedPassFromSource(t, source)
	method, ok := file.Decls[1].(*ast.FuncDecl)
	if !ok {
		t.Fatalf("second declaration = %T, want method", file.Decls[1])
	}
	if !hasMethodDirective(pass, method, "validate-all") {
		t.Fatal("central method directive consumer ignored loose validate-all directive")
	}
	if !hasValidateAllDirectiveForType(pass, "Value", nil, nil) {
		t.Fatal("validate-all type consumer ignored directive on Validate method")
	}
}

func TestInspectDirectivesInFileCombinesAndBoundsLooseMethodGroups(t *testing.T) {
	t.Parallel()

	const source = `package directives

type Duplicate string

//goplint:nonzero

//goplint:nonzero
func (Duplicate) Validate() error { return nil }

//goplint:nonzero









type FarAway string
`
	pass, file := buildTypedPassFromSource(t, source)
	var diagnostics []analysis.Diagnostic
	pass.Report = func(diagnostic analysis.Diagnostic) {
		diagnostics = append(diagnostics, diagnostic)
	}
	inspectDirectivesInFile(pass, file)
	if len(diagnostics) != 2 {
		messages := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			messages = append(messages, diagnostic.Message)
		}
		t.Fatalf("directive diagnostics = %d, want duplicate and bounded-loose failures: %v", len(diagnostics), messages)
	}
	wants := []string{
		`duplicate goplint directive "nonzero"`,
		`directive "nonzero" is not allowed on file documentation`,
	}
	for _, want := range wants {
		found := false
		for _, diagnostic := range diagnostics {
			if strings.Contains(diagnostic.Message, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing directive diagnostic containing %q", want)
		}
	}
}

func TestInspectDirectivesInFileKeepsAdjacentStatementDirectivesDistinct(t *testing.T) {
	t.Parallel()

	const source = `package directives

type Value string

func Convert(first, second string) {
	//goplint:ignore -- first boundary value is validated by the caller.
	left := Value(first)
	//goplint:ignore -- second boundary value is validated by the caller.
	right := Value(second)
	_ = left
	_ = right
}
`
	pass, file := buildTypedPassFromSource(t, source)
	var diagnostics []analysis.Diagnostic
	pass.Report = func(diagnostic analysis.Diagnostic) {
		diagnostics = append(diagnostics, diagnostic)
	}
	inspectDirectivesInFile(pass, file)
	if len(diagnostics) != 0 {
		messages := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			messages = append(messages, diagnostic.Message)
		}
		t.Fatalf("adjacent statement directives were merged: %v", messages)
	}
}

func TestFileDirectiveFindingIDsAreSourceLocal(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	left, err := parser.ParseFile(fset, "left.go", "//goplint:typo\npackage directives\n", parser.ParseComments)
	if err != nil {
		t.Fatalf("parse left file: %v", err)
	}
	right, err := parser.ParseFile(fset, "right.go", "//goplint:typo\npackage directives\n", parser.ParseComments)
	if err != nil {
		t.Fatalf("parse right file: %v", err)
	}
	var diagnostics []analysis.Diagnostic
	pass := &analysis.Pass{
		Fset:      fset,
		Files:     []*ast.File{left, right},
		Pkg:       types.NewPackage("example.com/directives", "directives"),
		TypesInfo: &types.Info{Defs: make(map[*ast.Ident]types.Object)},
		Report: func(diagnostic analysis.Diagnostic) {
			diagnostics = append(diagnostics, diagnostic)
		},
	}
	inspectDirectivesInFile(pass, left)
	inspectDirectivesInFile(pass, right)
	if len(diagnostics) != 2 {
		t.Fatalf("file directive diagnostics = %d, want 2", len(diagnostics))
	}
	leftID := FindingIDFromDiagnosticURL(diagnostics[0].URL)
	rightID := FindingIDFromDiagnosticURL(diagnostics[1].URL)
	if leftID == "" || rightID == "" || leftID == rightID {
		t.Fatalf("file directive IDs = %q and %q, want distinct source-local IDs", leftID, rightID)
	}
}

func TestDirectiveConsumersRejectInvalidSets(t *testing.T) {
	t.Parallel()

	commentGroup := func(lines ...string) *ast.CommentGroup {
		comments := make([]*ast.Comment, 0, len(lines))
		for _, line := range lines {
			comments = append(comments, &ast.Comment{Text: line})
		}
		return &ast.CommentGroup{List: comments}
	}

	if value, ok := directiveValue([]*ast.CommentGroup{commentGroup("//goplint:enum-cue=#Runtime")}, "enum-cue"); !ok || value != "#Runtime" {
		t.Fatalf("valid enum-cue directive = %q, %v; want #Runtime, true", value, ok)
	}
	for _, comments := range []*ast.CommentGroup{
		commentGroup("//goplint:enum-cue"),
		commentGroup("//goplint:enum-cue=Runtime"),
		commentGroup("//goplint:enum-cue=#One", "//goplint:enum-cue=#Two"),
	} {
		if value, ok := directiveValue([]*ast.CommentGroup{comments}, "enum-cue"); ok {
			t.Errorf("invalid enum-cue directive was consumed as %q", value)
		}
	}

	conflict := commentGroup("//goplint:constant-only", "//goplint:mutable")
	if hasDirectiveKey(conflict, nil, "constant-only") || hasDirectiveKey(conflict, nil, "mutable") {
		t.Error("conflicting directives must not influence consumers")
	}
}
