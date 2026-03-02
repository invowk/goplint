// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// resolveCastAssignmentTarget resolves whether a cast call is assigned to a
// trackable LHS target. It handles parenthesized RHS forms like:
//   - x := (DDDType(raw))
//   - var x DDDType = ((DDDType(raw)))
//
// The returned assignment node is either *ast.AssignStmt or *ast.ValueSpec.
func resolveCastAssignmentTarget(
	pass *analysis.Pass,
	call *ast.CallExpr,
	parentMap map[ast.Node]ast.Node,
) (target castTarget, assignment ast.Node, ok bool) {
	assignedNode, parent := parentAfterParens(call, parentMap)
	if parent == nil {
		return castTarget{}, nil, false
	}

	switch p := parent.(type) {
	case *ast.AssignStmt:
		idx := exprNodeIndex(p.Rhs, assignedNode)
		if idx < 0 || idx >= len(p.Lhs) {
			return castTarget{}, nil, false
		}
		target, ok := castTargetFromExpr(pass, p.Lhs[idx])
		if !ok {
			return castTarget{}, nil, false
		}
		return target, p, true
	case *ast.ValueSpec:
		idx := exprNodeIndex(p.Values, assignedNode)
		if idx < 0 || idx >= len(p.Names) {
			return castTarget{}, nil, false
		}
		target, ok := castTargetFromExpr(pass, p.Names[idx])
		if !ok {
			return castTarget{}, nil, false
		}
		return target, p, true
	default:
		return castTarget{}, nil, false
	}
}

// parentAfterParens walks upward through parenthesized expression wrappers and
// returns the outer-most parenthesized node (or the original node) plus its
// immediate non-paren parent.
func parentAfterParens(start ast.Node, parentMap map[ast.Node]ast.Node) (ast.Node, ast.Node) {
	current := start
	for {
		parent := parentMap[current]
		paren, ok := parent.(*ast.ParenExpr)
		if !ok || paren.X != current {
			return current, parent
		}
		current = paren
	}
}

func exprNodeIndex(exprs []ast.Expr, target ast.Node) int {
	for i, expr := range exprs {
		if expr == target {
			return i
		}
	}
	return -1
}
