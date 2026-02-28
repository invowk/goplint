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
) {
	if lit.Body == nil {
		return
	}

	// Build CFG for this closure's body.
	closureCFG := buildFuncCFG(lit.Body)
	if closureCFG == nil {
		return
	}

	parentMap := buildParentMap(lit.Body)

	// Collect casts using the shared CFA collection logic.
	// Nested closures are analyzed recursively with compound prefixes.
	assignedCasts, unassignedCasts := collectCFACasts(
		pass, lit.Body, parentMap,
		func(nested *ast.FuncLit, nestedIdx int) {
			nestedPrefix := closurePrefix + "/" + strconv.Itoa(nestedIdx)
			inspectClosureCastsCFA(pass, nested, qualEnclosingFunc, nestedPrefix, excCfg, bl)
		},
	)

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

		if !hasPathToReturnWithoutValidate(closureCFG, defBlock, defIdx, ac.varName) {
			continue
		}

		msg := fmt.Sprintf("type conversion to %s from non-constant without Validate() check", ac.typeName)
		findingID := StableFindingID(CategoryUnvalidatedCast, "cfa", "closure", closurePrefix, qualEnclosingFunc, ac.typeName, "assigned", strconv.Itoa(ac.castIndex))
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
		findingID := StableFindingID(CategoryUnvalidatedCast, "cfa", "closure", closurePrefix, qualEnclosingFunc, uc.typeName, "unassigned", strconv.Itoa(uc.castIndex))
		if bl.ContainsFinding(CategoryUnvalidatedCast, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, uc.pos.Pos(), CategoryUnvalidatedCast, findingID, msg)
	}
}
