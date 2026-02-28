// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// inspectConstructorErrorUsage walks a function body to detect NewXxx()
// constructor calls whose error return is assigned to a blank identifier (_).
//
// Flagged patterns:
//   - result, _ := NewFoo(input) — error explicitly blanked in short declaration
//   - result, _ = NewFoo(input)  — error explicitly blanked in regular assignment
//
// Not flagged:
//   - Functions that don't start with "New"
//   - Functions that don't return error as the last return type
//   - _, err := NewFoo() where the value is blanked but error is captured
//   - Calls inside closures (consistent with other modes)
func inspectConstructorErrorUsage(pass *analysis.Pass, fn *ast.FuncDecl, cfg *ExceptionConfig, bl *BaselineConfig) {
	if fn.Body == nil {
		return
	}
	if shouldSkipFunc(fn) {
		return
	}

	// Build the qualified function name for exception matching.
	pkgName := packageName(pass.Pkg)
	funcName := fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recvName := receiverTypeName(fn.Recv.List[0].Type)
		if recvName != "" {
			funcName = recvName + "." + funcName
		}
	}
	qualFuncName := pkgName + "." + funcName

	inspectConstructorErrorUsageInBody(pass, fn.Body, qualFuncName, cfg, bl)
}

// inspectConstructorErrorUsageInBody checks a block statement for constructor
// calls with blanked error returns. Closures are analyzed independently.
func inspectConstructorErrorUsageInBody(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	qualFuncName string,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	ast.Inspect(body, func(n ast.Node) bool {
		// Analyze closure bodies independently.
		if lit, ok := n.(*ast.FuncLit); ok {
			inspectConstructorErrorUsageInBody(pass, lit.Body, qualFuncName, cfg, bl)
			return false
		}

		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}

		// Look at each RHS expression for constructor calls.
		for _, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				continue
			}

			ctorName := constructorCallName(call)
			if ctorName == "" {
				continue
			}

			if !returnsErrorLast(pass, call) {
				continue
			}

			if !isErrorReturnBlanked(assign, call) {
				continue
			}

			reportConstructorErrorUsageFinding(pass, call.Pos(), qualFuncName, ctorName, cfg, bl)
		}

		return true
	})
}

// constructorCallName extracts the function name from a call expression if
// it starts with "New". Handles both local calls (NewFoo) and selector
// expression calls (pkg.NewFoo, receiver.NewFoo).
// Returns "" if the function name doesn't start with "New".
func constructorCallName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		if len(fn.Name) > 3 && fn.Name[:3] == "New" {
			return fn.Name
		}
	case *ast.SelectorExpr:
		if len(fn.Sel.Name) > 3 && fn.Sel.Name[:3] == "New" {
			return fn.Sel.Name
		}
	}
	return ""
}

// returnsErrorLast reports whether the called function returns at least 2
// values with the last being the error type. Uses the type checker to
// resolve the function's signature.
func returnsErrorLast(pass *analysis.Pass, call *ast.CallExpr) bool {
	// Resolve the function's type via the type checker.
	var sig *types.Signature

	switch fn := call.Fun.(type) {
	case *ast.Ident:
		obj := pass.TypesInfo.Uses[fn]
		if obj == nil {
			return false
		}
		s, ok := obj.Type().(*types.Signature)
		if !ok {
			return false
		}
		sig = s
	case *ast.SelectorExpr:
		sel := pass.TypesInfo.Selections[fn]
		if sel != nil {
			// Method call via receiver.
			s, ok := sel.Type().(*types.Signature)
			if !ok {
				return false
			}
			sig = s
		} else {
			// Qualified package call (pkg.NewFoo).
			obj := pass.TypesInfo.Uses[fn.Sel]
			if obj == nil {
				return false
			}
			s, ok := obj.Type().(*types.Signature)
			if !ok {
				return false
			}
			sig = s
		}
	default:
		return false
	}

	if sig == nil {
		return false
	}

	results := sig.Results()
	if results.Len() < 2 {
		return false
	}

	lastResult := results.At(results.Len() - 1)
	return isErrorType(lastResult.Type())
}

// isErrorReturnBlanked reports whether the error return (last LHS entry)
// of a constructor call is assigned to a blank identifier (_) in the
// given assignment statement.
func isErrorReturnBlanked(assign *ast.AssignStmt, call *ast.CallExpr) bool {
	// For multi-value returns assigned via a single RHS expression,
	// the LHS has multiple entries and the RHS has one entry.
	if len(assign.Rhs) == 1 && assign.Rhs[0] == call && len(assign.Lhs) >= 2 {
		// The last LHS position corresponds to the error return.
		lastLHS := assign.Lhs[len(assign.Lhs)-1]
		ident, ok := lastLHS.(*ast.Ident)
		return ok && ident.Name == "_"
	}
	return false
}

// reportConstructorErrorUsageFinding emits a diagnostic for a constructor
// error return assigned to blank identifier, respecting exception patterns
// and baseline suppression.
func reportConstructorErrorUsageFinding(
	pass *analysis.Pass,
	pos token.Pos,
	qualFuncName string,
	ctorName string,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	excKey := qualFuncName + ".constructor-error-usage"
	if cfg.isExcepted(excKey) {
		return
	}

	msg := fmt.Sprintf("constructor %s error return assigned to blank identifier", ctorName)
	findingID := StableFindingID(CategoryUnusedConstructorError, qualFuncName, ctorName)
	if bl.ContainsFinding(CategoryUnusedConstructorError, findingID, msg) {
		return
	}

	reportDiagnostic(pass, pos, CategoryUnusedConstructorError, findingID, msg)
}
