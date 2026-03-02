// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

// inspectUnvalidatedCastsCFA is the CFA-enhanced replacement for
// inspectUnvalidatedCasts. Instead of a name-based heuristic that considers
// any Validate() call in the function body as validating all same-named
// variables, this function uses CFG path reachability to determine whether
// each individual cast has an unvalidated path to a return block.
//
// When --cfa is enabled, this function is called instead of
// inspectUnvalidatedCasts. The two implementations are fully compartmentalized:
// neither imports from the other.
func inspectUnvalidatedCastsCFA(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	excCfg *ExceptionConfig,
	bl *BaselineConfig,
	checkUBV bool,
	checkUBVCross bool,
) {
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

	// Build parent map for auto-skip context detection.
	parentMap := buildParentMap(fn.Body)

	// Build the CFG for path-sensitive analysis.
	funcCFG := buildFuncCFGForPass(pass, fn.Body)
	if funcCFG == nil {
		return
	}

	// Collect casts using the shared CFA collection logic.
	// Closures are delegated to inspectClosureCastsCFA.
	assignedCasts, unassignedCasts, closureCalls, _ := collectCFACasts(
		pass, fn.Body, parentMap,
		func(lit *ast.FuncLit, closureIdx int) {
			inspectClosureCastsCFA(pass, lit, qualFuncName, strconv.Itoa(closureIdx), excCfg, bl, checkUBV, checkUBVCross)
		},
	)

	// Collect closure classifications lazily — only needed when assigned casts exist.
	// Path validation includes deferred closures + IIFEs; UBV ordering uses only IIFEs.
	var pathSyncLits map[*ast.FuncLit]bool
	var ubvSyncLits map[*ast.FuncLit]bool
	var pathSyncCalls closureVarCallSet
	var ubvSyncCalls closureVarCallSet
	var pathMethodCalls methodValueValidateCallSet
	var ubvMethodCalls methodValueValidateCallSet
	if len(assignedCasts) > 0 {
		pathSyncLits = collectSynchronousClosureLits(fn.Body)
		pathSyncCalls = collectSynchronousClosureVarCalls(closureCalls)
		pathMethodCalls = collectMethodValueValidateCalls(pass, fn.Body)
		if checkUBV || checkUBVCross {
			ubvSyncLits = collectUBVClosureLits(fn.Body)
			ubvSyncCalls = collectUBVClosureVarCalls(closureCalls)
			ubvMethodCalls = pathMethodCalls
		}
	}

	// Report assigned casts where an unvalidated path to return exists.
	for _, ac := range assignedCasts {
		excKey := qualFuncName + ".cast-validation"
		if excCfg.isExcepted(excKey) {
			continue
		}

		// Check inline //goplint:ignore directive on the cast statement.
		if hasIgnoreAtPos(pass, ac.pos.Pos()) {
			continue
		}

		// Find the assignment in the CFG.
		defBlock, defIdx := findDefiningBlock(funcCFG, ac.assign)
		if defBlock == nil {
			// Node not found in CFG — likely in dead code. Skip.
			continue
		}

		// Check if there's any path from the cast to a return block
		// that doesn't pass through varName.Validate().
		if !hasPathToReturnWithoutValidate(pass, funcCFG, defBlock, defIdx, ac.target, pathSyncLits, pathSyncCalls, pathMethodCalls) {
			// All paths DO have validate. Check for use-before-validate:
			// same-block takes priority over cross-block — both cannot fire
			// on the same cast. --check-all only enables same-block.
			if checkUBV && hasUseBeforeValidateInBlock(pass, defBlock.Nodes, defIdx+1, ac.target, ubvSyncLits, ubvSyncCalls, ubvMethodCalls) {
				ubvMsg := fmt.Sprintf("variable %s of type %s used before Validate() in same block", ac.target.displayName, ac.typeName)
				ubvID := PackageScopedFindingID(pass,
					CategoryUseBeforeValidate,
					"cfa",
					qualFuncName,
					ac.typeName,
					"ubv",
					stablePosKey(pass, ac.pos.Pos()),
					ac.target.key(),
				)
				if !bl.ContainsFinding(CategoryUseBeforeValidate, ubvID, ubvMsg) {
					reportDiagnostic(pass, ac.pos.Pos(), CategoryUseBeforeValidate, ubvID, ubvMsg)
				}
			} else if checkUBVCross && hasUseBeforeValidateCrossBlock(pass, defBlock, defIdx, ac.target, ubvSyncLits, ubvSyncCalls, ubvMethodCalls) {
				// Cross-block UBV: the variable is used in a successor
				// block before any block on that path calls Validate().
				ubvMsg := fmt.Sprintf("variable %s of type %s used before Validate() across blocks", ac.target.displayName, ac.typeName)
				ubvID := PackageScopedFindingID(pass,
					CategoryUseBeforeValidate,
					"cfa",
					qualFuncName,
					ac.typeName,
					"ubv-xblock",
					stablePosKey(pass, ac.pos.Pos()),
					ac.target.key(),
				)
				if !bl.ContainsFinding(CategoryUseBeforeValidate, ubvID, ubvMsg) {
					reportDiagnostic(pass, ac.pos.Pos(), CategoryUseBeforeValidate, ubvID, ubvMsg)
				}
			}
			continue // all paths validated
		}

		msg := fmt.Sprintf("type conversion to %s from non-constant without Validate() check", ac.typeName)
		findingID := PackageScopedFindingID(pass,
			CategoryUnvalidatedCast,
			"cfa",
			qualFuncName,
			ac.typeName,
			"assigned",
			stablePosKey(pass, ac.pos.Pos()),
			ac.target.key(),
		)
		if bl.ContainsFinding(CategoryUnvalidatedCast, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, ac.pos.Pos(), CategoryUnvalidatedCast, findingID, msg)
	}

	// Unassigned casts: always report (no variable to track).
	for _, uc := range unassignedCasts {
		excKey := qualFuncName + ".cast-validation"
		if excCfg.isExcepted(excKey) {
			continue
		}

		// Check inline //goplint:ignore directive on the cast expression.
		if hasIgnoreAtPos(pass, uc.pos.Pos()) {
			continue
		}

		msg := fmt.Sprintf("type conversion to %s from non-constant without Validate() check", uc.typeName)
		findingID := PackageScopedFindingID(pass,
			CategoryUnvalidatedCast,
			"cfa",
			qualFuncName,
			uc.typeName,
			"unassigned",
			stablePosKey(pass, uc.pos.Pos()),
		)
		if bl.ContainsFinding(CategoryUnvalidatedCast, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, uc.pos.Pos(), CategoryUnvalidatedCast, findingID, msg)
	}
}
