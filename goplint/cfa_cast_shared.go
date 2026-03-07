// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"strconv"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

type cfaCastFindingScope struct {
	exceptionKey string
	findingParts []string
	callChain    []string
}

func newFunctionCFACastFindingScope(qualFuncName string) cfaCastFindingScope {
	return cfaCastFindingScope{
		exceptionKey: qualFuncName + ".cast-validation",
		findingParts: []string{qualFuncName},
		callChain:    []string{qualFuncName},
	}
}

func newClosureCFACastFindingScope(qualEnclosingFunc, closurePrefix string) cfaCastFindingScope {
	return cfaCastFindingScope{
		exceptionKey: qualEnclosingFunc + ".cast-validation",
		findingParts: []string{"closure", closurePrefix, qualEnclosingFunc},
		callChain:    []string{qualEnclosingFunc, "closure:" + closurePrefix},
	}
}

func (s cfaCastFindingScope) findingID(pass *analysis.Pass, category string, parts ...string) string {
	scopedParts := make([]string, 0, len(s.findingParts)+1+len(parts))
	scopedParts = append(scopedParts, "cfa")
	scopedParts = append(scopedParts, s.findingParts...)
	scopedParts = append(scopedParts, parts...)
	return PackageScopedFindingID(pass, category, scopedParts...)
}

func refineAssignedCastPathResult(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	refiner cfgRefinementController,
	solver interprocSolver,
	cfg *gocfg.CFG,
	ac cfaAssignedCast,
	castInput interprocCastPathInput,
	pathResult interprocPathResult,
	pathAnchors map[string]string,
) (string, interprocPathResult) {
	pathFindingID := scope.findingID(
		pass,
		CategoryUnvalidatedCast,
		ac.typeName,
		"assigned",
		castInput.OriginKey,
		ac.target.key(),
	)
	pathResult = refiner.Refine(cfgRefinementRequest{
		Pass:          pass,
		Position:      ac.pos.Pos(),
		CFG:           cfg,
		Result:        pathResult,
		Category:      CategoryUnvalidatedCast,
		FindingID:     pathFindingID,
		CallChain:     scope.callChain,
		OriginAnchors: pathAnchors,
		SyntheticPath: []int32{castInput.DefBlock.Index},
		Rerun: func(override cfgRefinementOverride) interprocPathResult {
			next := castInput
			if override.MaxStates > 0 {
				next.MaxStates = override.MaxStates
			}
			if override.MaxDepth > 0 {
				next.MaxDepth = override.MaxDepth
			}
			next.DischargedWitnesses = override.DischargedWitnesses
			next.AllowSafe = override.AllowSafe
			if override.ResolveTargets {
				next.ResolveCFGCalls = true
			}
			refined := solver.EvaluateCastPath(next)
			if override.ResolveTargets &&
				refined.Class == interprocOutcomeInconclusive &&
				refined.Reason == pathOutcomeReasonUnresolvedTarget {
				refined = mergeResolvedTargetRefinement(refined, solver.EvaluateCastPathLegacy(next))
			}
			return refined
		},
	})
	writeRefinementTraceToSink(pass, ac.pos.Pos(), pathResult)
	return pathFindingID, pathResult
}

func refineUBVInBlockResult(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	refiner cfgRefinementController,
	solver interprocSolver,
	cfg *gocfg.CFG,
	ac cfaAssignedCast,
	inBlockInput interprocUBVInBlockInput,
	inBlockResult interprocPathResult,
	pathAnchors map[string]string,
) (string, interprocPathResult) {
	inBlockFindingID := scope.findingID(
		pass,
		CategoryUseBeforeValidateSameBlock,
		ac.typeName,
		"ubv",
		inBlockInput.OriginKey,
		ac.target.key(),
	)
	inBlockResult = refiner.Refine(cfgRefinementRequest{
		Pass:          pass,
		Position:      ac.pos.Pos(),
		CFG:           cfg,
		Result:        inBlockResult,
		Category:      CategoryUseBeforeValidateSameBlock,
		FindingID:     inBlockFindingID,
		CallChain:     scope.callChain,
		OriginAnchors: pathAnchors,
		SyntheticPath: []int32{inBlockInput.DefBlockIndex},
		Rerun: func(override cfgRefinementOverride) interprocPathResult {
			next := inBlockInput
			if override.RefineRecursion {
				next.SummaryStack = summaryStackWithRecursionFallback(next.SummaryStack)
			}
			return solver.EvaluateUBVInBlock(next)
		},
	})
	writeRefinementTraceToSink(pass, ac.pos.Pos(), inBlockResult)
	return inBlockFindingID, inBlockResult
}

func reportSameBlockUBVUnsafe(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	bl *BaselineConfig,
	ac cfaAssignedCast,
	inBlockResult interprocPathResult,
	originKey string,
	ubvMode string,
	defBlock *gocfg.Block,
) {
	ubvMsg := useBeforeValidateMessage(ac.target.displayName, ac.typeName, false)
	ubvID := scope.findingID(pass, CategoryUseBeforeValidateSameBlock, ac.typeName, "ubv", originKey, ac.target.key())
	meta := appendPhaseCMeta(map[string]string{
		"ubv_mode":         ubvMode,
		"ubv_scope":        "same-block",
		"witness_cast_pos": originKey,
		"witness_def_block": strconv.FormatInt(
			int64(defBlock.Index),
			10,
		),
	}, inBlockResult)
	reportFindingWithMetaIfNotBaselined(pass, bl, ac.pos.Pos(), CategoryUseBeforeValidateSameBlock, ubvID, ubvMsg, meta)
}

func reportSameBlockUBVInconclusive(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	bl *BaselineConfig,
	cfgBackend string,
	effectiveBudget blockVisitBudget,
	cfgInconclusivePolicy string,
	cfgWitnessMaxSteps int,
	ac cfaAssignedCast,
	inBlockReason pathOutcomeReason,
	inBlockResult interprocPathResult,
	originKey string,
	ubvMode string,
	defBlock *gocfg.Block,
) {
	ubvMsg := useBeforeValidateInconclusiveMessage(ac.target.displayName, ac.typeName)
	ubvID := scope.findingID(
		pass,
		CategoryUseBeforeValidateInconclusive,
		ac.typeName,
		"ubv-inconclusive",
		"same-block",
		originKey,
		ac.target.key(),
		string(inBlockReason),
	)
	meta := cfgOutcomeMetaWithWitness(
		cfgBackend,
		effectiveBudget.maxStates,
		effectiveBudget.maxDepth,
		inBlockReason,
		[]int32{defBlock.Index},
		cfgWitnessMaxSteps,
	)
	meta["ubv_mode"] = ubvMode
	meta["ubv_scope"] = "same-block"
	meta["witness_cast_pos"] = originKey
	meta["witness_def_block"] = strconv.FormatInt(int64(defBlock.Index), 10)
	addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
	meta = appendPhaseCMeta(meta, inBlockResult)
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass,
		bl,
		cfgInconclusivePolicy,
		ac.pos.Pos(),
		CategoryUseBeforeValidateInconclusive,
		ubvID,
		ubvMsg,
		meta,
	)
}

func refineUBVCrossBlockResult(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	refiner cfgRefinementController,
	solver interprocSolver,
	cfg *gocfg.CFG,
	ac cfaAssignedCast,
	crossInput interprocUBVCrossBlockInput,
	crossResult interprocPathResult,
	pathAnchors map[string]string,
) (string, interprocPathResult) {
	crossFindingID := scope.findingID(
		pass,
		CategoryUseBeforeValidateCrossBlock,
		ac.typeName,
		"ubv-xblock",
		crossInput.OriginKey,
		ac.target.key(),
	)
	crossResult = refiner.Refine(cfgRefinementRequest{
		Pass:          pass,
		Position:      ac.pos.Pos(),
		CFG:           cfg,
		Result:        crossResult,
		Category:      CategoryUseBeforeValidateCrossBlock,
		FindingID:     crossFindingID,
		CallChain:     scope.callChain,
		OriginAnchors: pathAnchors,
		SyntheticPath: []int32{crossInput.DefBlock.Index},
		Rerun: func(override cfgRefinementOverride) interprocPathResult {
			next := crossInput
			if override.MaxStates > 0 {
				next.MaxStates = override.MaxStates
			}
			if override.MaxDepth > 0 {
				next.MaxDepth = override.MaxDepth
			}
			next.DischargedWitnesses = override.DischargedWitnesses
			if override.ResolveTargets {
				next.ResolveCFGCalls = true
			}
			if override.RefineRecursion {
				next.SummaryStack = summaryStackWithRecursionFallback(next.SummaryStack)
			}
			refined := solver.EvaluateUBVCrossBlock(next)
			if override.ResolveTargets &&
				refined.Class == interprocOutcomeInconclusive &&
				refined.Reason == pathOutcomeReasonUnresolvedTarget {
				refined = mergeResolvedTargetRefinement(refined, solver.EvaluateUBVCrossBlockLegacy(next))
			}
			return refined
		},
	})
	writeRefinementTraceToSink(pass, ac.pos.Pos(), crossResult)
	return crossFindingID, crossResult
}

func reportCrossBlockUBVUnsafe(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	bl *BaselineConfig,
	cfgWitnessMaxSteps int,
	ac cfaAssignedCast,
	crossResult interprocPathResult,
	ubvWitness []int32,
	originKey string,
	ubvMode string,
	defBlock *gocfg.Block,
) {
	ubvMsg := useBeforeValidateMessage(ac.target.displayName, ac.typeName, true)
	ubvID := scope.findingID(pass, CategoryUseBeforeValidateCrossBlock, ac.typeName, "ubv-xblock", originKey, ac.target.key())
	meta := map[string]string{
		"ubv_mode":         ubvMode,
		"ubv_scope":        "cross-block",
		"witness_cast_pos": originKey,
		"witness_def_block": strconv.FormatInt(
			int64(defBlock.Index),
			10,
		),
	}
	addCFGWitnessMeta(meta, ubvWitness, cfgWitnessMaxSteps)
	addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
	meta = appendPhaseCMeta(meta, crossResult)
	reportFindingWithMetaIfNotBaselined(
		pass,
		bl,
		ac.pos.Pos(),
		CategoryUseBeforeValidateCrossBlock,
		ubvID,
		ubvMsg,
		meta,
	)
}

func reportCrossBlockUBVInconclusive(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	bl *BaselineConfig,
	cfgBackend string,
	effectiveBudget blockVisitBudget,
	cfgInconclusivePolicy string,
	cfgWitnessMaxSteps int,
	ac cfaAssignedCast,
	ubvReason pathOutcomeReason,
	ubvWitness []int32,
	crossResult interprocPathResult,
	originKey string,
	ubvMode string,
	defBlock *gocfg.Block,
) {
	ubvMsg := useBeforeValidateInconclusiveMessage(ac.target.displayName, ac.typeName)
	ubvID := scope.findingID(
		pass,
		CategoryUseBeforeValidateInconclusive,
		ac.typeName,
		"ubv-inconclusive",
		"cross-block",
		originKey,
		ac.target.key(),
		string(ubvReason),
	)
	meta := cfgOutcomeMetaWithWitness(
		cfgBackend,
		effectiveBudget.maxStates,
		effectiveBudget.maxDepth,
		ubvReason,
		ubvWitness,
		cfgWitnessMaxSteps,
	)
	meta["ubv_mode"] = ubvMode
	meta["ubv_scope"] = "cross-block"
	meta["witness_cast_pos"] = originKey
	meta["witness_def_block"] = strconv.FormatInt(int64(defBlock.Index), 10)
	addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
	meta = appendPhaseCMeta(meta, crossResult)
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass,
		bl,
		cfgInconclusivePolicy,
		ac.pos.Pos(),
		CategoryUseBeforeValidateInconclusive,
		ubvID,
		ubvMsg,
		meta,
	)
}

func reportAssignedCastInconclusive(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	bl *BaselineConfig,
	cfgBackend string,
	effectiveBudget blockVisitBudget,
	cfgInconclusivePolicy string,
	cfgWitnessMaxSteps int,
	ac cfaAssignedCast,
	pathReason pathOutcomeReason,
	pathWitness []int32,
	pathResult interprocPathResult,
	originKey string,
	defBlock *gocfg.Block,
) {
	msg := unvalidatedCastInconclusiveMessage(ac.typeName)
	findingID := scope.findingID(
		pass,
		CategoryUnvalidatedCastInconclusive,
		ac.typeName,
		"assigned",
		"inconclusive",
		string(pathReason),
		originKey,
		ac.target.key(),
	)
	meta := cfgOutcomeMetaWithWitness(
		cfgBackend,
		effectiveBudget.maxStates,
		effectiveBudget.maxDepth,
		pathReason,
		pathWitness,
		cfgWitnessMaxSteps,
	)
	meta["witness_cast_pos"] = originKey
	meta["witness_def_block"] = strconv.FormatInt(int64(defBlock.Index), 10)
	addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
	meta = appendPhaseCMeta(meta, pathResult)
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass,
		bl,
		cfgInconclusivePolicy,
		ac.pos.Pos(),
		CategoryUnvalidatedCastInconclusive,
		findingID,
		msg,
		meta,
	)
}
