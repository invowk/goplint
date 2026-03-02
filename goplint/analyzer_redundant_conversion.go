// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// inspectRedundantConversions walks a function body to detect redundant
// intermediate type conversions of the form NamedType(basic(namedExpr)),
// where both the outer target and the inner argument are named types that
// share the same underlying type. The intermediate hop through the basic
// type (e.g., string, int) is unnecessary — Go allows direct conversion
// between named types with identical underlying types.
//
// This pattern commonly arises when converting between DDD Value Types
// from different packages (e.g., tuiserver.AuthToken → runtime.TUIServerToken)
// via an explicit string() intermediary. The intermediate conversion also
// causes false positives in cast-validation (which sees string as a "raw
// primitive" source), so fixing these reduces noise in the DDD pipeline.
func inspectRedundantConversions(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	if fn.Body == nil {
		return
	}
	if shouldSkipFunc(fn) {
		return
	}
	if hasIgnoreDirective(fn.Doc, nil) {
		return
	}

	funcQualName := qualFuncName(pass, fn)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		outerCall, ok := n.(*ast.CallExpr)
		if !ok || len(outerCall.Args) != 1 {
			return true
		}

		// Step 1: Is the outer call a type conversion?
		outerTV, ok := pass.TypesInfo.Types[outerCall.Fun]
		if !ok || !outerTV.IsType() {
			return true
		}

		// Step 2: Is the single argument also a type conversion?
		innerCall, ok := outerCall.Args[0].(*ast.CallExpr)
		if !ok || len(innerCall.Args) != 1 {
			return true
		}

		innerTV, ok := pass.TypesInfo.Types[innerCall.Fun]
		if !ok || !innerTV.IsType() {
			return true
		}

		// Step 3: Is the inner target a basic (non-named) type?
		innerTarget := types.Unalias(innerTV.Type)
		if _, isBasic := innerTarget.(*types.Basic); !isBasic {
			return true
		}

		// Step 4: Is the inner argument a named type?
		innerArgType := pass.TypesInfo.TypeOf(innerCall.Args[0])
		if innerArgType == nil {
			return true
		}
		innerArgResolved := types.Unalias(innerArgType)
		if _, isNamed := innerArgResolved.(*types.Named); !isNamed {
			return true
		}

		// Step 5: Do the outer target and inner argument share the same
		// underlying type? If so, the intermediate basic-type hop is
		// redundant — the outer conversion can accept the inner argument
		// directly.
		outerTarget := types.Unalias(outerTV.Type)
		if !types.Identical(outerTarget.Underlying(), innerArgResolved.Underlying()) {
			return true
		}

		// Build diagnostic.
		outerName := qualifiedTypeName(outerTarget, pass.Pkg)
		basicName := innerTarget.(*types.Basic).Name()

		excKey := funcQualName + ".redundant-conversion"
		if cfg.isExcepted(excKey) {
			return true
		}

		posKey := stablePosKey(pass, outerCall.Pos())
		msg := fmt.Sprintf(
			"redundant intermediate conversion to %s in %s(%s(...)); use %s(...) directly",
			basicName, outerName, basicName, outerName,
		)
		findingID := PackageScopedFindingID(pass, CategoryRedundantConversion, funcQualName, posKey)
		if bl.ContainsFinding(CategoryRedundantConversion, findingID, msg) {
			return true
		}

		reportDiagnostic(pass, outerCall.Pos(), CategoryRedundantConversion, findingID, msg)
		return true
	})
}
