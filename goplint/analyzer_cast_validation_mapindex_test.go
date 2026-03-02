// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestIsMapIndexExpr(t *testing.T) {
	t.Parallel()

	src := `package testpkg
func f() {
	m := map[string]int{}
	s := []int{1, 2, 3}
	_ = m["k"]
	_ = s[0]
}`
	pass, file := buildTypedPassFromSource(t, src)
	fn := findFuncDecl(t, file, "f")
	mapIndex := findIndexExprByBase(t, fn, "m")
	sliceIndex := findIndexExprByBase(t, fn, "s")

	if !isMapIndexExpr(pass, mapIndex) {
		t.Fatal("isMapIndexExpr(map index) = false, want true")
	}
	if isMapIndexExpr(pass, sliceIndex) {
		t.Fatal("isMapIndexExpr(slice index) = true, want false")
	}
	if isMapIndexExpr(nil, mapIndex) {
		t.Fatal("isMapIndexExpr(nil pass, idx) = true, want false")
	}
	if isMapIndexExpr(&analysis.Pass{}, mapIndex) {
		t.Fatal("isMapIndexExpr(pass without TypesInfo, idx) = true, want false")
	}
	if isMapIndexExpr(pass, nil) {
		t.Fatal("isMapIndexExpr(pass, nil idx) = true, want false")
	}

	missingTypeIdx := &ast.IndexExpr{X: ast.NewIdent("missing"), Index: ast.NewIdent("k")}
	if isMapIndexExpr(pass, missingTypeIdx) {
		t.Fatal("isMapIndexExpr(missing type info) = true, want false")
	}
}

func findIndexExprByBase(t *testing.T, fn *ast.FuncDecl, base string) *ast.IndexExpr {
	t.Helper()

	var found *ast.IndexExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		ident, ok := stripParens(idx.X).(*ast.Ident)
		if !ok || ident.Name != base {
			return true
		}
		found = idx
		return false
	})
	if found == nil {
		t.Fatalf("index expression for base %q not found", base)
	}
	return found
}
