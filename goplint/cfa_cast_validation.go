// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"strconv"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// inspectUnvalidatedCastsCFA runs canonical path-sensitive cast validation.
// Each cast is tracked through SSA identity, conditional validation effects,
// and finite interprocedural propagation.
func inspectUnvalidatedCastsCFA(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
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
	if fn.Body == nil {
		return nil
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
	funcCFG := buildProtocolCFG(pass, fn.Body, ssaRes)
	if funcCFG == nil {
		return nil
	}
	solver := newInterprocSolverWithSSA(pass, ssaRes, calleeSummaryCache)
	refiner := newCFGRefinementController(refinement)
	effectiveBudget := adaptiveBlockVisitBudget(
		funcCFG,
		blockVisitBudget{maxStates: cfgMaxStates},
	)
	ssaAvailability := protocolSSAAvailabilityForDecl(pass, ssaRes, fn)
	if !rootAvailability.ready() {
		ssaAvailability = rootAvailability
	}

	// Collect casts for this procedure only. Every closure is routed exactly
	// once from the package-wide protocol procedure inventory.
	assignedCasts, unassignedCasts, closureCalls, _ := collectCFACasts(
		pass, fn.Body, parentMap,
		func(*ast.FuncLit, int) {},
	)

	// Canonical SSA must-alias enrichment is mandatory.
	if aliasAvailability := enrichAssignedCastsWithSSA(pass, ssaRes, fn, assignedCasts); !aliasAvailability.ready() {
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
		pathSyncLits = collectSynchronousClosureLits(fn.Body)
		pathSyncCalls = collectSynchronousClosureVarCalls(closureCalls)
		methodValueCalls := collectMethodValueValidateCalls(pass, fn.Body)
		calleeValidatedCalls := collectCalleeValidatedCalls(
			pass,
			fn.Body,
			ssaRes,
			stackScopeFromMap(nil, ssaRes),
			calleeSummaryCache,
		)
		pathMethodCalls = mergeMethodValueValidateCallSets(methodValueCalls, calleeValidatedCalls)
		if checkUBV {
			ubvSyncLits = collectUBVClosureLits(fn.Body)
			ubvSyncCalls = collectUBVClosureVarCalls(closureCalls)
			ubvMethodCalls = pathMethodCalls
		}
	}

	// Report assigned casts where an unvalidated path to return exists.
	scope := newFunctionCFACastFindingScope(qualFuncName)
	for _, ac := range assignedCasts {
		// Find the assignment in the CFG.
		defBlock, defIdx := findDefiningBlock(funcCFG, ac.assign)
		if defBlock == nil {
			// Node not found in CFG — likely in dead code. Skip.
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
			Decl:            fn,
			CFG:             funcCFG,
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
			funcCFG,
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
					funcCFG,
					ac,
					ubvInput,
					ubvResult,
					pathAnchors,
				)
				reportUBVResult(pass, scope, bl, effectiveBudget, cfgWitnessMaxSteps, ac, ubvResult, ubvInput)
			}
			continue // all paths validated
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

	// Unassigned casts: always report (no variable to track).
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
