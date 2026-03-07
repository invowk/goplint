// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

func constructorReturnPathOutcomeWithWitness(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	returnTypeName string,
	returnTypePkgPath string,
	returnTypeKey string,
	cfgBackend string,
	cfgMaxStates int,
	cfgMaxDepth int,
	summaryStack map[string]bool,
) (pathOutcome, pathOutcomeReason, []int32) {
	funcCFG := buildFuncCFGForBackend(pass, fn.Body, cfgBackend)
	if funcCFG == nil || len(funcCFG.Blocks) == 0 {
		return pathOutcomeSafe, pathOutcomeReasonNone, nil
	}
	noReturnAliases := collectNoReturnFuncAliasEvents(pass, fn.Body)
	parentMap := buildParentMap(fn.Body)
	_, _, closureCalls, methodValueCalls := collectCFACasts(
		pass,
		fn.Body,
		parentMap,
		func(*ast.FuncLit, int) {},
	)
	syncLits := collectSynchronousClosureLits(fn.Body)
	syncCalls := collectSynchronousClosureVarCalls(closureCalls)
	methodCalls := collectMethodValueValidateCallSet(methodValueCalls)
	methodCalls = mergeMethodValueValidateCallSets(
		methodCalls,
		collectCalleeValidatedCalls(pass, fn.Body, stackScopeFromMap(summaryStack)),
	)
	bareReturnIncludesTarget := constructorBareReturnIncludesType(pass, fn, returnTypeKey)
	returnTargetKeys := collectConstructorReturnTargetKeys(pass, fn, returnTypeKey, bareReturnIncludesTarget)
	matcher := constructorReturnTargetMatcher(returnTypeKey, returnTargetKeys)

	// DFS from the entry block (index 0).
	ctx := newCFGTraversalContext(
		cfgTraversalModeConstructorPath,
		returnTypeKey,
		cfgValidationStateNeedsValidate,
		funcCFG,
	)
	seenStates := 0
	budget := adaptiveBlockVisitBudget(
		funcCFG,
		blockVisitBudget{maxStates: cfgMaxStates, maxDepth: cfgMaxDepth},
	)
	return dfsConstructorUnvalidatedOutcome(
		pass,
		funcCFG.Blocks[0:1],
		returnTypeName,
		returnTypePkgPath,
		returnTypeKey,
		matcher,
		bareReturnIncludesTarget,
		ctx,
		cfgVisitAnyPredecessor,
		syncLits,
		syncCalls,
		methodCalls,
		noReturnAliases,
		summaryStack,
		0,
		nil,
		&seenStates,
		budget,
	)
}

func dfsConstructorUnvalidatedOutcome(
	pass *analysis.Pass,
	blocks []*gocfg.Block,
	returnTypeName string,
	returnTypePkgPath string,
	returnTypeKey string,
	matcher validateReceiverMatcher,
	bareReturnIncludesTarget bool,
	ctx *cfgTraversalContext,
	predecessor int32,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	noReturnAliases noReturnAliasSet,
	summaryStack map[string]bool,
	depth int,
	path []int32,
	seenStates *int,
	budget blockVisitBudget,
) (pathOutcome, pathOutcomeReason, []int32) {
	inconclusiveReason := pathOutcomeReasonNone
	checker := func(block *gocfg.Block) bool {
		if blockTerminatesWithoutReturn(pass, block, noReturnAliases) {
			return true
		}
		// Return blocks that do not return the constructor target type
		// (for example, early `return nil, err`) are irrelevant for
		// constructor-validates path checks.
		if len(block.Succs) == 0 && !blockReturnsTargetType(pass, block, returnTypeKey, bareReturnIncludesTarget) {
			return true
		}
		for _, node := range block.Nodes {
			if containsValidateOnReceiver(pass, node, matcher, syncLits, syncCalls, methodCalls) {
				return true
			}
			if validated, reason := nodeUsesCalleeSummaryForType(pass, node, returnTypeKey, summaryStack); validated {
				return true
			} else if reason != pathOutcomeReasonNone {
				inconclusiveReason = reason
			}
			if stmt, ok := node.(ast.Stmt); ok {
				// Consider transitive helper validation for this statement.
				// Wrapping in a one-statement block preserves the existing
				// transitive walker behavior while keeping block-level path
				// sensitivity in the outer CFG DFS.
				stmtBody := &ast.BlockStmt{List: []ast.Stmt{stmt}}
				if bodyCallsValidateTransitive(pass, stmtBody, returnTypeName, returnTypePkgPath, returnTypeKey, nil, 0) {
					return true
				}
			}
		}
		return false
	}
	outcome, reason, witness := dfsUnvalidatedBlocksOutcomeWithWitness(
		blocks,
		predecessor,
		ctx,
		checker,
		depth,
		path,
		seenStates,
		budget,
	)
	if outcome == pathOutcomeUnsafe && inconclusiveReason != pathOutcomeReasonNone {
		return pathOutcomeInconclusive, inconclusiveReason, witness
	}
	return outcome, reason, witness
}

func nodeUsesCalleeSummaryForType(
	pass *analysis.Pass,
	node ast.Node,
	returnTypeKey string,
	summaryStack map[string]bool,
) (bool, pathOutcomeReason) {
	if pass == nil || node == nil || returnTypeKey == "" {
		return false, pathOutcomeReasonNone
	}
	bestReason := pathOutcomeReasonNone
	foundValidated := false
	scope := stackScopeFromMap(summaryStack)
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, candidate := range allCallTargetSlots(call) {
			if !exprReturnsType(pass, candidate.expr, returnTypeKey) {
				continue
			}
			summary, ok, reason := callCalleeSummaryForSlotWithStack(
				pass,
				call,
				candidate.slot,
				scope,
			)
			if ok && summary.AlwaysValidatesTarget && !summary.EscapesTargetBeforeValidate {
				foundValidated = true
				return false
			}
			if !ok && reason == pathOutcomeReasonRecursionCycle && summaryStackHasRecursionFallback(summaryStack) {
				continue
			}
			if !ok && (reason == pathOutcomeReasonRecursionCycle ||
				reason == pathOutcomeReasonStateBudget ||
				reason == pathOutcomeReasonDepthBudget) {
				bestReason = reason
			}
		}
		return true
	})
	if foundValidated {
		return true, pathOutcomeReasonNone
	}
	return false, bestReason
}

func blockReturnsTargetType(pass *analysis.Pass, block *gocfg.Block, returnTypeKey string, bareReturnIncludesTarget bool) bool {
	for _, node := range block.Nodes {
		ret, ok := node.(*ast.ReturnStmt)
		if !ok {
			continue
		}
		if returnStmtReturnsType(pass, ret, returnTypeKey, bareReturnIncludesTarget) {
			return true
		}
	}
	return false
}

func returnStmtReturnsType(pass *analysis.Pass, ret *ast.ReturnStmt, returnTypeKey string, bareReturnIncludesTarget bool) bool {
	if ret == nil {
		return false
	}
	if len(ret.Results) == 0 {
		return bareReturnIncludesTarget
	}
	for _, expr := range ret.Results {
		if exprReturnsType(pass, expr, returnTypeKey) {
			return true
		}
	}
	return false
}

func constructorBareReturnIncludesType(pass *analysis.Pass, fn *ast.FuncDecl, returnTypeKey string) bool {
	if pass == nil || pass.TypesInfo == nil || fn == nil {
		return false
	}
	obj := pass.TypesInfo.Defs[fn.Name]
	if obj == nil {
		return false
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Results() == nil || sig.Results().Len() == 0 {
		return false
	}

	hasNamedResults := false
	for resultVar := range sig.Results().Variables() {
		if resultVar.Name() != "" {
			hasNamedResults = true
		}
		if typeIdentityKey(resultVar.Type()) == returnTypeKey && hasNamedResults {
			return true
		}
	}
	return false
}

func constructorReturnTargetMatcher(returnTypeKey string, returnTargetKeys map[string]bool) validateReceiverMatcher {
	return func(pass *analysis.Pass, expr ast.Expr) bool {
		if pass == nil || pass.TypesInfo == nil || expr == nil {
			return false
		}
		receiverType := pass.TypesInfo.TypeOf(expr)
		if receiverType == nil || typeIdentityKey(receiverType) != returnTypeKey {
			return false
		}
		if len(returnTargetKeys) == 0 {
			return true
		}
		key := targetKeyForExpr(pass, expr)
		return key != "" && returnTargetKeys[key]
	}
}

func collectConstructorReturnTargetKeys(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	returnTypeKey string,
	bareReturnIncludesTarget bool,
) map[string]bool {
	if pass == nil || pass.TypesInfo == nil || fn == nil || fn.Body == nil {
		return nil
	}
	keys := make(map[string]bool)
	edges := make(map[string]map[string]bool)

	if bareReturnIncludesTarget {
		obj := pass.TypesInfo.Defs[fn.Name]
		if fnObj, ok := obj.(*types.Func); ok {
			if sig, sigOK := fnObj.Type().(*types.Signature); sigOK && sig.Results() != nil {
				for resultVar := range sig.Results().Variables() {
					if resultVar.Name() == "" {
						continue
					}
					if typeIdentityKey(resultVar.Type()) == returnTypeKey {
						keys[objectKey(resultVar)] = true
					}
				}
			}
		}
	}

	addEdge := func(a, b string) {
		if !isReferenceTargetKey(a) || !isReferenceTargetKey(b) || a == b {
			return
		}
		if edges[a] == nil {
			edges[a] = make(map[string]bool)
		}
		if edges[b] == nil {
			edges[b] = make(map[string]bool)
		}
		edges[a][b] = true
		edges[b][a] = true
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.ReturnStmt:
			for _, expr := range node.Results {
				if !exprReturnsType(pass, expr, returnTypeKey) {
					continue
				}
				if key := targetKeyForExpr(pass, expr); isReferenceTargetKey(key) {
					keys[key] = true
				}
			}
		case *ast.AssignStmt:
			for i, rhs := range node.Rhs {
				if i >= len(node.Lhs) {
					break
				}
				addEdge(targetKeyForExpr(pass, node.Lhs[i]), targetKeyForExpr(pass, rhs))
			}
		case *ast.ValueSpec:
			for i, rhs := range node.Values {
				if i >= len(node.Names) {
					break
				}
				addEdge(targetKeyForExpr(pass, node.Names[i]), targetKeyForExpr(pass, rhs))
			}
		}
		return true
	})

	if len(keys) == 0 {
		return nil
	}
	queue := make([]string, 0, len(keys))
	for key := range keys {
		queue = append(queue, key)
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for next := range edges[current] {
			if keys[next] {
				continue
			}
			keys[next] = true
			queue = append(queue, next)
		}
	}
	return keys
}

func isReferenceTargetKey(key string) bool {
	return key != "" && !strings.HasPrefix(key, "expr:")
}

func helperBodyAlwaysValidatesType(pass *analysis.Pass, body *ast.BlockStmt, returnTypeKey string) bool {
	if body == nil {
		return false
	}
	cfg := buildFuncCFGForPass(pass, body)
	if cfg == nil || len(cfg.Blocks) == 0 {
		return false
	}
	parentMap := buildParentMap(body)
	_, _, closureCalls, methodValueCalls := collectCFACasts(
		pass,
		body,
		parentMap,
		func(*ast.FuncLit, int) {},
	)
	syncLits := collectSynchronousClosureLits(body)
	syncCalls := collectSynchronousClosureVarCalls(closureCalls)
	methodCalls := collectMethodValueValidateCallSet(methodValueCalls)
	noReturnAliases := collectNoReturnFuncAliasEvents(pass, body)
	matcher := typeKeyMatcher(returnTypeKey)
	checker := func(block *gocfg.Block) bool {
		if blockTerminatesWithoutReturn(pass, block, noReturnAliases) {
			return true
		}
		for _, node := range block.Nodes {
			if containsValidateOnReceiver(pass, node, matcher, syncLits, syncCalls, methodCalls) {
				return true
			}
		}
		return false
	}
	return !dfsUnvalidatedBlocks(cfg.Blocks[0:1], nil, checker)
}

func exprReturnsType(pass *analysis.Pass, expr ast.Expr, returnTypeKey string) bool {
	if pass == nil || pass.TypesInfo == nil || expr == nil {
		return false
	}
	exprType := pass.TypesInfo.TypeOf(expr)
	if exprType == nil {
		return false
	}
	exprType = types.Unalias(exprType)
	if tuple, ok := exprType.(*types.Tuple); ok {
		for variable := range tuple.Variables() {
			if typeIdentityKey(variable.Type()) == returnTypeKey {
				return true
			}
		}
		return false
	}
	return typeIdentityKey(exprType) == returnTypeKey
}
