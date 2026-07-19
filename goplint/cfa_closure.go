// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"strconv"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// inspectClosureCastsCFA analyzes a FuncLit body for unvalidated casts using
// its own CFG and independent validation scope. Each closure receives the same
// canonical path-sensitive protocol analysis as a declared function.
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
	cfgMaxStates int,
	cfgWitnessMaxSteps int,
	refinement cfgProtocolRefinementOptions,
	ssaRes *ssaResult,
	calleeSummaryCache *sync.Map,
	rootAvailability ssaAvailability,
) error {
	if lit.Body == nil {
		return nil
	}

	// Build the protocol CFG for this closure's body.
	closureCFG := buildProtocolCFG(pass, lit.Body, ssaRes)
	if closureCFG == nil {
		return nil
	}
	solver := newInterprocSolverWithSSA(pass, ssaRes, calleeSummaryCache)
	refiner := newCFGRefinementController(refinement)
	effectiveBudget := adaptiveBlockVisitBudget(
		closureCFG,
		blockVisitBudget{maxStates: cfgMaxStates},
	)
	ssaAvailability := protocolSSAAvailabilityForClosure(ssaRes, lit)
	if !rootAvailability.ready() {
		ssaAvailability = rootAvailability
	}

	parentMap := buildParentMap(lit.Body)

	// Collect casts for this procedure only. Nested closures are routed exactly
	// once from the package-wide protocol procedure inventory.
	assignedCasts, unassignedCasts, closureCalls, _ := collectCFACasts(
		pass, lit.Body, parentMap,
		func(*ast.FuncLit, int) {},
	)

	// Canonical SSA must-alias enrichment is mandatory.
	if aliasAvailability := enrichAssignedCastsWithSSAClosure(pass, ssaRes, lit, assignedCasts); !aliasAvailability.ready() {
		ssaAvailability = aliasAvailability
	}

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
		methodValueCalls := collectMethodValueValidateCalls(pass, lit.Body)
		calleeValidatedCalls := collectCalleeValidatedCalls(
			pass,
			lit.Body,
			ssaRes,
			stackScopeFromMap(nil, ssaRes),
			calleeSummaryCache,
		)
		pathMethodCalls = mergeMethodValueValidateCallSets(methodValueCalls, calleeValidatedCalls)
		if checkUBV {
			ubvSyncLits = collectUBVClosureLits(lit.Body)
			ubvSyncCalls = collectUBVClosureVarCalls(closureCalls)
			ubvMethodCalls = pathMethodCalls
		}
	}

	// Report assigned casts with unvalidated paths.
	scope := newClosureCFACastFindingScope(qualEnclosingFunc, closurePrefix)
	for _, ac := range assignedCasts {
		defBlock, defIdx := findDefiningBlock(closureCFG, ac.assign)
		if defBlock == nil {
			continue
		}

		originKey := semanticNodeKey(pass, ac.pos.Pos())
		callChain := scope.callChain
		pathAnchors := map[string]string{
			"witness_cast_pos": originKey,
			"witness_def_block": strconv.FormatInt(
				int64(defBlock.Index),
				10,
			),
		}
		castInput := interprocCastPathInput{
			CFG:             closureCFG,
			ParentMap:       parentMap,
			DefBlock:        defBlock,
			DefIdx:          defIdx,
			Target:          ac.target,
			TypeName:        ac.typeName,
			OriginKey:       originKey,
			SyncLits:        pathSyncLits,
			SyncCalls:       pathSyncCalls,
			MethodCalls:     pathMethodCalls,
			MaxStates:       effectiveBudget.maxStates,
			CallChain:       callChain,
			AllowSafe:       refinement.AllowsSafeResult(),
			SSAAvailability: ac.protocolAvailability(ssaAvailability),
		}
		pathSolver := solver.withControl(refiner.newDeadline())
		pathResult := pathSolver.EvaluateCastPath(castInput)
		_, pathResult = refineAssignedCastPathResult(
			pass,
			scope,
			refiner,
			pathSolver,
			closureCFG,
			ac,
			castInput,
			pathResult,
			pathAnchors,
		)
		pathOutcome := pathResult.toPathOutcome()
		pathReason := pathResult.Reason
		pathWitness := pathResult.Witness
		if pathOutcome == pathOutcomeSafe {
			// All paths validate. Use-before-validation is evaluated once from
			// the definition instruction; the qualified hazard witness determines
			// whether the diagnostic is same-block or cross-block.
			if checkUBV {
				ubvInput := interprocUBVCrossBlockInput{
					Target:          ac.target,
					DefBlock:        defBlock,
					DefIdx:          defIdx,
					OriginKey:       originKey,
					TypeName:        ac.typeName,
					SyncLits:        ubvSyncLits,
					SyncCalls:       ubvSyncCalls,
					MethodCalls:     ubvMethodCalls,
					MaxStates:       effectiveBudget.maxStates,
					CallChain:       callChain,
					SSAAvailability: ac.protocolAvailability(ssaAvailability),
				}
				ubvSolver := solver.withControl(refiner.newDeadline())
				ubvResult := ubvSolver.EvaluateUBVCrossBlock(ubvInput)
				_, ubvResult = refineUBVResult(
					pass,
					scope,
					refiner,
					ubvSolver,
					closureCFG,
					ac,
					ubvInput,
					ubvResult,
					pathAnchors,
				)
				reportUBVResult(pass, scope, bl, effectiveBudget, cfgWitnessMaxSteps, ac, ubvResult, ubvInput)
			}
			continue
		}
		if pathOutcome == pathOutcomeInconclusive {
			if checkUBV && !ssaAvailability.ready() {
				reportSSAUnavailableUBVInconclusive(
					pass,
					scope,
					bl,
					effectiveBudget,
					cfgWitnessMaxSteps,
					ac,
					pathResult,
					originKey,
					defBlock,
				)
			}
			reportAssignedCastInconclusive(
				pass,
				scope,
				bl,
				effectiveBudget,
				cfgWitnessMaxSteps,
				ac,
				pathReason,
				pathWitness,
				pathResult,
				originKey,
				defBlock,
			)
			continue
		}
		if protocolPolicySuppressesDefiniteFinding(
			pathOutcome,
			func() bool { return excCfg != nil && excCfg.isExcepted(scope.exceptionKey) },
			func() bool { return hasIgnoreAtPos(pass, ac.pos.Pos()) },
		) {
			continue
		}

		msg := unvalidatedCastMessage(ac.typeName)
		findingID := scope.findingID(pass, CategoryUnvalidatedCast, ac.typeName, "assigned", originKey)
		var meta map[string]string
		if pathResult.Refinement.Enabled {
			meta = appendProtocolRefinementMeta(copyFindingMeta(pathAnchors), pathResult)
			addCFGWitnessMeta(meta, pathWitness, cfgWitnessMaxSteps)
			addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
		}
		reportFindingWithMetaIfNotBaselined(pass, bl, ac.pos.Pos(), CategoryUnvalidatedCast, findingID, msg, meta)
	}

	// Report unassigned casts.
	for _, uc := range unassignedCasts {
		if uc.uncertainSourceTypeSet {
			reportUnassignedCastInconclusive(pass, scope, bl, effectiveBudget, uc)
			continue
		}
		if protocolPolicySuppressesDefiniteFinding(
			pathOutcomeUnsafe,
			func() bool { return excCfg != nil && excCfg.isExcepted(scope.exceptionKey) },
			func() bool { return hasIgnoreAtPos(pass, uc.pos.Pos()) },
		) {
			continue
		}

		msg := unvalidatedCastMessage(uc.typeName)
		findingID := scope.findingID(pass, CategoryUnvalidatedCast, uc.typeName, "unassigned", semanticNodeKey(pass, uc.pos.Pos()))
		reportFindingIfNotBaselined(pass, bl, uc.pos.Pos(), CategoryUnvalidatedCast, findingID, msg)
	}

	return nil
}
