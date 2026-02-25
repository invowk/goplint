// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestShouldSkipFunc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		funcName string
		want     bool
	}{
		{name: "init", funcName: "init", want: true},
		{name: "main", funcName: "main", want: true},
		{name: "Test prefix", funcName: "TestFoo", want: true},
		{name: "Benchmark prefix", funcName: "BenchmarkFoo", want: true},
		{name: "Fuzz prefix", funcName: "FuzzFoo", want: true},
		{name: "Example prefix", funcName: "ExampleFoo", want: true},
		{name: "normal func", funcName: "doSomething", want: false},
		{name: "Testing (still Test prefix)", funcName: "Testing", want: true},
		{name: "test lowercase", funcName: "testHelper", want: false},
		{name: "Mainfunc (not main)", funcName: "Mainfunc", want: false},
		{name: "Init capitalized (not init)", funcName: "Init", want: false},
		{name: "empty name", funcName: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fn := &ast.FuncDecl{
				Name: ast.NewIdent(tt.funcName),
			}
			got := shouldSkipFunc(fn)
			if got != tt.want {
				t.Errorf("shouldSkipFunc(%q) = %v, want %v", tt.funcName, got, tt.want)
			}
		})
	}
}

func TestIsInterfaceMethodReturn(t *testing.T) {
	t.Parallel()

	// Helpers to build FuncType with specific return types.
	funcTypeWithResults := func(paramCount int, results ...*ast.Field) *ast.FuncType {
		ft := &ast.FuncType{}
		if paramCount > 0 {
			fields := make([]*ast.Field, paramCount)
			for i := range fields {
				fields[i] = &ast.Field{Type: ast.NewIdent("int")}
			}
			ft.Params = &ast.FieldList{List: fields}
		}
		if len(results) > 0 {
			ft.Results = &ast.FieldList{List: results}
		}
		return ft
	}

	// Shorthand for common return type fields.
	stringResult := &ast.Field{Type: ast.NewIdent("string")}
	intResult := &ast.Field{Type: ast.NewIdent("int")}
	errorResult := &ast.Field{Type: ast.NewIdent("error")}
	byteSliceResult := &ast.Field{Type: &ast.ArrayType{Elt: ast.NewIdent("byte")}}

	tests := []struct {
		name    string
		hasRecv bool
		fnName  string
		fnType  *ast.FuncType // nil = no Type field
		want    bool
	}{
		// Correct signatures — should be recognized as interface methods.
		{name: "String() string", hasRecv: true, fnName: "String", fnType: funcTypeWithResults(0, stringResult), want: true},
		{name: "Error() string", hasRecv: true, fnName: "Error", fnType: funcTypeWithResults(0, stringResult), want: true},
		{name: "GoString() string", hasRecv: true, fnName: "GoString", fnType: funcTypeWithResults(0, stringResult), want: true},
		{name: "MarshalText() ([]byte, error)", hasRecv: true, fnName: "MarshalText", fnType: funcTypeWithResults(0, byteSliceResult, errorResult), want: true},
		{name: "MarshalBinary() ([]byte, error)", hasRecv: true, fnName: "MarshalBinary", fnType: funcTypeWithResults(0, byteSliceResult, errorResult), want: true},
		{name: "MarshalJSON() ([]byte, error)", hasRecv: true, fnName: "MarshalJSON", fnType: funcTypeWithResults(0, byteSliceResult, errorResult), want: true},

		// Wrong return types — name matches, count matches, but type is wrong.
		{name: "String() int — wrong return type", hasRecv: true, fnName: "String", fnType: funcTypeWithResults(0, intResult), want: false},
		{name: "Error() int — wrong return type", hasRecv: true, fnName: "Error", fnType: funcTypeWithResults(0, intResult), want: false},
		{name: "GoString() int — wrong return type", hasRecv: true, fnName: "GoString", fnType: funcTypeWithResults(0, intResult), want: false},
		{name: "MarshalText() (string, error) — wrong first result", hasRecv: true, fnName: "MarshalText", fnType: funcTypeWithResults(0, stringResult, errorResult), want: false},
		{name: "MarshalText() ([]byte, string) — wrong second result", hasRecv: true, fnName: "MarshalText", fnType: funcTypeWithResults(0, byteSliceResult, stringResult), want: false},
		{name: "MarshalBinary() (string, error) — wrong first result", hasRecv: true, fnName: "MarshalBinary", fnType: funcTypeWithResults(0, stringResult, errorResult), want: false},
		{name: "MarshalJSON() ([]byte, string) — wrong second result", hasRecv: true, fnName: "MarshalJSON", fnType: funcTypeWithResults(0, byteSliceResult, stringResult), want: false},

		// Wrong signatures — name matches but param/result count is wrong.
		{name: "String(x int) string — has param", hasRecv: true, fnName: "String", fnType: funcTypeWithResults(1, stringResult), want: false},
		{name: "Error() (string, error) — wrong result count", hasRecv: true, fnName: "Error", fnType: funcTypeWithResults(0, stringResult, errorResult), want: false},
		{name: "MarshalText() []byte — wrong result count", hasRecv: true, fnName: "MarshalText", fnType: funcTypeWithResults(0, byteSliceResult), want: false},

		// Not a method (no receiver).
		{name: "String without receiver", hasRecv: false, fnName: "String", fnType: funcTypeWithResults(0, stringResult), want: false},
		{name: "Error without receiver", hasRecv: false, fnName: "Error", fnType: funcTypeWithResults(0, stringResult), want: false},

		// Other names.
		{name: "other method", hasRecv: true, fnName: "GetName", fnType: funcTypeWithResults(0, stringResult), want: false},
		{name: "regular func", hasRecv: false, fnName: "doSomething", fnType: funcTypeWithResults(0), want: false},

		// Nil Type field — should not panic.
		{name: "nil Type field", hasRecv: true, fnName: "String", fnType: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fn := &ast.FuncDecl{
				Name: ast.NewIdent(tt.fnName),
				Type: tt.fnType,
			}
			if tt.hasRecv {
				fn.Recv = &ast.FieldList{
					List: []*ast.Field{{
						Type: ast.NewIdent("T"),
					}},
				}
			}
			got := isInterfaceMethodReturn(fn)
			if got != tt.want {
				t.Errorf("isInterfaceMethodReturn(recv=%v, %q) = %v, want %v",
					tt.hasRecv, tt.fnName, got, tt.want)
			}
		})
	}
}

func TestParseDirectiveKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		text        string
		wantKeys    []string
		wantUnknown []string
	}{
		{
			name:     "plint:ignore standalone",
			text:     "//plint:ignore",
			wantKeys: []string{"ignore"},
		},
		{
			name:     "plint:internal standalone",
			text:     "//plint:internal",
			wantKeys: []string{"internal"},
		},
		{
			name:     "combined ignore,internal",
			text:     "//plint:ignore,internal",
			wantKeys: []string{"ignore", "internal"},
		},
		{
			name:     "combined internal,ignore (order preserved)",
			text:     "//plint:internal,ignore",
			wantKeys: []string{"internal", "ignore"},
		},
		{
			name:     "combined with reason suffix",
			text:     "//plint:ignore,internal -- computed cache",
			wantKeys: []string{"ignore", "internal"},
		},
		{
			name:     "goplint prefix combined",
			text:     "//goplint:ignore,internal",
			wantKeys: []string{"ignore", "internal"},
		},
		{
			name:     "whitespace around comma trimmed",
			text:     "//plint:ignore, internal",
			wantKeys: []string{"ignore", "internal"},
		},
		{
			name:        "known and unknown keys mixed",
			text:        "//plint:ignore,foo",
			wantKeys:    []string{"ignore"},
			wantUnknown: []string{"foo"},
		},
		{
			name:        "all unknown keys",
			text:        "//plint:foo,bar",
			wantUnknown: []string{"foo", "bar"},
		},
		{
			name:     "nolint:goplint special case",
			text:     "//nolint:goplint",
			wantKeys: []string{"ignore"},
		},
		{
			name: "regular comment, no directive prefix",
			text: "// regular comment",
		},
		{
			name: "empty string",
			text: "",
		},
		{
			name:     "plint:ignore with space after //",
			text:     "// plint:ignore",
			wantKeys: []string{"ignore"},
		},
		{
			name:     "goplint:ignore with reason",
			text:     "//goplint:ignore -- display label",
			wantKeys: []string{"ignore"},
		},
		{
			name:        "trailing comma produces empty token (ignored)",
			text:        "//plint:ignore,",
			wantKeys:    []string{"ignore"},
			wantUnknown: nil,
		},
		{
			name:     "plint:render standalone",
			text:     "//plint:render",
			wantKeys: []string{"render"},
		},
		{
			name:     "combined render,internal",
			text:     "//plint:render,internal",
			wantKeys: []string{"render", "internal"},
		},
		{
			name:     "combined ignore,render",
			text:     "//plint:ignore,render",
			wantKeys: []string{"ignore", "render"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotKeys, gotUnknown := parseDirectiveKeys(tt.text)
			if !slices.Equal(gotKeys, tt.wantKeys) {
				t.Errorf("parseDirectiveKeys(%q) keys = %v, want %v", tt.text, gotKeys, tt.wantKeys)
			}
			if !slices.Equal(gotUnknown, tt.wantUnknown) {
				t.Errorf("parseDirectiveKeys(%q) unknown = %v, want %v", tt.text, gotUnknown, tt.wantUnknown)
			}
		})
	}
}

func TestHasIgnoreDirective(t *testing.T) {
	t.Parallel()

	makeCommentGroup := func(text string) *ast.CommentGroup {
		if text == "" {
			return nil
		}
		return &ast.CommentGroup{
			List: []*ast.Comment{{Text: text}},
		}
	}

	tests := []struct {
		name        string
		doc         string
		lineComment string
		want        bool
	}{
		{
			name:        "goplint:ignore in line comment",
			lineComment: "//goplint:ignore",
			want:        true,
		},
		{
			name:        "nolint:goplint in line comment",
			lineComment: "//nolint:goplint",
			want:        true,
		},
		{
			name: "goplint:ignore in doc comment",
			doc:  "//goplint:ignore -- reason",
			want: true,
		},
		{
			name:        "regular comment, no directive",
			lineComment: "// regular comment",
			want:        false,
		},
		{
			name: "both nil",
			want: false,
		},
		{
			name:        "directive with extra text",
			lineComment: "//goplint:ignore -- display label",
			want:        true,
		},
		{
			name:        "directive in doc, regular in line",
			doc:         "//goplint:ignore",
			lineComment: "// something else",
			want:        true,
		},
		{
			name:        "plint:ignore short-form alias",
			lineComment: "//plint:ignore -- short form",
			want:        true,
		},
		{
			name: "plint:ignore in doc comment",
			doc:  "//plint:ignore",
			want: true,
		},
		{
			name:        "combined ignore,internal matches ignore",
			lineComment: "//plint:ignore,internal",
			want:        true,
		},
		{
			name:        "combined internal,ignore matches ignore",
			lineComment: "//plint:internal,ignore",
			want:        true,
		},
		{
			name:        "goplint combined matches ignore",
			lineComment: "//goplint:ignore,internal",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := makeCommentGroup(tt.doc)
			lineComment := makeCommentGroup(tt.lineComment)
			got := hasIgnoreDirective(doc, lineComment)
			if got != tt.want {
				t.Errorf("hasIgnoreDirective(doc=%q, line=%q) = %v, want %v",
					tt.doc, tt.lineComment, got, tt.want)
			}
		})
	}
}

func TestHasInternalDirective(t *testing.T) {
	t.Parallel()

	makeCommentGroup := func(text string) *ast.CommentGroup {
		if text == "" {
			return nil
		}
		return &ast.CommentGroup{
			List: []*ast.Comment{{Text: text}},
		}
	}

	tests := []struct {
		name        string
		doc         string
		lineComment string
		want        bool
	}{
		{
			name:        "plint:internal in line comment",
			lineComment: "//plint:internal",
			want:        true,
		},
		{
			name: "plint:internal in doc comment",
			doc:  "//plint:internal -- computed cache",
			want: true,
		},
		{
			name:        "plint:ignore does not match internal",
			lineComment: "//plint:ignore",
			want:        false,
		},
		{
			name:        "regular comment",
			lineComment: "// some comment",
			want:        false,
		},
		{
			name: "both nil",
			want: false,
		},
		{
			name:        "combined ignore,internal matches internal",
			lineComment: "//plint:ignore,internal",
			want:        true,
		},
		{
			name:        "combined internal,ignore matches internal",
			lineComment: "//plint:internal,ignore",
			want:        true,
		},
		{
			name:        "combined with reason matches internal",
			lineComment: "//plint:ignore,internal -- cache",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := makeCommentGroup(tt.doc)
			lineComment := makeCommentGroup(tt.lineComment)
			got := hasInternalDirective(doc, lineComment)
			if got != tt.want {
				t.Errorf("hasInternalDirective(doc=%q, line=%q) = %v, want %v",
					tt.doc, tt.lineComment, got, tt.want)
			}
		})
	}
}

func TestHasRenderDirective(t *testing.T) {
	t.Parallel()

	makeCommentGroup := func(text string) *ast.CommentGroup {
		if text == "" {
			return nil
		}
		return &ast.CommentGroup{
			List: []*ast.Comment{{Text: text}},
		}
	}

	tests := []struct {
		name        string
		doc         string
		lineComment string
		want        bool
	}{
		{
			name: "plint:render in doc comment",
			doc:  "//plint:render",
			want: true,
		},
		{
			name:        "plint:render in line comment",
			lineComment: "//plint:render -- display text",
			want:        true,
		},
		{
			name:        "combined render,internal",
			lineComment: "//plint:render,internal",
			want:        true,
		},
		{
			name:        "combined ignore,render",
			lineComment: "//plint:ignore,render",
			want:        true,
		},
		{
			name:        "plint:ignore does not match render",
			lineComment: "//plint:ignore",
			want:        false,
		},
		{
			name:        "plint:internal does not match render",
			lineComment: "//plint:internal",
			want:        false,
		},
		{
			name:        "regular comment",
			lineComment: "// render this text",
			want:        false,
		},
		{
			name: "both nil",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := makeCommentGroup(tt.doc)
			lineComment := makeCommentGroup(tt.lineComment)
			got := hasRenderDirective(doc, lineComment)
			if got != tt.want {
				t.Errorf("hasRenderDirective(doc=%q, line=%q) = %v, want %v",
					tt.doc, tt.lineComment, got, tt.want)
			}
		})
	}
}

func TestReceiverTypeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{
			name: "simple ident",
			expr: &ast.Ident{Name: "MyType"},
			want: "MyType",
		},
		{
			name: "pointer receiver",
			expr: &ast.StarExpr{X: &ast.Ident{Name: "MyType"}},
			want: "MyType",
		},
		{
			name: "generic single param T[P]",
			expr: &ast.IndexExpr{X: &ast.Ident{Name: "Container"}},
			want: "Container",
		},
		{
			name: "generic multi param T[P1, P2]",
			expr: &ast.IndexListExpr{X: &ast.Ident{Name: "Pair"}},
			want: "Pair",
		},
		{
			name: "star with non-ident inner",
			expr: &ast.StarExpr{X: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "pkg"},
				Sel: &ast.Ident{Name: "Type"},
			}},
			want: "",
		},
		{
			name: "index with non-ident inner",
			expr: &ast.IndexExpr{X: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "pkg"},
				Sel: &ast.Ident{Name: "Type"},
			}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := receiverTypeName(tt.expr)
			if got != tt.want {
				t.Errorf("receiverTypeName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPackageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pkg  *types.Package
		want string
	}{
		{
			name: "multi-segment path",
			pkg:  types.NewPackage("github.com/foo/bar", "bar"),
			want: "bar",
		},
		{
			name: "single segment",
			pkg:  types.NewPackage("mypackage", "mypackage"),
			want: "mypackage",
		},
		{
			name: "nil package",
			pkg:  nil,
			want: "",
		},
		{
			name: "deep nesting",
			pkg:  types.NewPackage("github.com/org/repo/internal/deep/pkg", "pkg"),
			want: "pkg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := packageName(tt.pkg)
			if got != tt.want {
				t.Errorf("packageName(%v) = %q, want %q", tt.pkg, got, tt.want)
			}
		})
	}
}

// TestIsTestFile verifies _test.go detection by calling the real
// isTestFile function with a minimal analysis.Pass (only Fset populated).
func TestIsTestFile(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()

	testFile := fset.AddFile("foo_test.go", -1, 100)
	testPos := testFile.Pos(0)

	regularFile := fset.AddFile("foo.go", -1, 100)
	regularPos := regularFile.Pos(0)

	// Minimal-named test file — exactly 8 chars ("_test.go").
	minimalTestFile := fset.AddFile("_test.go", -1, 100)
	minimalTestPos := minimalTestFile.Pos(0)

	// analysis.Pass is a struct, so we can construct a partial instance
	// with only the Fset field populated — isTestFile only uses pass.Fset.
	pass := &analysis.Pass{Fset: fset}

	if !isTestFile(pass, testPos) {
		t.Error("expected foo_test.go to be detected as test file")
	}
	if isTestFile(pass, regularPos) {
		t.Error("expected foo.go to NOT be detected as test file")
	}
	if !isTestFile(pass, minimalTestPos) {
		t.Error("expected _test.go (minimal name) to be detected as test file")
	}
}
