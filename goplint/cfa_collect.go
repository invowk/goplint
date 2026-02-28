// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// cfaAssignedCast records a type conversion assigned to a named variable,
// along with its containing AssignStmt for CFG lookup.
type cfaAssignedCast struct {
	varName  string
	typeName string
	pos      ast.Node
	assign   ast.Node // the AssignStmt containing this cast
	castIndex int
}

// cfaUnassignedCast records a type conversion not assigned to a named
// variable (e.g., return, function argument, blank identifier).
type cfaUnassignedCast struct {
	typeName string
	pos      ast.Node
	castIndex int
}

// cfaClosureHandler is called when the cast-collection walk encounters a
// FuncLit. Implementations decide how to handle the closure (e.g.,
// delegate to inspectClosureCastsCFA or recurse for nested closures).
// Returning false from the outer walk callback prevents descent into
// the closure body.
type cfaClosureHandler func(lit *ast.FuncLit, closureIdx int)

// collectCFACasts walks a function or closure body and classifies type
// conversions from raw primitives to DDD Value Types into assigned and
// unassigned casts. Closures are delegated to the provided handler
// rather than being analyzed inline — each closure gets its own CFG
// and independent validation scope.
//
// This is the shared cast-collection logic used by both
// inspectUnvalidatedCastsCFA (outer functions) and
// inspectClosureCastsCFA (closure bodies). The walk root is always
// body (*ast.BlockStmt), so the parent *ast.FuncLit is never visited.
func collectCFACasts(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	parentMap map[ast.Node]ast.Node,
	onClosure cfaClosureHandler,
) ([]cfaAssignedCast, []cfaUnassignedCast) {
	var assignedCasts []cfaAssignedCast
	var unassignedCasts []cfaUnassignedCast
	castIndex := 0
	closureIndex := 0

	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			onClosure(lit, closureIndex)
			closureIndex++
			return false
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Not a type conversion — skip.
		tv, ok := pass.TypesInfo.Types[call.Fun]
		if !ok || !tv.IsType() {
			return true
		}

		if len(call.Args) != 1 {
			return true
		}

		// Target must have Validate() — i.e., it's a DDD Value Type.
		targetType := tv.Type
		if !hasValidateMethod(targetType) {
			return true
		}

		// Source must be a raw primitive from a runtime expression.
		srcTV, srcOK := pass.TypesInfo.Types[call.Args[0]]
		if !srcOK {
			return true
		}
		if srcTV.Value != nil {
			return true // constant expression — skip
		}
		if isErrorMessageExpr(pass, call.Args[0]) {
			return true // error-message source — skip
		}
		if !isRawPrimitive(srcTV.Type) {
			return true // named-to-named cast — skip
		}

		targetTypeName := qualifiedTypeName(targetType, pass.Pkg)
		parent := parentMap[call]

		// Check if assigned to a named variable.
		if assign, ok := parent.(*ast.AssignStmt); ok {
			for i, rhs := range assign.Rhs {
				if rhs != call {
					continue
				}
				if i < len(assign.Lhs) {
					if ident, ok := assign.Lhs[i].(*ast.Ident); ok && ident.Name != "_" {
						assignedCasts = append(assignedCasts, cfaAssignedCast{
							varName:  ident.Name,
							typeName: targetTypeName,
							pos:      call,
							assign:   assign,
							castIndex: castIndex,
						})
						castIndex++
						return true
					}
				}
			}
		}

		// Unassigned cast — check auto-skip contexts.
		if isAutoSkipContext(pass, call, parent, parentMap) {
			return true
		}

		unassignedCasts = append(unassignedCasts, cfaUnassignedCast{
			typeName: targetTypeName,
			pos:      call,
			castIndex: castIndex,
		})
		castIndex++
		return true
	})

	return assignedCasts, unassignedCasts
}
