// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

const protocolValidationDirectProvenance = "direct-ssa"

const protocolValidationSummaryProvenance = "summary-fact"

type protocolValidationProgram struct {
	invocationsByResult map[string][]protocolValidationInvocation
	invocationsByCall   map[token.Pos][]protocolValidationInvocation
	directCallPositions map[token.Pos]bool
	ssaValues           cfgSSAValueIndex
}

func buildProtocolValidationProgram(
	pass *analysis.Pass,
	ssaRes *ssaResult,
	methodCalls methodValueValidateCallSet,
) protocolValidationProgram {
	if pass == nil || ssaRes == nil || !ssaRes.availability().ready() || ssaRes.Pkg == nil || ssaRes.Pkg.Prog == nil {
		return newProtocolValidationProgram()
	}
	ssaRes.validationOnce.Do(func() {
		ssaRes.validationBase = buildProtocolValidationBase(pass, ssaRes)
	})
	program := cloneProtocolValidationProgram(ssaRes.validationBase)
	if len(methodCalls) == 0 {
		return program
	}

	callEvents := buildProtocolCallEventIndex(pass, ssaRes)
	interners := make(map[*ssa.Function]*protocolIdentityInterner)
	for call, validation := range methodCalls {
		receiverExpr := validation.receiver
		if call == nil || receiverExpr == nil || program.directCallPositions[call.Pos()] {
			continue
		}
		callEvent, mapped := callEvents.eventForCall(call)
		ssaCall := callEvent.Instruction
		if !mapped || ssaCall == nil || ssaCall.Parent() == nil {
			continue
		}
		receiverValue, receiverOK := cfgSSAValueForExpr(pass, program.ssaValues, receiverExpr)
		if !receiverOK || receiverValue == nil {
			continue
		}
		errorResult := protocolSSAErrorResult(ssaCall)
		if errorResult == nil && !validation.onSuccessfulReturn {
			continue
		}
		interner := interners[ssaCall.Parent()]
		if interner == nil {
			interner = newProtocolIdentityInterner()
			interners[ssaCall.Parent()] = interner
		}
		provenance := protocolValidationSummaryProvenance
		if fn := calledFunctionObject(pass, call); fn != nil {
			provenance += ":" + objectKey(fn)
		}
		invocation := protocolValidationInvocation{
			Call:               ssaCall,
			Receiver:           receiverValue,
			Result:             errorResult,
			OnSuccessfulReturn: validation.onSuccessfulReturn,
			Relation: protocolValidationRelation{
				ReceiverIdentity: interner.internValue(receiverValue),
				ErrorIdentity:    interner.internValue(errorResult),
			},
			CallSite:          fmt.Sprintf("%s@%d", protocolProcedureKey(ssaCall.Parent()), protocolCallInstructionPosition(ssaCall)),
			SummaryProvenance: provenance,
			ReceiverExpr:      receiverExpr,
		}
		if resultKey := cfgSSAValueKeyOrEmpty(errorResult); resultKey != "" {
			program.invocationsByResult[resultKey] = append(program.invocationsByResult[resultKey], invocation)
		}
		position := protocolCallInstructionPosition(ssaCall)
		program.invocationsByCall[position] = append(program.invocationsByCall[position], invocation)
	}
	return program
}

func newProtocolValidationProgram() protocolValidationProgram {
	return protocolValidationProgram{
		invocationsByResult: make(map[string][]protocolValidationInvocation),
		invocationsByCall:   make(map[token.Pos][]protocolValidationInvocation),
		directCallPositions: make(map[token.Pos]bool),
		ssaValues:           newCFGSSAValueIndex(),
	}
}

func buildProtocolValidationBase(pass *analysis.Pass, ssaRes *ssaResult) protocolValidationProgram {
	program := newProtocolValidationProgram()
	interners := make(map[*ssa.Function]*protocolIdentityInterner)
	for _, fn := range protocolPackageFunctions(ssaRes) {
		interner := newProtocolIdentityInterner()
		interners[fn] = interner
		for _, invocation := range collectProtocolValidationInvocations(fn, interner) {
			position := protocolCallInstructionPosition(invocation.Call)
			program.directCallPositions[position] = true
			invocation.CallSite = fmt.Sprintf("%s@%d", protocolProcedureKey(fn), position)
			invocation.SummaryProvenance = protocolValidationDirectProvenance
			invocation.ReceiverExpr = protocolValidationReceiverExpr(pass, position, nil)
			resultKey := cfgSSAValueKey(invocation.Result)
			if resultKey != "" {
				program.invocationsByResult[resultKey] = append(program.invocationsByResult[resultKey], invocation)
			}
			program.invocationsByCall[position] = append(program.invocationsByCall[position], invocation)
		}
		for _, block := range fn.Blocks {
			for _, instruction := range block.Instrs {
				call, ok := instruction.(ssa.CallInstruction)
				if !ok {
					continue
				}
				for _, invocation := range protocolResultSummaryInvocations(pass, call, interner) {
					program.invocationsByResult[cfgSSAValueKey(invocation.Result)] = append(
						program.invocationsByResult[cfgSSAValueKey(invocation.Result)],
						invocation,
					)
					position := protocolCallInstructionPosition(call)
					program.invocationsByCall[position] = append(program.invocationsByCall[position], invocation)
				}
			}
		}
	}
	program.ssaValues = buildCFGSSAValueIndexFromResult(ssaRes)
	return program
}

func cloneProtocolValidationProgram(base protocolValidationProgram) protocolValidationProgram {
	clone := protocolValidationProgram{
		invocationsByResult: make(map[string][]protocolValidationInvocation, len(base.invocationsByResult)),
		invocationsByCall:   make(map[token.Pos][]protocolValidationInvocation, len(base.invocationsByCall)),
		directCallPositions: base.directCallPositions,
		ssaValues:           base.ssaValues,
	}
	for key, invocations := range base.invocationsByResult {
		clone.invocationsByResult[key] = append([]protocolValidationInvocation(nil), invocations...)
	}
	for position, invocations := range base.invocationsByCall {
		clone.invocationsByCall[position] = append([]protocolValidationInvocation(nil), invocations...)
	}
	return clone
}

func cfgSSAValueKeyOrEmpty(value ssa.Value) string {
	if value == nil {
		return ""
	}
	return cfgSSAValueKey(value)
}

func protocolResultSummaryInvocations(
	pass *analysis.Pass,
	call ssa.CallInstruction,
	interner *protocolIdentityInterner,
) []protocolValidationInvocation {
	if pass == nil || call == nil || call.Common() == nil || interner == nil {
		return nil
	}
	callee := call.Common().StaticCallee()
	if callee == nil || callee.Object() == nil {
		return nil
	}
	function, ok := callee.Object().(*types.Func)
	if !ok {
		return nil
	}
	fact := buildProtocolSummaryFact("", function.Name(), callee)
	if function.Pkg() != nil {
		fact.PackagePath = function.Pkg().Path()
	}
	if function.Pkg() != pass.Pkg {
		fact = ProtocolSummaryFact{}
		if pass.ImportObjectFact == nil || !pass.ImportObjectFact(function, &fact) {
			return nil
		}
	}
	if function.Pkg() == nil || validateProtocolSummaryFact(&fact, function) != 0 {
		return nil
	}
	invocations := make([]protocolValidationInvocation, 0)
	for _, effect := range fact.Effects {
		if effect.Kind != protocolSummaryEffectValidate ||
			effect.TargetKind != protocolSummaryTargetResult ||
			effect.Condition != protocolSummaryConditionResultNil {
			continue
		}
		receiver := protocolSSAResultAtSlot(call, effect.TargetSlot)
		errorResult := protocolSSAResultAtSlot(call, effect.ConditionResultSlot)
		if receiver == nil || errorResult == nil {
			continue
		}
		invocations = append(invocations, protocolValidationInvocation{
			Call:     call,
			Receiver: receiver,
			Result:   errorResult,
			Relation: protocolValidationRelation{
				ReceiverIdentity: interner.internValue(receiver),
				ErrorIdentity:    interner.internValue(errorResult),
			},
			CallSite:          fmt.Sprintf("%s@%d", protocolProcedureKey(call.Parent()), protocolCallInstructionPosition(call)),
			SummaryProvenance: protocolValidationSummaryProvenance + ":result:" + objectKey(function),
		})
	}
	return invocations
}

func protocolSSAResultAtSlot(call ssa.CallInstruction, slot int) ssa.Value {
	if call == nil || call.Common() == nil || call.Parent() == nil || slot < 0 {
		return nil
	}
	signature := call.Common().Signature()
	if signature == nil || signature.Results() == nil || slot >= signature.Results().Len() {
		return nil
	}
	callValue, ok := call.(ssa.Value)
	if !ok {
		return nil
	}
	if signature.Results().Len() == 1 {
		return callValue
	}
	for _, block := range call.Parent().Blocks {
		for _, instruction := range block.Instrs {
			extract, extractOK := instruction.(*ssa.Extract)
			if extractOK && extract.Tuple == callValue && extract.Index == slot {
				return extract
			}
		}
	}
	return nil
}

func (program protocolValidationProgram) returnPropagatesTargetValidationError(
	pass *analysis.Pass,
	ret *ast.ReturnStmt,
	target castTarget,
) bool {
	if ret == nil || program.ssaValues.empty() {
		return false
	}
	for _, resultExpression := range ret.Results {
		if pass == nil || pass.TypesInfo == nil || !isErrorType(pass.TypesInfo.TypeOf(resultExpression)) {
			continue
		}
		returnedValue, ok := cfgSSAValueForExpr(pass, program.ssaValues, resultExpression)
		if !ok || returnedValue == nil {
			continue
		}
		for _, invocations := range program.invocationsByCall {
			for _, invocation := range invocations {
				if protocolInvocationTargetResolution(pass, target, invocation) != protocolAliasMust {
					continue
				}
				if protocolValueDerivedFrom(returnedValue, invocation.Result, make(map[ssa.Value]bool)) {
					return true
				}
			}
		}
	}
	return false
}

func (program protocolValidationProgram) nodeHasTargetInvocation(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
) bool {
	return program.nodeTargetInvocationResolution(pass, node, target) != protocolAliasUnknown
}

func (program protocolValidationProgram) nodeTargetInvocationResolution(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
) protocolAliasResolution {
	if node == nil || len(program.invocationsByCall) == 0 {
		return protocolAliasUnknown
	}
	resolution := protocolAliasUnknown
	calls := protocolOrderedCallsInNode(node)
	if call, exact := node.(*ast.CallExpr); exact {
		calls = []*ast.CallExpr{call}
	}
	for _, call := range calls {
		for _, invocation := range program.invocationsByCall[call.Lparen] {
			switch protocolInvocationTargetResolution(pass, target, invocation) {
			case protocolAliasAmbiguous:
				return protocolAliasAmbiguous
			case protocolAliasMust:
				resolution = protocolAliasMust
			case protocolAliasUnknown:
			}
		}
	}
	return resolution
}

func (program protocolValidationProgram) callHasCheckedSuccess(call *ast.CallExpr) bool {
	if call == nil {
		return false
	}
	edgeEffects := make(map[*ssa.Function][]protocolValidationEdgeEffect)
	for _, invocation := range program.invocationsByCall[call.Lparen] {
		if invocation.Call == nil || invocation.Call.Parent() == nil {
			continue
		}
		function := invocation.Call.Parent()
		effects, ok := edgeEffects[function]
		if !ok {
			functionInvocations := program.functionInvocations(function)
			effects = collectProtocolValidationEdgeEffects(function, functionInvocations)
			edgeEffects[function] = effects
		}
		if invocation.OnSuccessfulReturn || protocolInvocationHasCheckedSuccess(function, invocation, effects) {
			return true
		}
	}
	return false
}

func (program protocolValidationProgram) nodeTargetSuccessfulReturnResolution(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
) protocolAliasResolution {
	if node == nil || len(program.invocationsByCall) == 0 {
		return protocolAliasUnknown
	}
	resolution := protocolAliasUnknown
	calls := protocolOrderedCallsInNode(node)
	if call, exact := node.(*ast.CallExpr); exact {
		calls = []*ast.CallExpr{call}
	}
	for _, call := range calls {
		for _, invocation := range program.invocationsByCall[call.Lparen] {
			if !invocation.OnSuccessfulReturn {
				continue
			}
			switch protocolInvocationTargetResolution(pass, target, invocation) {
			case protocolAliasAmbiguous:
				return protocolAliasAmbiguous
			case protocolAliasMust:
				resolution = protocolAliasMust
			case protocolAliasUnknown:
			}
		}
	}
	return resolution
}

func (program protocolValidationProgram) functionInvocations(function *ssa.Function) []protocolValidationInvocation {
	if function == nil {
		return nil
	}
	var invocations []protocolValidationInvocation
	for _, candidates := range program.invocationsByCall {
		for _, candidate := range candidates {
			if candidate.Call != nil && candidate.Call.Parent() == function {
				invocations = append(invocations, candidate)
			}
		}
	}
	return invocations
}

func (program protocolValidationProgram) firstTargetValidationPosition(
	pass *analysis.Pass,
	target castTarget,
) token.Pos {
	positions := make([]token.Pos, 0, len(program.invocationsByCall))
	for position := range program.invocationsByCall {
		positions = append(positions, position)
	}
	slices.Sort(positions)
	for _, position := range positions {
		for _, invocation := range program.invocationsByCall[position] {
			if protocolInvocationTargetResolution(pass, target, invocation) == protocolAliasMust {
				return position
			}
		}
	}
	return token.NoPos
}

func protocolInvocationTargetResolution(
	pass *analysis.Pass,
	target castTarget,
	invocation protocolValidationInvocation,
) protocolAliasResolution {
	if target.typeKey != "" && invocation.Receiver != nil &&
		typeIdentityKey(invocation.Receiver.Type()) != target.typeKey {
		return protocolAliasUnknown
	}
	if target.flowAliases != nil && invocation.Call != nil && invocation.Receiver != nil {
		resolution := target.flowAliases.resolutionAt(invocation.Call, invocation.Receiver)
		if resolution == protocolAliasUnknown && invocation.ReceiverExpr != nil {
			resolution = target.flowAliases.capturedResolutionAt(pass, invocation.Call, invocation.ReceiverExpr)
		}
		if resolution == protocolAliasUnknown && target.dynamicIndexBase != "" &&
			invocation.ReceiverExpr != nil &&
			dynamicIndexCandidateMayAlias(pass, invocation.ReceiverExpr, target.dynamicIndexBase) {
			return protocolAliasAmbiguous
		}
		return resolution
	}
	if invocation.ReceiverExpr == nil {
		return protocolAliasUnknown
	}
	return target.aliasResolution(pass, invocation.ReceiverExpr)
}

func protocolCallInstructionPosition(call ssa.CallInstruction) token.Pos {
	if call == nil || call.Common() == nil {
		return token.NoPos
	}
	return call.Common().Pos()
}

func protocolSSAErrorResult(call ssa.CallInstruction) ssa.Value {
	if call == nil || call.Common() == nil || call.Parent() == nil {
		return nil
	}
	signature := call.Common().Signature()
	if signature == nil || signature.Results() == nil {
		return nil
	}
	errorType := types.Universe.Lookup("error").Type()
	errorSlot := -1
	for index := range signature.Results().Len() {
		if types.Identical(signature.Results().At(index).Type(), errorType) {
			errorSlot = index
			break
		}
	}
	if errorSlot < 0 {
		return nil
	}
	callValue, ok := call.(ssa.Value)
	if !ok {
		return nil
	}
	if signature.Results().Len() == 1 {
		return callValue
	}
	for _, block := range call.Parent().Blocks {
		for _, instruction := range block.Instrs {
			extract, ok := instruction.(*ssa.Extract)
			if ok && extract.Tuple == callValue && extract.Index == errorSlot {
				return extract
			}
		}
	}
	return nil
}

func protocolValidationReceiverExpr(
	pass *analysis.Pass,
	position token.Pos,
	methodCalls methodValueValidateCallSet,
) ast.Expr {
	call := protocolASTCallAtPosition(pass, position)
	if call == nil {
		return nil
	}
	if receiver := methodCalls[call].receiver; receiver != nil {
		return receiver
	}
	selector, ok := stripParens(call.Fun).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != validateMethodName {
		return nil
	}
	return selector.X
}

func protocolASTCallAtPosition(pass *analysis.Pass, position token.Pos) *ast.CallExpr {
	if pass == nil || !position.IsValid() {
		return nil
	}
	for _, file := range pass.Files {
		var exact *ast.CallExpr
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || call.Lparen != position {
				return true
			}
			exact = call
			return false
		})
		if exact != nil {
			return exact
		}
	}
	return nil
}

func (program protocolValidationProgram) edgeResultFacts(edge interprocEdge) []protocolValidationEdgeEffect {
	if len(program.invocationsByResult) == 0 || len(edge.PredicateProvenance) == 0 {
		return nil
	}
	var effects []protocolValidationEdgeEffect
	for _, provenance := range edge.PredicateProvenance {
		formula := cfgSSAFormulaFromPredicateProvenance(provenance)
		if formula.unsupported {
			continue
		}
		resultKeys := make([]string, 0, len(program.invocationsByResult))
		for resultKey := range program.invocationsByResult {
			resultKeys = append(resultKeys, resultKey)
		}
		slices.Sort(resultKeys)
		for _, resultKey := range resultKeys {
			invocations := program.invocationsByResult[resultKey]
			result, proven := protocolResultProvenByFormula(formula, resultKey)
			if !proven {
				continue
			}
			for _, invocation := range invocations {
				effects = append(effects, protocolValidationEdgeEffect{
					Effect:     protocolConditionalEffect{Kind: protocolEffectValidate, Relation: invocation.Relation},
					Result:     result,
					Invocation: invocation,
				})
			}
		}
	}
	return effects
}

func protocolResultProvenByFormula(
	formula cfgSSAConstraintFormula,
	resultKey string,
) (protocolErrorResult, bool) {
	if formula.unsupported || len(formula.alternatives) == 0 || resultKey == "" {
		return protocolErrorResultUnknown, false
	}
	result := protocolErrorResultUnknown
	for _, alternative := range formula.alternatives {
		alternativeResult := protocolErrorResultUnknown
		for _, constraint := range alternative {
			if constraint.subject != resultKey || constraint.value != "<nil>" {
				continue
			}
			switch constraint.op {
			case "eq":
				alternativeResult = protocolErrorResultNil
			case "neq":
				alternativeResult = protocolErrorResultNonNil
			}
		}
		if alternativeResult == protocolErrorResultUnknown {
			return protocolErrorResultUnknown, false
		}
		if result != protocolErrorResultUnknown && result != alternativeResult {
			return protocolErrorResultUnknown, false
		}
		result = alternativeResult
	}
	return result, result != protocolErrorResultUnknown
}

func (program protocolValidationProgram) targetEdgeTransfer(
	pass *analysis.Pass,
	target castTarget,
) interprocEdgeTransferFn {
	return func(edge interprocEdge, state protocolAbstractState) (ideEdgeFuncTag, pathOutcomeReason) {
		if !state.validationRequired() {
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		}
		for _, fact := range program.edgeResultFacts(edge) {
			switch protocolInvocationTargetResolution(pass, target, fact.Invocation) {
			case protocolAliasMust:
				switch fact.Result {
				case protocolErrorResultNil:
					return ideEdgeFuncValidate, pathOutcomeReasonNone
				case protocolErrorResultNonNil:
					return ideEdgeFuncValidationFailed, pathOutcomeReasonNone
				case protocolErrorResultUnknown:
					continue
				}
			case protocolAliasAmbiguous:
				return ideEdgeFuncIdentity, pathOutcomeReasonAmbiguousIdentity
			case protocolAliasUnknown:
				continue
			}
		}
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
}

func (program protocolValidationProgram) targetFailureDischargesObligationOnEdge(
	pass *analysis.Pass,
	parentMap map[ast.Node]ast.Node,
	target castTarget,
	edge interprocEdge,
	successor ast.Node,
) bool {
	for _, fact := range program.edgeResultFacts(edge) {
		if fact.Result != protocolErrorResultNonNil ||
			protocolInvocationTargetResolution(pass, target, fact.Invocation) != protocolAliasMust {
			continue
		}
		if statement, ok := successor.(ast.Stmt); ok &&
			protocolStatementTerminatesProcedure(statement) &&
			!protocolStatementReferencesTarget(pass, statement, target) {
			return true
		}
		if protocolValidationFailureDischargesObligation(pass, parentMap, target, fact.Invocation) {
			return true
		}
	}
	return false
}

func protocolValidationFailureDischargesObligation(
	pass *analysis.Pass,
	parentMap map[ast.Node]ast.Node,
	target castTarget,
	invocation protocolValidationInvocation,
) bool {
	if pass == nil || len(parentMap) == 0 || target.originExpr == nil || invocation.Call == nil {
		return false
	}
	call := protocolASTCallAtPosition(pass, protocolCallInstructionPosition(invocation.Call))
	if call == nil {
		return false
	}
	conditional := protocolValidationGuardForCall(call, parentMap)
	if conditional == nil {
		return false
	}
	failureUsesTrue, relationOK := protocolInvocationFailureBranch(invocation)
	if !relationOK {
		return false
	}
	failureBranch := ast.Stmt(conditional.Body)
	if !failureUsesTrue {
		failureBranch = conditional.Else
	}
	if protocolStatementTerminatesProcedure(failureBranch) &&
		!protocolStatementReferencesTarget(pass, failureBranch, target) {
		return true
	}
	block, ok := failureBranch.(*ast.BlockStmt)
	if !ok || len(block.List) != 1 {
		return false
	}
	branch, ok := block.List[0].(*ast.BranchStmt)
	if !ok || branch.Tok != token.CONTINUE || branch.Label != nil {
		return false
	}
	loopBody := protocolUnlabeledContinueLoopBody(branch, parentMap)
	return loopBody != nil && target.originExpr.Pos() >= loopBody.Pos() && target.originExpr.End() <= loopBody.End()
}

func protocolStatementReferencesTarget(pass *analysis.Pass, statement ast.Stmt, target castTarget) bool {
	if pass == nil || statement == nil {
		return false
	}
	found := false
	ast.Inspect(statement, func(node ast.Node) bool {
		if found {
			return false
		}
		expression, ok := node.(ast.Expr)
		if ok && target.matchesExpr(pass, expression) {
			found = true
			return false
		}
		return true
	})
	return found
}

func protocolInvocationFailureBranch(invocation protocolValidationInvocation) (bool, bool) {
	if invocation.Call == nil || invocation.Call.Parent() == nil {
		return false, false
	}
	for _, effect := range collectProtocolValidationEdgeEffects(
		invocation.Call.Parent(),
		[]protocolValidationInvocation{invocation},
	) {
		if effect.Result != protocolErrorResultNonNil || effect.From == nil || len(effect.From.Succs) != 2 {
			continue
		}
		return effect.To == effect.From.Succs[0], true
	}
	return false, false
}

func protocolStatementTerminatesProcedure(statement ast.Stmt) bool {
	switch typed := statement.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		return len(typed.List) > 0 && protocolStatementTerminatesProcedure(typed.List[len(typed.List)-1])
	case *ast.IfStmt:
		return typed.Else != nil &&
			protocolStatementTerminatesProcedure(typed.Body) &&
			protocolStatementTerminatesProcedure(typed.Else)
	default:
		return false
	}
}

func protocolValidationGuardForCall(
	call *ast.CallExpr,
	parentMap map[ast.Node]ast.Node,
) *ast.IfStmt {
	for current := ast.Node(call); current != nil; current = parentMap[current] {
		conditional, ok := current.(*ast.IfStmt)
		if !ok || conditional.Init == nil {
			continue
		}
		if isDescendantOrSelf(call, conditional.Init, parentMap) {
			return conditional
		}
	}
	return nil
}

func protocolUnlabeledContinueLoopBody(
	branch *ast.BranchStmt,
	parentMap map[ast.Node]ast.Node,
) *ast.BlockStmt {
	for current := parentMap[branch]; current != nil; current = parentMap[current] {
		switch statement := current.(type) {
		case *ast.RangeStmt:
			return statement.Body
		case *ast.ForStmt:
			return statement.Body
		case *ast.FuncLit:
			return nil
		}
	}
	return nil
}
