// SPDX-License-Identifier: MPL-2.0

package goplint

import (
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
	ubvMode string,
	cfgBackend string,
	cfgInterprocEngine string,
	cfgMaxStates int,
	cfgMaxDepth int,
	cfgInconclusivePolicy string,
	cfgWitnessMaxSteps int,
	phaseC cfgPhaseCOptions,
	cfgAliasMode string,
	ssaRes *ssaResult,
) error {
	if lit.Body == nil {
		return nil
	}

	// Build CFG for this closure's body.
	closureCFG := buildFuncCFGForBackend(pass, lit.Body, cfgBackend)
	if closureCFG == nil {
		return nil
	}
	solver := newInterprocSolver(pass, cfgBackend, cfgInterprocEngine)
	compatTracker := newInterprocCompatTracker(cfgInterprocEngine)
	refiner := newCFGRefinementController(phaseC)
	effectiveBudget := adaptiveBlockVisitBudget(
		closureCFG,
		blockVisitBudget{maxStates: cfgMaxStates, maxDepth: cfgMaxDepth},
	)
	noReturnAliases := collectNoReturnFuncAliasEvents(pass, lit.Body)

	parentMap := buildParentMap(lit.Body)

	// Collect casts using the shared CFA collection logic.
	// Nested closures are analyzed recursively with compound prefixes.
	var nestedErr error
	assignedCasts, unassignedCasts, closureCalls, _ := collectCFACasts(
		pass, lit.Body, parentMap,
		func(nested *ast.FuncLit, nestedIdx int) {
			if nestedErr != nil {
				return
			}
			nestedPrefix := closurePrefix + "/" + strconv.Itoa(nestedIdx)
			nestedErr = inspectClosureCastsCFA(
				pass,
				nested,
				qualEnclosingFunc,
				nestedPrefix,
				excCfg,
				bl,
				checkUBV,
				ubvMode,
				cfgBackend,
				cfgInterprocEngine,
				cfgMaxStates,
				cfgMaxDepth,
				cfgInconclusivePolicy,
				cfgWitnessMaxSteps,
				phaseC,
				cfgAliasMode,
				ssaRes,
			)
		},
	)
	if nestedErr != nil {
		return nestedErr
	}

	// Enrich cast targets with SSA-derived alias sets when alias mode is active.
	if cfgAliasMode == cfgAliasModeSSA {
		enrichAssignedCastsWithSSAClosure(ssaRes, lit, assignedCasts)
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
		pathMethodCalls = mergeMethodValueValidateCallSets(
			collectMethodValueValidateCalls(pass, lit.Body),
			collectCalleeValidatedCalls(pass, lit.Body, stackScopeFromMap(nil)),
		)
		if checkUBV {
			ubvSyncLits = collectUBVClosureLits(lit.Body)
			ubvSyncCalls = collectUBVClosureVarCalls(closureCalls)
			ubvMethodCalls = pathMethodCalls
		}
	}

	// Report assigned casts with unvalidated paths.
	scope := newClosureCFACastFindingScope(qualEnclosingFunc, closurePrefix)
	for _, ac := range assignedCasts {
		if excCfg.isExcepted(scope.exceptionKey) {
			continue
		}

		if hasIgnoreAtPos(pass, ac.pos.Pos()) {
			continue
		}

		defBlock, defIdx := findDefiningBlock(closureCFG, ac.assign)
		if defBlock == nil {
			continue
		}

		originKey := stablePosKey(pass, ac.pos.Pos())
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
			DefBlock:        defBlock,
			DefIdx:          defIdx,
			Target:          ac.target,
			TypeName:        ac.typeName,
			OriginKey:       originKey,
			SyncLits:        pathSyncLits,
			SyncCalls:       pathSyncCalls,
			MethodCalls:     pathMethodCalls,
			NoReturnAliases: noReturnAliases,
			MaxStates:       effectiveBudget.maxStates,
			MaxDepth:        effectiveBudget.maxDepth,
			CallChain:       callChain,
			AllowSafe:       phaseC.AllowsSafeResult(),
		}
		pathLegacy := solver.EvaluateCastPathLegacy(castInput)
		pathResult := solver.EvaluateCastPath(castInput)
		pathFindingID, pathResult := refineAssignedCastPathResult(
			pass,
			scope,
			refiner,
			solver,
			closureCFG,
			ac,
			castInput,
			pathResult,
			pathAnchors,
		)
		hasEquivalentUnsafe := pathResult.Class == interprocOutcomeUnsafe
		pathOutcome := pathResult.toPathOutcome()
		pathReason := pathResult.Reason
		pathWitness := pathResult.Witness
		if pathOutcome == pathOutcomeSafe {
			// All paths validated. Check use-before-validate with
			// same-block priority, then cross-block.
			if checkUBV {
				inBlockInput := interprocUBVInBlockInput{
					Target:        ac.target,
					Nodes:         defBlock.Nodes,
					StartIndex:    defIdx + 1,
					Mode:          ubvMode,
					OriginKey:     originKey,
					TypeName:      ac.typeName,
					SyncLits:      ubvSyncLits,
					SyncCalls:     ubvSyncCalls,
					MethodCalls:   ubvMethodCalls,
					DefBlockIndex: defBlock.Index,
					CallChain:     callChain,
				}
				inBlockLegacy := solver.EvaluateUBVInBlockLegacy(inBlockInput)
				inBlockResult := solver.EvaluateUBVInBlock(inBlockInput)
				hasEquivalentUnsafe = hasEquivalentUnsafe || inBlockResult.Class == interprocOutcomeUnsafe
				inBlockFindingID, inBlockResult := refineUBVInBlockResult(
					pass,
					scope,
					refiner,
					solver,
					closureCFG,
					ac,
					inBlockInput,
					inBlockResult,
					pathAnchors,
				)
				inBlockOutcome := inBlockResult.toPathOutcome()
				inBlockReason := inBlockResult.Reason
				switch inBlockOutcome {
				case pathOutcomeUnsafe:
					reportSameBlockUBVUnsafe(pass, scope, bl, ac, inBlockResult, originKey, ubvMode, defBlock)
				case pathOutcomeInconclusive:
					reportSameBlockUBVInconclusive(
						pass,
						scope,
						bl,
						cfgBackend,
						effectiveBudget,
						cfgInconclusivePolicy,
						cfgWitnessMaxSteps,
						ac,
						inBlockReason,
						inBlockResult,
						originKey,
						ubvMode,
						defBlock,
					)
				default:
					crossInput := interprocUBVCrossBlockInput{
						Target:      ac.target,
						DefBlock:    defBlock,
						DefIdx:      defIdx,
						Mode:        ubvMode,
						OriginKey:   originKey,
						TypeName:    ac.typeName,
						SyncLits:    ubvSyncLits,
						SyncCalls:   ubvSyncCalls,
						MethodCalls: ubvMethodCalls,
						MaxStates:   effectiveBudget.maxStates,
						MaxDepth:    effectiveBudget.maxDepth,
						CallChain:   callChain,
					}
					crossLegacy := solver.EvaluateUBVCrossBlockLegacy(crossInput)
					crossResult := solver.EvaluateUBVCrossBlock(crossInput)
					hasEquivalentUnsafe = hasEquivalentUnsafe || crossResult.Class == interprocOutcomeUnsafe
					crossFindingID, crossResult := refineUBVCrossBlockResult(
						pass,
						scope,
						refiner,
						solver,
						closureCFG,
						ac,
						crossInput,
						crossResult,
						pathAnchors,
					)
					compatTracker.Check(
						CategoryUseBeforeValidateCrossBlock,
						crossFindingID,
						crossLegacy,
						crossResult,
						hasEquivalentUnsafe,
					)
					ubvOutcome := crossResult.toPathOutcome()
					ubvReason := crossResult.Reason
					ubvWitness := crossResult.Witness
					if ubvOutcome == pathOutcomeUnsafe {
						reportCrossBlockUBVUnsafe(pass, scope, bl, cfgWitnessMaxSteps, ac, crossResult, ubvWitness, originKey, ubvMode, defBlock)
					} else if ubvOutcome == pathOutcomeInconclusive {
						reportCrossBlockUBVInconclusive(
							pass,
							scope,
							bl,
							cfgBackend,
							effectiveBudget,
							cfgInconclusivePolicy,
							cfgWitnessMaxSteps,
							ac,
							ubvReason,
							ubvWitness,
							crossResult,
							originKey,
							ubvMode,
							defBlock,
						)
					}
				}
				compatTracker.Check(
					CategoryUseBeforeValidateSameBlock,
					inBlockFindingID,
					inBlockLegacy,
					inBlockResult,
					hasEquivalentUnsafe,
				)
			}
			compatTracker.Check(
				CategoryUnvalidatedCast,
				pathFindingID,
				pathLegacy,
				pathResult,
				hasEquivalentUnsafe,
			)
			continue
		}
		compatTracker.Check(
			CategoryUnvalidatedCast,
			pathFindingID,
			pathLegacy,
			pathResult,
			hasEquivalentUnsafe,
		)
		if pathOutcome == pathOutcomeInconclusive {
			reportAssignedCastInconclusive(
				pass,
				scope,
				bl,
				cfgBackend,
				effectiveBudget,
				cfgInconclusivePolicy,
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

		msg := unvalidatedCastMessage(ac.typeName)
		findingID := scope.findingID(pass, CategoryUnvalidatedCast, ac.typeName, "assigned", originKey, ac.target.key())
		var meta map[string]string
		if pathResult.PhaseC.Enabled {
			meta = appendPhaseCMeta(copyFindingMeta(pathAnchors), pathResult)
			addCFGWitnessMeta(meta, pathWitness, cfgWitnessMaxSteps)
			addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
		}
		reportFindingWithMetaIfNotBaselined(pass, bl, ac.pos.Pos(), CategoryUnvalidatedCast, findingID, msg, meta)
	}

	// Report unassigned casts.
	for _, uc := range unassignedCasts {
		if excCfg.isExcepted(scope.exceptionKey) {
			continue
		}

		if hasIgnoreAtPos(pass, uc.pos.Pos()) {
			continue
		}

		msg := unvalidatedCastMessage(uc.typeName)
		findingID := scope.findingID(pass, CategoryUnvalidatedCast, uc.typeName, "unassigned", stablePosKey(pass, uc.pos.Pos()))
		reportFindingIfNotBaselined(pass, bl, uc.pos.Pos(), CategoryUnvalidatedCast, findingID, msg)
	}

	return compatTracker.Err()
}
