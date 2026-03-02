// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

// inspectClosureCastsCFA analyzes a FuncLit body for unvalidated casts
// using its own CFG and independent validation scope. This is the CFA
// counterpart to the AST mode's closure skip — instead of ignoring closures
// entirely, CFA mode builds a per-closure CFG and performs the same
// path-reachability analysis.
//
// Each closure gets its own cast index counter (starting from 0) and
// finding IDs include "cfa", "closure", and the closure's prefix
// within the enclosing function for uniqueness. Nested closures use
// compound prefixes (e.g., "0/1" for the second closure inside the first).
func inspectClosureCastsCFA(
	pass *analysis.Pass,
	lit *ast.FuncLit,
	qualEnclosingFunc string,
	closurePrefix string,
	excCfg *ExceptionConfig,
	bl *BaselineConfig,
	checkUBV bool,
	checkUBVCross bool,
) {
	if lit.Body == nil {
		return
	}

	// Build CFG for this closure's body.
	closureCFG := buildFuncCFGForPass(pass, lit.Body)
	if closureCFG == nil {
		return
	}

	parentMap := buildParentMap(lit.Body)

	// Collect casts using the shared CFA collection logic.
	// Nested closures are analyzed recursively with compound prefixes.
	assignedCasts, unassignedCasts, closureCalls, _ := collectCFACasts(
		pass, lit.Body, parentMap,
		func(nested *ast.FuncLit, nestedIdx int) {
			nestedPrefix := closurePrefix + "/" + strconv.Itoa(nestedIdx)
			inspectClosureCastsCFA(pass, nested, qualEnclosingFunc, nestedPrefix, excCfg, bl, checkUBV, checkUBVCross)
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
		pathSyncLits = collectSynchronousClosureLits(lit.Body)
		pathSyncCalls = collectSynchronousClosureVarCalls(closureCalls)
		pathMethodCalls = collectMethodValueValidateCalls(pass, lit.Body)
		if checkUBV || checkUBVCross {
			ubvSyncLits = collectUBVClosureLits(lit.Body)
			ubvSyncCalls = collectUBVClosureVarCalls(closureCalls)
			ubvMethodCalls = pathMethodCalls
		}
	}

	// Report assigned casts with unvalidated paths.
	for _, ac := range assignedCasts {
		excKey := qualEnclosingFunc + ".cast-validation"
		if excCfg.isExcepted(excKey) {
			continue
		}

		if hasIgnoreAtPos(pass, ac.pos.Pos()) {
			continue
		}

		defBlock, defIdx := findDefiningBlock(closureCFG, ac.assign)
		if defBlock == nil {
			continue
		}

		if !hasPathToReturnWithoutValidate(pass, closureCFG, defBlock, defIdx, ac.target, pathSyncLits, pathSyncCalls, pathMethodCalls) {
			// All paths validated. Check for use-before-validate (same-block first, then cross-block).
			if checkUBV && hasUseBeforeValidateInBlock(pass, defBlock.Nodes, defIdx+1, ac.target, ubvSyncLits, ubvSyncCalls, ubvMethodCalls) {
				ubvMsg := fmt.Sprintf("variable %s of type %s used before Validate() in same block", ac.target.displayName, ac.typeName)
				ubvID := PackageScopedFindingID(pass,
					CategoryUseBeforeValidate,
					"cfa",
					"closure",
					closurePrefix,
					qualEnclosingFunc,
					ac.typeName,
					"ubv",
					stablePosKey(pass, ac.pos.Pos()),
					ac.target.key(),
				)
				if !bl.ContainsFinding(CategoryUseBeforeValidate, ubvID, ubvMsg) {
					reportDiagnostic(pass, ac.pos.Pos(), CategoryUseBeforeValidate, ubvID, ubvMsg)
				}
			} else if checkUBVCross && hasUseBeforeValidateCrossBlock(pass, defBlock, defIdx, ac.target, ubvSyncLits, ubvSyncCalls, ubvMethodCalls) {
				ubvMsg := fmt.Sprintf("variable %s of type %s used before Validate() across blocks", ac.target.displayName, ac.typeName)
				ubvID := PackageScopedFindingID(pass,
					CategoryUseBeforeValidate,
					"cfa",
					"closure",
					closurePrefix,
					qualEnclosingFunc,
					ac.typeName,
					"ubv-xblock",
					stablePosKey(pass, ac.pos.Pos()),
					ac.target.key(),
				)
				if !bl.ContainsFinding(CategoryUseBeforeValidate, ubvID, ubvMsg) {
					reportDiagnostic(pass, ac.pos.Pos(), CategoryUseBeforeValidate, ubvID, ubvMsg)
				}
			}
			continue
		}

		msg := fmt.Sprintf("type conversion to %s from non-constant without Validate() check", ac.typeName)
		findingID := PackageScopedFindingID(pass,
			CategoryUnvalidatedCast,
			"cfa",
			"closure",
			closurePrefix,
			qualEnclosingFunc,
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

	// Report unassigned casts.
	for _, uc := range unassignedCasts {
		excKey := qualEnclosingFunc + ".cast-validation"
		if excCfg.isExcepted(excKey) {
			continue
		}

		if hasIgnoreAtPos(pass, uc.pos.Pos()) {
			continue
		}

		msg := fmt.Sprintf("type conversion to %s from non-constant without Validate() check", uc.typeName)
		findingID := PackageScopedFindingID(pass,
			CategoryUnvalidatedCast,
			"cfa",
			"closure",
			closurePrefix,
			qualEnclosingFunc,
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
