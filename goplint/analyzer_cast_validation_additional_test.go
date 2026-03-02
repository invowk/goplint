// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestPackageCallFuncName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call *ast.CallExpr
		want string
	}{
		{
			name: "nil call",
			call: nil,
			want: "",
		},
		{
			name: "selector call",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   ast.NewIdent("slices"),
					Sel: ast.NewIdent("Contains"),
				},
			},
			want: "Contains",
		},
		{
			name: "ident call",
			call: &ast.CallExpr{Fun: ast.NewIdent("validate")},
			want: "validate",
		},
		{
			name: "unsupported expression",
			call: &ast.CallExpr{Fun: &ast.FuncLit{}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := packageCallFuncName(tt.call); got != tt.want {
				t.Fatalf("packageCallFuncName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsSlicesComparisonFunc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: "Contains", want: true},
		{name: "ContainsFunc", want: true},
		{name: "Index", want: true},
		{name: "IndexFunc", want: true},
		{name: "Sort", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isSlicesComparisonFunc(tt.name); got != tt.want {
				t.Fatalf("isSlicesComparisonFunc(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestReceiverImplementsError(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type ValErr struct{}
func (ValErr) Error() string { return "" }

type PtrErr struct{}
func (*PtrErr) Error() string { return "" }

type Plain struct{}

func use() {
	var ve ValErr
	var pe PtrErr
	var plain Plain
	_ = ve
	_ = pe
	_ = plain
}`

	pass, file := buildTypedPassFromSource(t, src)
	useFn := findFuncDecl(t, file, "use")
	veIdent := findIdentInFunc(t, useFn, "ve")
	peIdent := findIdentInFunc(t, useFn, "pe")
	plainIdent := findIdentInFunc(t, useFn, "plain")

	t.Run("guards", func(t *testing.T) {
		t.Parallel()

		if receiverImplementsError(nil, veIdent) {
			t.Fatal("receiverImplementsError(nil, expr) = true, want false")
		}
		if receiverImplementsError(pass, nil) {
			t.Fatal("receiverImplementsError(pass, nil) = true, want false")
		}
	})

	t.Run("value and pointer receivers", func(t *testing.T) {
		t.Parallel()

		if !receiverImplementsError(pass, veIdent) {
			t.Fatal("expected value receiver type to implement error")
		}
		if !receiverImplementsError(pass, peIdent) {
			t.Fatal("expected pointer-only receiver type to implement error through *T")
		}
		if receiverImplementsError(pass, plainIdent) {
			t.Fatal("plain type should not implement error")
		}
	})

	t.Run("missing type info", func(t *testing.T) {
		t.Parallel()

		cloned := clonePassTypesInfo(pass)
		delete(cloned.TypesInfo.Types, plainIdent)
		if receiverImplementsError(cloned, plainIdent) {
			t.Fatal("receiverImplementsError without TypeOf info = true, want false")
		}
	})
}

func TestQualifiedTypeName(t *testing.T) {
	t.Parallel()

	currentPkg := types.NewPackage("example.com/current", "current")
	currentObj := types.NewTypeName(token.NoPos, currentPkg, "Config", nil)
	currentNamed := types.NewNamed(currentObj, types.NewStruct(nil, nil), nil)
	if got := qualifiedTypeName(currentNamed, currentPkg); got != "Config" {
		t.Fatalf("qualifiedTypeName(same package) = %q, want %q", got, "Config")
	}

	nilPkgObj := types.NewTypeName(token.NoPos, nil, "Adhoc", nil)
	nilPkgNamed := types.NewNamed(nilPkgObj, types.NewStruct(nil, nil), nil)
	if got := qualifiedTypeName(nilPkgNamed, currentPkg); got != "Adhoc" {
		t.Fatalf("qualifiedTypeName(nil pkg) = %q, want %q", got, "Adhoc")
	}

	externalPkg := types.NewPackage("example.com/remote/subpkg", "subpkg")
	externalObj := types.NewTypeName(token.NoPos, externalPkg, "Value", nil)
	externalNamed := types.NewNamed(externalObj, types.NewStruct(nil, nil), nil)
	if got := qualifiedTypeName(externalNamed, currentPkg); got != "subpkg.Value" {
		t.Fatalf("qualifiedTypeName(external slash path) = %q, want %q", got, "subpkg.Value")
	}

	externalNoSlashPkg := types.NewPackage("externalpkg", "ext")
	externalNoSlashObj := types.NewTypeName(token.NoPos, externalNoSlashPkg, "Name", nil)
	externalNoSlashNamed := types.NewNamed(externalNoSlashObj, types.NewStruct(nil, nil), nil)
	if got := qualifiedTypeName(externalNoSlashNamed, currentPkg); got != "ext.Name" {
		t.Fatalf("qualifiedTypeName(external no slash path) = %q, want %q", got, "ext.Name")
	}

	if got := qualifiedTypeName(types.Typ[types.String], currentPkg); got != "string" {
		t.Fatalf("qualifiedTypeName(non-named) = %q, want %q", got, "string")
	}
}

func TestSelectorIsDirectCallTarget(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Name string
func (Name) Validate() error { return nil }
func use(raw string) {
	_ = Name(raw).Validate
	_ = Name(raw).Validate()
}`

	pass, file := buildTypedPassFromSource(t, src)
	useFn := findFuncDecl(t, file, "use")
	parentMap := buildParentMap(useFn.Body)

	storedCast := findTypeConversionCallBySelectorCallState(t, pass, useFn, parentMap, false)
	calledCast := findTypeConversionCallBySelectorCallState(t, pass, useFn, parentMap, true)

	if isAutoSkipContext(pass, storedCast, parentMap[storedCast], parentMap) {
		t.Fatal("expected stored method value cast to not be auto-skipped")
	}
	if !isAutoSkipContext(pass, calledCast, parentMap[calledCast], parentMap) {
		t.Fatal("expected chained Validate cast to be auto-skipped")
	}
}

func findIdentInFunc(t *testing.T, fn *ast.FuncDecl, name string) *ast.Ident {
	t.Helper()

	var found *ast.Ident
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok || ident.Name != name {
			return true
		}
		found = ident
		return false
	})
	if found == nil {
		t.Fatalf("identifier %q not found", name)
	}
	return found
}

func findTypeConversionCallBySelectorCallState(
	t *testing.T,
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	parentMap map[ast.Node]ast.Node,
	called bool,
) *ast.CallExpr {
	t.Helper()

	var found *ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := parentMap[call].(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Validate" {
			return true
		}
		if selectorIsDirectCallTarget(sel, parentMap) != called {
			return true
		}
		tv, ok := pass.TypesInfo.Types[call.Fun]
		if !ok || !tv.IsType() {
			return true
		}
		found = call
		return false
	})
	if found == nil {
		t.Fatalf("type conversion with called=%v not found", called)
	}
	return found
}
