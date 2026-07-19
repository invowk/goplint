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
		Control:       solver.control,
		Rerun: func(override cfgRefinementOverride) interprocPathResult {
			next := castInput
			if override.MaxStates > 0 {
				next.MaxStates = override.MaxStates
			}
			next.DischargedWitnesses = override.DischargedWitnesses
			next.AllowSafe = override.AllowSafe
			if override.ResolveTargets {
				next.ResolveCFGCalls = true
			}
			return solver.EvaluateCastPath(next)
		},
	})
	writeRefinementTraceToSink(pass, ac.pos.Pos(), pathResult)
	return pathFindingID, pathResult
}

type ubvFindingScope string

const (
	ubvFindingScopeSameBlock  ubvFindingScope = "same-block"
	ubvFindingScopeCrossBlock ubvFindingScope = "cross-block"
)

func classifyUBVFindingScope(result interprocPathResult, input interprocUBVCrossBlockInput) ubvFindingScope {
	if input.DefBlock == nil {
		return ubvFindingScopeCrossBlock
	}
	rootFunction := "cfg.ubv." + input.OriginKey
	if rootFunction == "cfg.ubv." {
		rootFunction = "cfg.ubv"
	}
	hazardNode := result.WitnessTerminal
	for _, edge := range result.WitnessEdges {
		if edge.StateBefore == ideStateNeedsValidate &&
			(edge.StateAfter == ideStateEscapedBeforeValidate || edge.StateAfter == ideStateConsumedBeforeValidate) {
			hazardNode = edge.From
			break
		}
	}
	if hazardNode.FuncKey == rootFunction && hazardNode.BlockIndex == input.DefBlock.Index {
		return ubvFindingScopeSameBlock
	}
	return ubvFindingScopeCrossBlock
}

func ubvRefinementIdentity(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	ac cfaAssignedCast,
	input interprocUBVCrossBlockInput,
	result interprocPathResult,
) (string, string) {
	if classifyUBVFindingScope(result, input) == ubvFindingScopeSameBlock {
		return CategoryUseBeforeValidateSameBlock, scope.findingID(
			pass,
			CategoryUseBeforeValidateSameBlock,
			ac.typeName,
			"ubv",
			input.OriginKey,
		)
	}
	return CategoryUseBeforeValidateCrossBlock, scope.findingID(
		pass,
		CategoryUseBeforeValidateCrossBlock,
		ac.typeName,
		"ubv-xblock",
		input.OriginKey,
	)
}

func reportSameBlockUBVUnsafe(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	bl *BaselineConfig,
	ac cfaAssignedCast,
	inBlockResult interprocPathResult,
	originKey string,
	defBlock *gocfg.Block,
) {
	ubvMsg := useBeforeValidateMessage(ac.target.displayName, ac.typeName, false)
	ubvID := scope.findingID(pass, CategoryUseBeforeValidateSameBlock, ac.typeName, "ubv", originKey)
	meta := appendProtocolRefinementMeta(map[string]string{
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
	effectiveBudget blockVisitBudget,
	cfgWitnessMaxSteps int,
	ac cfaAssignedCast,
	inBlockReason pathOutcomeReason,
	inBlockResult interprocPathResult,
	originKey string,
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
		string(inBlockReason),
	)
	meta := cfgOutcomeMetaWithWitness(
		effectiveBudget.maxStates,
		inBlockReason,
		[]int32{defBlock.Index},
		cfgWitnessMaxSteps,
	)
	meta["ubv_scope"] = "same-block"
	meta["witness_cast_pos"] = originKey
	meta["witness_def_block"] = strconv.FormatInt(int64(defBlock.Index), 10)
	addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
	meta = appendProtocolRefinementMeta(meta, inBlockResult)
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass,
		bl,
		ac.pos.Pos(),
		CategoryUseBeforeValidateInconclusive,
		ubvID,
		ubvMsg,
		meta,
	)
}

func refineUBVResult(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	refiner cfgRefinementController,
	solver interprocSolver,
	cfg *gocfg.CFG,
	ac cfaAssignedCast,
	input interprocUBVCrossBlockInput,
	result interprocPathResult,
	pathAnchors map[string]string,
) (string, interprocPathResult) {
	category, findingID := ubvRefinementIdentity(pass, scope, ac, input, result)
	result = refiner.Refine(cfgRefinementRequest{
		Pass:          pass,
		Position:      ac.pos.Pos(),
		CFG:           cfg,
		Result:        result,
		Category:      category,
		FindingID:     findingID,
		CallChain:     scope.callChain,
		OriginAnchors: pathAnchors,
		SyntheticPath: []int32{input.DefBlock.Index},
		Control:       solver.control,
		WitnessIdentity: func(candidate interprocPathResult) cfgRefinementWitnessIdentity {
			candidateCategory, candidateFindingID := ubvRefinementIdentity(pass, scope, ac, input, candidate)
			return cfgRefinementWitnessIdentity{
				Category:      candidateCategory,
				FindingID:     candidateFindingID,
				OriginAnchors: pathAnchors,
				SyntheticPath: []int32{input.DefBlock.Index},
			}
		},
		Rerun: func(override cfgRefinementOverride) interprocPathResult {
			next := input
			if override.MaxStates > 0 {
				next.MaxStates = override.MaxStates
			}
			next.DischargedWitnesses = override.DischargedWitnesses
			if override.ResolveTargets {
				next.ResolveCFGCalls = true
			}
			return solver.EvaluateUBVCrossBlock(next)
		},
	})
	writeRefinementTraceToSink(pass, ac.pos.Pos(), result)
	_, findingID = ubvRefinementIdentity(pass, scope, ac, input, result)
	return findingID, result
}

func reportUBVResult(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	bl *BaselineConfig,
	effectiveBudget blockVisitBudget,
	cfgWitnessMaxSteps int,
	ac cfaAssignedCast,
	result interprocPathResult,
	input interprocUBVCrossBlockInput,
) {
	resultScope := classifyUBVFindingScope(result, input)
	switch result.toPathOutcome() {
	case pathOutcomeUnsafe:
		if resultScope == ubvFindingScopeSameBlock {
			reportSameBlockUBVUnsafe(pass, scope, bl, ac, result, input.OriginKey, input.DefBlock)
			return
		}
		reportCrossBlockUBVUnsafe(
			pass,
			scope,
			bl,
			cfgWitnessMaxSteps,
			ac,
			result,
			result.Witness,
			input.OriginKey,
			input.DefBlock,
		)
	case pathOutcomeInconclusive:
		if resultScope == ubvFindingScopeSameBlock {
			reportSameBlockUBVInconclusive(
				pass,
				scope,
				bl,
				effectiveBudget,
				cfgWitnessMaxSteps,
				ac,
				result.Reason,
				result,
				input.OriginKey,
				input.DefBlock,
			)
			return
		}
		reportCrossBlockUBVInconclusive(
			pass,
			scope,
			bl,
			effectiveBudget,
			cfgWitnessMaxSteps,
			ac,
			result.Reason,
			result.Witness,
			result,
			input.OriginKey,
			input.DefBlock,
		)
	case pathOutcomeSafe:
	}
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
	defBlock *gocfg.Block,
) {
	ubvMsg := useBeforeValidateMessage(ac.target.displayName, ac.typeName, true)
	ubvID := scope.findingID(pass, CategoryUseBeforeValidateCrossBlock, ac.typeName, "ubv-xblock", originKey)
	meta := map[string]string{
		"ubv_scope":        "cross-block",
		"witness_cast_pos": originKey,
		"witness_def_block": strconv.FormatInt(
			int64(defBlock.Index),
			10,
		),
	}
	addCFGWitnessMeta(meta, ubvWitness, cfgWitnessMaxSteps)
	addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
	meta = appendProtocolRefinementMeta(meta, crossResult)
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
	effectiveBudget blockVisitBudget,
	cfgWitnessMaxSteps int,
	ac cfaAssignedCast,
	ubvReason pathOutcomeReason,
	ubvWitness []int32,
	crossResult interprocPathResult,
	originKey string,
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
		string(ubvReason),
	)
	meta := cfgOutcomeMetaWithWitness(
		effectiveBudget.maxStates,
		ubvReason,
		ubvWitness,
		cfgWitnessMaxSteps,
	)
	meta["ubv_scope"] = "cross-block"
	meta["witness_cast_pos"] = originKey
	meta["witness_def_block"] = strconv.FormatInt(int64(defBlock.Index), 10)
	addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
	meta = appendProtocolRefinementMeta(meta, crossResult)
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass,
		bl,
		ac.pos.Pos(),
		CategoryUseBeforeValidateInconclusive,
		ubvID,
		ubvMsg,
		meta,
	)
}

func reportSSAUnavailableUBVInconclusive(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	bl *BaselineConfig,
	effectiveBudget blockVisitBudget,
	cfgWitnessMaxSteps int,
	ac cfaAssignedCast,
	pathResult interprocPathResult,
	originKey string,
	defBlock *gocfg.Block,
) {
	ubvMsg := useBeforeValidateInconclusiveMessage(ac.target.displayName, ac.typeName)
	ubvID := scope.findingID(
		pass,
		CategoryUseBeforeValidateInconclusive,
		ac.typeName,
		"ubv-inconclusive",
		"ssa-unavailable",
		originKey,
		string(pathResult.Reason),
		string(pathResult.SSAAvailability.Status),
	)
	meta := cfgOutcomeMetaWithWitness(
		effectiveBudget.maxStates,
		pathResult.Reason,
		pathResult.Witness,
		cfgWitnessMaxSteps,
	)
	meta["ubv_scope"] = "ssa-unavailable"
	meta["witness_cast_pos"] = originKey
	meta["witness_def_block"] = strconv.FormatInt(int64(defBlock.Index), 10)
	addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
	meta = appendProtocolRefinementMeta(meta, pathResult)
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass,
		bl,
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
	effectiveBudget blockVisitBudget,
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
	)
	meta := cfgOutcomeMetaWithWitness(
		effectiveBudget.maxStates,
		pathReason,
		pathWitness,
		cfgWitnessMaxSteps,
	)
	meta["witness_cast_pos"] = originKey
	meta["witness_def_block"] = strconv.FormatInt(int64(defBlock.Index), 10)
	addCFGWitnessCallChainMeta(meta, scope.callChain, cfgWitnessMaxSteps)
	meta = appendProtocolRefinementMeta(meta, pathResult)
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass,
		bl,
		ac.pos.Pos(),
		CategoryUnvalidatedCastInconclusive,
		findingID,
		msg,
		meta,
	)
}

func reportUnassignedCastInconclusive(
	pass *analysis.Pass,
	scope cfaCastFindingScope,
	bl *BaselineConfig,
	effectiveBudget blockVisitBudget,
	uc cfaUnassignedCast,
) {
	reason := pathOutcomeReasonUnsupportedInstr
	originKey := semanticNodeKey(pass, uc.pos.Pos())
	findingID := scope.findingID(
		pass,
		CategoryUnvalidatedCastInconclusive,
		uc.typeName,
		"unassigned",
		"inconclusive",
		string(reason),
		originKey,
	)
	meta := cfgOutcomeMeta(effectiveBudget.maxStates, reason)
	meta["witness_cast_pos"] = originKey
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass,
		bl,
		uc.pos.Pos(),
		CategoryUnvalidatedCastInconclusive,
		findingID,
		unvalidatedCastInconclusiveMessage(uc.typeName),
		meta,
	)
}
