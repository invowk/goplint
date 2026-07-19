// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestTargetKeyForExprRejectsSyntacticIdentityFallback(t *testing.T) {
	t.Parallel()

	pass := &analysis.Pass{TypesInfo: &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Uses:  make(map[*ast.Ident]types.Object),
		Defs:  make(map[*ast.Ident]types.Object),
	}}
	expression := &ast.BinaryExpr{
		X:  ast.NewIdent("left"),
		Op: token.ADD,
		Y:  ast.NewIdent("right"),
	}
	if got := targetKeyForExpr(pass, expression); got != "" {
		t.Fatalf("targetKeyForExpr() = %q, want unresolved identity", got)
	}
}
