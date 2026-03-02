// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"
)

func TestObjectKeyDeterministic(t *testing.T) {
	t.Parallel()

	pkg := types.NewPackage("example.com/project/pkg", "pkg")
	v1 := types.NewVar(token.Pos(42), pkg, "value", types.Typ[types.String])
	v2 := types.NewVar(token.Pos(42), pkg, "value", types.Typ[types.String])

	key1 := objectKey(v1)
	key2 := objectKey(v2)
	if key1 != key2 {
		t.Fatalf("objectKey() should be deterministic for semantic identity: %q != %q", key1, key2)
	}

	v3 := types.NewVar(token.Pos(43), pkg, "value", types.Typ[types.String])
	if objectKey(v1) == objectKey(v3) {
		t.Fatal("expected different declaration positions to produce different object keys")
	}
}

func TestExprStringKey(t *testing.T) {
	t.Parallel()

	if got := exprStringKey(nil); got != "" {
		t.Fatalf("exprStringKey(nil) = %q, want empty", got)
	}

	expr := &ast.SelectorExpr{X: ast.NewIdent("cfg"), Sel: ast.NewIdent("Name")}
	if got := exprStringKey(expr); got != "cfg.Name" {
		t.Fatalf("exprStringKey(selector) = %q, want %q", got, "cfg.Name")
	}
}
