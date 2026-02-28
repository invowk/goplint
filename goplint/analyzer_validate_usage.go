// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// inspectValidateUsage walks a function body to detect misuse of Validate()
// calls on DDD Value Types where the error return is completely discarded:
//
//   - unused-validate-result: The error return is discarded as a bare
//     expression statement (x.Validate()) or assigned to blank identifier
//     (_ = x.Validate()).
func inspectValidateUsage(pass *analysis.Pass, fn *ast.FuncDecl, cfg *ExceptionConfig, bl *BaselineConfig) {
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

	// Build a parent map for detecting expression statements and blank
	// assignments containing Validate() calls.
	parentMap := buildParentMap(fn.Body)

	inspectValidateUsageInBody(pass, fn.Body, parentMap, qualFuncName, cfg, bl)
}

// inspectValidateUsageInBody checks a block statement for discarded
// Validate() results. Closures are analyzed independently with their
// own parent maps and fresh scopes.
func inspectValidateUsageInBody(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	parentMap map[ast.Node]ast.Node,
	qualFuncName string,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	ast.Inspect(body, func(n ast.Node) bool {
		// Analyze closure bodies independently with their own parent maps.
		if lit, ok := n.(*ast.FuncLit); ok {
			closureParentMap := buildParentMap(lit.Body)
			inspectValidateUsageInBody(pass, lit.Body, closureParentMap, qualFuncName, cfg, bl)
			return false
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Check for x.Validate() pattern.
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Validate" {
			return true
		}

		// Verify the receiver type actually has Validate() error.
		recvType := pass.TypesInfo.TypeOf(sel.X)
		if recvType == nil || !hasValidateMethod(recvType) {
			return true
		}

		parent := parentMap[call]

		// Check 1: Result completely discarded as a bare expression statement.
		// Pattern: x.Validate() as a standalone statement.
		if _, isExprStmt := parent.(*ast.ExprStmt); isExprStmt {
			reportValidateUsageFinding(pass, call.Pos(), qualFuncName, cfg, bl,
				"Validate() result discarded — error return is unused")
			return true
		}

		// Check 2: Error assigned to blank identifier.
		// Pattern: _ = x.Validate()
		if assign, isAssign := parent.(*ast.AssignStmt); isAssign {
			if isAllBlankForValidate(assign, call) {
				reportValidateUsageFinding(pass, call.Pos(), qualFuncName, cfg, bl,
					"Validate() result discarded — error return is unused")
				return true
			}
		}

		return true
	})
}

// isAllBlankForValidate reports whether the LHS of the assignment for the
// Validate() call is a blank identifier (_). Since Validate() returns a
// single error, we check if the corresponding LHS is blank.
func isAllBlankForValidate(assign *ast.AssignStmt, call *ast.CallExpr) bool {
	// Find which RHS position this call occupies.
	rhsIdx := -1
	for i, rhs := range assign.Rhs {
		if rhs == call {
			rhsIdx = i
			break
		}
	}
	if rhsIdx < 0 {
		return false
	}

	// For single-value returns, the LHS has one entry matching the RHS.
	if rhsIdx < len(assign.Lhs) {
		ident, ok := assign.Lhs[rhsIdx].(*ast.Ident)
		return ok && ident.Name == "_"
	}

	return false
}

// reportValidateUsageFinding emits a diagnostic for a Validate() usage issue,
// respecting exception patterns and baseline suppression.
func reportValidateUsageFinding(
	pass *analysis.Pass,
	pos token.Pos,
	qualFuncName string,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
	message string,
) {
	excKey := qualFuncName + ".validate-usage"
	if cfg.isExcepted(excKey) {
		return
	}

	findingID := StableFindingID(CategoryUnusedValidateResult, qualFuncName, message)
	if bl.ContainsFinding(CategoryUnusedValidateResult, findingID, message) {
		return
	}

	reportDiagnostic(pass, pos, CategoryUnusedValidateResult, findingID, message)
}
