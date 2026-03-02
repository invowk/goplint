// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestIsErrorReturnBlankedValueSpecOneToOne(t *testing.T) {
	t.Parallel()

	call := &ast.CallExpr{Fun: ast.NewIdent("NewThing")}
	valueSpec := &ast.ValueSpec{
		Names:  []*ast.Ident{ast.NewIdent("_")},
		Values: []ast.Expr{call},
	}
	if !isErrorReturnBlankedValueSpec(valueSpec, call) {
		t.Fatal("expected one-to-one blank var assignment to be detected")
	}
}

func TestIsErrorReturnBlankedValueSpecOneToOneNotBlank(t *testing.T) {
	t.Parallel()

	call := &ast.CallExpr{Fun: ast.NewIdent("NewThing")}
	valueSpec := &ast.ValueSpec{
		Names:  []*ast.Ident{ast.NewIdent("err")},
		Values: []ast.Expr{call},
	}
	if isErrorReturnBlankedValueSpec(valueSpec, call) {
		t.Fatal("expected non-blank var assignment to be ignored")
	}
}

func TestIsErrorReturnBlankedValueSpec_MultiValueSingleCall(t *testing.T) {
	t.Parallel()

	call := &ast.CallExpr{Fun: ast.NewIdent("NewThing")}
	valueSpec := &ast.ValueSpec{
		Names:  []*ast.Ident{ast.NewIdent("thing"), ast.NewIdent("_")},
		Values: []ast.Expr{call},
	}
	if !isErrorReturnBlankedValueSpec(valueSpec, call) {
		t.Fatal("expected multi-value var assignment with trailing blank error to be detected")
	}
}

func TestIsErrorReturnBlankedValueSpec_MultiValueSingleCallNotBlank(t *testing.T) {
	t.Parallel()

	call := &ast.CallExpr{Fun: ast.NewIdent("NewThing")}
	valueSpec := &ast.ValueSpec{
		Names:  []*ast.Ident{ast.NewIdent("thing"), ast.NewIdent("err")},
		Values: []ast.Expr{call},
	}
	if isErrorReturnBlankedValueSpec(valueSpec, call) {
		t.Fatal("expected multi-value var assignment with named error to be ignored")
	}
}

func TestIsErrorReturnBlankedValueSpec_CallNotFound(t *testing.T) {
	t.Parallel()

	call := &ast.CallExpr{Fun: ast.NewIdent("NewThing")}
	other := &ast.CallExpr{Fun: ast.NewIdent("NewOther")}
	valueSpec := &ast.ValueSpec{
		Names:  []*ast.Ident{ast.NewIdent("_")},
		Values: []ast.Expr{other},
	}
	if isErrorReturnBlankedValueSpec(valueSpec, call) {
		t.Fatal("expected false when constructor call is not present in ValueSpec values")
	}
}
