// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"sync"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/ssa"
)

type constructorDeferredLocation struct {
	block *gocfg.Block
	index int
}

type constructorDeferredRegistration struct {
	statement *ast.DeferStmt
	location  constructorDeferredLocation
	mapped    bool
}

type protocolDeferredErrorStore struct {
	position         token.Pos
	blockIndex       int
	instructionIndex int
	tag              ideEdgeFuncTag
	reason           pathOutcomeReason
}

type protocolDeferredErrorProgram struct {
	pass              *analysis.Pass
	procedure         protocolProcedure
	errorResult       *types.Var
	errorAddress      ssa.Value
	validationProgram protocolValidationProgram
	methodCalls       methodValueValidateCallSet
	target            castTarget
	stores            []protocolDeferredErrorStore
	errorLoadKeys     map[string]bool
	buildReason       pathOutcomeReason
	continuations     map[string]bool
}

// constructorDeferredPlanner composes deferred procedure summaries at each
// constructor exit. Its implementation is built on the canonical supergraph
// and IFDS tabulator; it does not interpret closure statements independently.
type constructorDeferredPlanner struct {
	pass               *analysis.Pass
	ssa                *ssaResult
	declaration        *ast.FuncDecl
	calleeSummaryCache *sync.Map
	cfg                *gocfg.CFG
	errorResult        *types.Var
	registrations      []constructorDeferredRegistration
	locations          map[ast.Node]constructorDeferredLocation
	dominators         map[int32]map[int32]bool
	procedureIndex     protocolProcedureIndex
}

func newConstructorDeferredPlanner(
	pass *analysis.Pass,
	ssaResult *ssaResult,
	declaration *ast.FuncDecl,
	calleeSummaryCache *sync.Map,
) constructorDeferredPlanner {
	planner := constructorDeferredPlanner{
		pass: pass, ssa: ssaResult, declaration: declaration,
		calleeSummaryCache: calleeSummaryCache,
		locations:          make(map[ast.Node]constructorDeferredLocation),
		procedureIndex:     buildProtocolProcedureIndex(pass, ssaResult),
	}
	if pass == nil || declaration == nil || declaration.Body == nil {
		return planner
	}
	planner.cfg = buildProtocolCFG(pass, declaration.Body, ssaResult)
	planner.errorResult = constructorNamedErrorResult(pass, declaration)
	if planner.cfg == nil {
		return planner
	}
	for _, block := range planner.cfg.Blocks {
		if block == nil {
			continue
		}
		for index, node := range block.Nodes {
			planner.locations[node] = constructorDeferredLocation{block: block, index: index}
		}
	}
	planner.dominators = protocolCFGBlockDominators(planner.cfg)
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		deferred, ok := node.(*ast.DeferStmt)
		if !ok {
			return true
		}
		location, mapped := planner.locations[deferred]
		planner.registrations = append(planner.registrations, constructorDeferredRegistration{
			statement: deferred,
			location:  location,
			mapped:    mapped,
		})
		return true
	})
	return planner
}

func (planner constructorDeferredPlanner) returnEffect(
	returned *ast.ReturnStmt,
	target castTarget,
	initiallyValidated bool,
) (ideEdgeFuncTag, pathOutcomeReason) {
	if returned == nil || planner.errorResult == nil || len(planner.registrations) == 0 {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	returnLocation, mapped := planner.locations[returned]
	if !mapped {
		return constructorDeferredInconclusive(pathOutcomeReasonCallMapping)
	}
	type activeRegistration struct {
		registration constructorDeferredRegistration
		optional     bool
	}
	active := make([]activeRegistration, 0, len(planner.registrations))
	for _, registration := range planner.registrations {
		if !registration.mapped {
			if planner.deferCallRelevant(registration.statement.Call, target) {
				return constructorDeferredInconclusive(pathOutcomeReasonCallMapping)
			}
			continue
		}
		if !planner.registrationMayReachReturn(registration.location, returnLocation) {
			continue
		}
		if planner.registrationCanRepeat(registration.location) &&
			planner.deferCallRelevant(registration.statement.Call, target) {
			return constructorDeferredInconclusive(pathOutcomeReasonFeasibilityUnknown)
		}
		active = append(active, activeRegistration{
			registration: registration,
			optional:     !planner.registrationDominatesReturn(registration.location, returnLocation),
		})
	}
	if len(active) == 0 {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	for left := range active {
		for right := left + 1; right < len(active); right++ {
			if !planner.locationsComparable(active[left].registration.location, active[right].registration.location) {
				return constructorDeferredInconclusive(pathOutcomeReasonFeasibilityUnknown)
			}
		}
	}
	sort.SliceStable(active, func(left, right int) bool {
		return planner.registrationBefore(active[left].registration.location, active[right].registration.location)
	})

	initial := newProtocolRequiredState()
	initial.Result = protocolErrorResultNil
	initial.DeferredError = protocolDeferredErrorNil
	if initiallyValidated {
		initial.Validation = protocolValidationProven
	}
	states := []protocolAbstractState{initial}
	for _, registration := range slices.Backward(active) {
		next := make([]protocolAbstractState, 0, len(states)*2)
		if registration.optional {
			next = append(next, states...)
		}
		for _, state := range states {
			executed, reason := planner.executeRegistration(registration.registration, target, state)
			if reason != pathOutcomeReasonNone {
				return constructorDeferredInconclusive(reason)
			}
			next = append(next, executed...)
		}
		states = deduplicateProtocolDeferredStates(next)
		if len(states) == 0 {
			return constructorDeferredInconclusive(pathOutcomeReasonFeasibilityUnknown)
		}
	}

	allValidated := true
	for _, state := range states {
		if reason := state.pathOutcomeReason(); reason != pathOutcomeReasonNone {
			return constructorDeferredInconclusive(reason)
		}
		switch state.DeferredError {
		case protocolDeferredErrorValidation:
			state.Validation = protocolValidationProven
		case protocolDeferredErrorNil:
		case protocolDeferredErrorUnknown, protocolDeferredErrorOther:
			return constructorDeferredInconclusive(pathOutcomeReasonUnresolvedTarget)
		}
		if !state.validationProven() {
			allValidated = false
		}
	}
	if allValidated {
		return ideEdgeFuncValidate, pathOutcomeReasonNone
	}
	if initiallyValidated {
		return ideEdgeFuncInvalidate, pathOutcomeReasonNone
	}
	return ideEdgeFuncIdentity, pathOutcomeReasonNone
}

func constructorDeferredInconclusive(reason pathOutcomeReason) (ideEdgeFuncTag, pathOutcomeReason) {
	return ideEdgeFuncDeferredErrorUnknown, reason
}

func constructorNamedErrorResult(pass *analysis.Pass, declaration *ast.FuncDecl) *types.Var {
	if pass == nil || pass.TypesInfo == nil || declaration == nil || declaration.Name == nil {
		return nil
	}
	function, ok := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
	if !ok {
		return nil
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Results() == nil {
		return nil
	}
	var result *types.Var
	for variable := range signature.Results().Variables() {
		if variable.Name() == "" || !isErrorType(variable.Type()) {
			continue
		}
		if result != nil {
			return nil
		}
		result = variable
	}
	return result
}

func (planner constructorDeferredPlanner) registrationDominatesReturn(
	registration,
	returned constructorDeferredLocation,
) bool {
	if registration.block == nil || returned.block == nil {
		return false
	}
	if registration.block.Index == returned.block.Index {
		return registration.index < returned.index
	}
	return planner.dominators[returned.block.Index][registration.block.Index]
}

func (planner constructorDeferredPlanner) registrationMayReachReturn(
	registration,
	returned constructorDeferredLocation,
) bool {
	if registration.block == nil || returned.block == nil {
		return false
	}
	if registration.block.Index == returned.block.Index {
		return registration.index < returned.index
	}
	seen := make(map[int32]bool)
	queue := []*gocfg.Block{registration.block}
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if block == nil || seen[block.Index] {
			continue
		}
		seen[block.Index] = true
		if block.Index == returned.block.Index {
			return true
		}
		queue = append(queue, block.Succs...)
	}
	return false
}

func (planner constructorDeferredPlanner) registrationCanRepeat(location constructorDeferredLocation) bool {
	if location.block == nil {
		return false
	}
	seen := make(map[int32]bool)
	queue := append([]*gocfg.Block(nil), location.block.Succs...)
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if block == nil {
			continue
		}
		if block.Index == location.block.Index {
			return true
		}
		if seen[block.Index] {
			continue
		}
		seen[block.Index] = true
		queue = append(queue, block.Succs...)
	}
	return false
}

func (planner constructorDeferredPlanner) registrationBefore(left, right constructorDeferredLocation) bool {
	if left.block == nil || right.block == nil {
		return false
	}
	if left.block.Index == right.block.Index {
		return left.index < right.index
	}
	return planner.dominators[right.block.Index][left.block.Index]
}

func (planner constructorDeferredPlanner) locationsComparable(left, right constructorDeferredLocation) bool {
	return planner.registrationBefore(left, right) || planner.registrationBefore(right, left)
}

func deduplicateProtocolDeferredStates(states []protocolAbstractState) []protocolAbstractState {
	seen := make(map[string]bool)
	result := make([]protocolAbstractState, 0, len(states))
	for _, state := range states {
		key := state.key()
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, state)
	}
	return result
}

func protocolCFGBlockDominators(cfg *gocfg.CFG) map[int32]map[int32]bool {
	result := make(map[int32]map[int32]bool)
	if cfg == nil || len(cfg.Blocks) == 0 {
		return result
	}
	var entryBlock *gocfg.Block
	for _, block := range cfg.Blocks {
		if block != nil {
			entryBlock = block
			break
		}
	}
	if entryBlock == nil {
		return result
	}
	reachable := make(map[int32]bool)
	queue := []*gocfg.Block{entryBlock}
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if block == nil || reachable[block.Index] {
			continue
		}
		reachable[block.Index] = true
		queue = append(queue, block.Succs...)
	}
	blocks := make([]*gocfg.Block, 0, len(cfg.Blocks))
	all := make(map[int32]bool)
	predecessors := make(map[int32][]int32)
	for _, block := range cfg.Blocks {
		if block == nil || !reachable[block.Index] {
			continue
		}
		blocks = append(blocks, block)
		all[block.Index] = true
		for _, successor := range block.Succs {
			if successor != nil && reachable[successor.Index] {
				predecessors[successor.Index] = append(predecessors[successor.Index], block.Index)
			}
		}
	}
	if len(blocks) == 0 {
		return result
	}
	entry := entryBlock.Index
	for _, block := range blocks {
		if block.Index == entry {
			result[block.Index] = map[int32]bool{entry: true}
			continue
		}
		result[block.Index] = cloneConstructorBlockSet(all)
	}
	for changed := true; changed; {
		changed = false
		for _, block := range blocks {
			if block.Index == entry {
				continue
			}
			var next map[int32]bool
			for _, predecessor := range predecessors[block.Index] {
				if next == nil {
					next = cloneConstructorBlockSet(result[predecessor])
					continue
				}
				for candidate := range next {
					if !result[predecessor][candidate] {
						delete(next, candidate)
					}
				}
			}
			if next == nil {
				next = make(map[int32]bool)
			}
			next[block.Index] = true
			if !sameConstructorBlockSet(result[block.Index], next) {
				result[block.Index] = next
				changed = true
			}
		}
	}
	return result
}

func cloneConstructorBlockSet(input map[int32]bool) map[int32]bool {
	result := make(map[int32]bool, len(input))
	for block := range input {
		result[block] = true
	}
	return result
}

func sameConstructorBlockSet(left, right map[int32]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for block := range left {
		if !right[block] {
			return false
		}
	}
	return true
}

func (planner constructorDeferredPlanner) executeRegistration(
	registration constructorDeferredRegistration,
	target castTarget,
	initial protocolAbstractState,
) ([]protocolAbstractState, pathOutcomeReason) {
	if registration.statement == nil || registration.statement.Call == nil {
		return nil, pathOutcomeReasonCallMapping
	}
	call := registration.statement.Call
	var procedure protocolProcedure
	procedureOK := false
	if event, mapped := buildProtocolCallEventIndex(planner.pass, planner.ssa).eventForCall(call); mapped {
		procedure, procedureOK = planner.procedureIndex.resolveCall(event.Instruction)
	}
	if !procedureOK {
		if literal, literalOK := callFuncLit(call); literalOK {
			procedure, procedureOK = planner.procedureIndex.byLiteral[literal]
		}
	}
	if !procedureOK || procedure.Body == nil || procedure.Function == nil {
		if planner.deferCallRelevant(call, target) {
			return nil, unresolvedCallOutcomeReason(planner.pass, call)
		}
		return []protocolAbstractState{initial}, pathOutcomeReasonNone
	}
	if procedure.Literal == nil || len(procedure.Function.Params) != 0 {
		if planner.deferCallRelevant(call, target) {
			return nil, pathOutcomeReasonUnresolvedTarget
		}
		return []protocolAbstractState{initial}, pathOutcomeReasonNone
	}
	if !procedure.CaptureExact {
		return nil, pathOutcomeReasonAmbiguousIdentity
	}

	methodCalls := collectCalleeValidatedCalls(
		planner.pass,
		procedure.Body,
		planner.ssa,
		stackScopeFromMap(nil, planner.ssa),
		planner.calleeSummaryCache,
	)
	validationProgram := buildProtocolValidationProgram(planner.pass, planner.ssa, methodCalls)
	program := buildProtocolDeferredErrorProgram(
		planner.pass,
		procedure,
		planner.errorResult,
		validationProgram,
		methodCalls,
		target,
	)
	if program.buildReason != pathOutcomeReasonNone {
		return nil, program.buildReason
	}
	graph, start, graphOK := buildInterprocSupergraphForProcedure(planner.pass, procedure, planner.ssa)
	if !graphOK {
		return nil, pathOutcomeReasonCallMapping
	}
	program.continuations = protocolDeferredContinuationNodes(graph)
	exits := make([]protocolAbstractState, 0, 2)
	result, _ := runIFDSPropagationWithStatsOptions(
		graph,
		start,
		defaultCFGMaxStates,
		nil,
		nil,
		func(nodeID interprocNodeID, node ast.Node, state protocolAbstractState) (ideEdgeFuncTag, pathOutcomeReason) {
			return program.nodeTransfer(graph, nodeID, node, state, planner.calleeSummaryCache)
		},
		func(interprocNodeID, ast.Node, protocolAbstractState) bool { return false },
		func(nodeID interprocNodeID, node ast.Node, _ protocolAbstractState) bool {
			if event, ok := graph.callEvent(nodeID); ok && protocolCallIsBuiltin(event) {
				return false
			}
			if validationProgram.nodeHasTargetInvocation(planner.pass, node, target) {
				return false
			}
			return graphCallReferencesTarget(graph, nodeID, planner.pass, target) ||
				program.nodeReferencesError(node)
		},
		func(nodeID interprocNodeID, _ ast.Node) bool {
			return nodeID.FuncKey == start.FuncKey && graph.isFunctionExitNode(nodeID)
		},
		interprocSinkPolicy{
			UnresolvedIdentityAtSink:   true,
			MustAliasUncertaintyAtSink: true,
		},
		nil,
		nil,
		interprocTabulationOptions{
			InitialState: &initial,
			ObserveExit: func(_ interprocNodeID, state protocolAbstractState) {
				exits = append(exits, state)
			},
			PruneEdge: program.pruneEdge,
		},
	)
	if result.Class == interprocOutcomeInconclusive {
		return nil, result.Reason
	}
	if result.Class == interprocOutcomeUnsafe || len(exits) == 0 {
		return nil, pathOutcomeReasonFeasibilityUnknown
	}
	return deduplicateProtocolDeferredStates(exits), pathOutcomeReasonNone
}

func (planner constructorDeferredPlanner) deferCallRelevant(call *ast.CallExpr, target castTarget) bool {
	if call == nil || nodeHasTargetRelevantUnresolvedCall(planner.pass, call, target) {
		return call != nil
	}
	found := false
	ast.Inspect(call, func(node ast.Node) bool {
		if found {
			return false
		}
		identifier, ok := node.(*ast.Ident)
		if ok && objectForIdent(planner.pass, identifier) == planner.errorResult {
			found = true
			return false
		}
		return true
	})
	return found
}

func buildProtocolDeferredErrorProgram(
	pass *analysis.Pass,
	procedure protocolProcedure,
	errorResult *types.Var,
	validationProgram protocolValidationProgram,
	methodCalls methodValueValidateCallSet,
	target castTarget,
) protocolDeferredErrorProgram {
	program := protocolDeferredErrorProgram{
		pass: pass, procedure: procedure, errorResult: errorResult,
		validationProgram: validationProgram,
		methodCalls:       methodCalls,
		target:            target,
		errorLoadKeys:     make(map[string]bool),
	}
	if pass == nil || procedure.Function == nil || errorResult == nil {
		program.buildReason = pathOutcomeReasonMissingSSA
		return program
	}
	type rawStore struct {
		store            *ssa.Store
		blockIndex       int
		instructionIndex int
	}
	rawStores := make([]rawStore, 0)
	allStores := make([]rawStore, 0)
	for _, block := range procedure.Function.Blocks {
		if block == nil {
			continue
		}
		for instructionIndex, instruction := range block.Instrs {
			store, ok := instruction.(*ssa.Store)
			if !ok {
				continue
			}
			raw := rawStore{store: store, blockIndex: block.Index, instructionIndex: instructionIndex}
			allStores = append(allStores, raw)
			if constructorDeferredObjectAtPosition(pass, store.Pos()) != errorResult {
				continue
			}
			if program.errorAddress != nil && program.errorAddress != store.Addr {
				program.buildReason = pathOutcomeReasonAmbiguousIdentity
				return program
			}
			program.errorAddress = store.Addr
			rawStores = append(rawStores, raw)
		}
	}
	if program.errorAddress == nil {
		program.errorAddress = deferredErrorAddressFromDebugRefs(procedure.Function, errorResult)
	}
	if program.errorAddress != nil {
		for _, block := range procedure.Function.Blocks {
			if block == nil {
				continue
			}
			for _, instruction := range block.Instrs {
				loaded, ok := instruction.(*ssa.UnOp)
				if !ok || loaded.Op != token.MUL || !deferredSSAValueDependsOn(loaded.X, program.errorAddress, nil) {
					continue
				}
				program.errorLoadKeys[cfgSSAValueKey(loaded)] = true
			}
		}
		for _, raw := range allStores {
			if constructorDeferredObjectAtPosition(pass, raw.store.Pos()) == errorResult {
				continue
			}
			if deferredSSAValueDependsOn(raw.store.Addr, program.errorAddress, nil) ||
				ssaAddressStoresError(raw.store.Addr) {
				program.buildReason = pathOutcomeReasonAmbiguousIdentity
				return program
			}
		}
	}
	for _, raw := range rawStores {
		tag, reason := program.storeRelation(raw.store.Val)
		program.stores = append(program.stores, protocolDeferredErrorStore{
			position: raw.store.Pos(), blockIndex: raw.blockIndex,
			instructionIndex: raw.instructionIndex, tag: tag, reason: reason,
		})
	}
	sort.SliceStable(program.stores, func(left, right int) bool {
		if program.stores[left].blockIndex != program.stores[right].blockIndex {
			return program.stores[left].blockIndex < program.stores[right].blockIndex
		}
		return program.stores[left].instructionIndex < program.stores[right].instructionIndex
	})
	return program
}

func constructorDeferredObjectAtPosition(pass *analysis.Pass, position token.Pos) types.Object {
	if pass == nil || pass.TypesInfo == nil || !position.IsValid() {
		return nil
	}
	for _, file := range pass.Files {
		var result types.Object
		ast.Inspect(file, func(node ast.Node) bool {
			if result != nil {
				return false
			}
			identifier, ok := node.(*ast.Ident)
			if !ok || identifier.Pos() != position {
				return true
			}
			result = objectForIdent(pass, identifier)
			return false
		})
		if result != nil {
			return result
		}
	}
	return nil
}

func deferredErrorAddressFromDebugRefs(function *ssa.Function, errorResult *types.Var) ssa.Value {
	if function == nil || errorResult == nil {
		return nil
	}
	var address ssa.Value
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		for _, instruction := range block.Instrs {
			debug, ok := instruction.(*ssa.DebugRef)
			if !ok || debug.Object() != errorResult {
				continue
			}
			loaded, loadOK := debug.X.(*ssa.UnOp)
			if !loadOK || loaded.Op != token.MUL {
				continue
			}
			if address != nil && address != loaded.X {
				return nil
			}
			address = loaded.X
		}
	}
	return address
}

func deferredSSAValueDependsOn(value, target ssa.Value, seen map[ssa.Value]bool) bool {
	if value == nil || target == nil {
		return false
	}
	if value == target {
		return true
	}
	if seen == nil {
		seen = make(map[ssa.Value]bool)
	}
	if seen[value] {
		return false
	}
	seen[value] = true
	operander, ok := value.(interface {
		Operands([]*ssa.Value) []*ssa.Value
	})
	if !ok {
		return false
	}
	for _, operand := range operander.Operands(nil) {
		if operand != nil && deferredSSAValueDependsOn(*operand, target, seen) {
			return true
		}
	}
	return false
}

func ssaAddressStoresError(address ssa.Value) bool {
	if address == nil || address.Type() == nil {
		return false
	}
	pointer, ok := types.Unalias(address.Type()).Underlying().(*types.Pointer)
	return ok && isErrorType(pointer.Elem())
}

func (program protocolDeferredErrorProgram) storeRelation(value ssa.Value) (ideEdgeFuncTag, pathOutcomeReason) {
	if constant, ok := value.(*ssa.Const); ok && constant.IsNil() {
		return ideEdgeFuncDeferredErrorNil, pathOutcomeReasonNone
	}
	resolution := protocolAliasUnknown
	for _, invocations := range program.validationProgram.invocationsByCall {
		for _, invocation := range invocations {
			if invocation.Result == nil || !protocolValueDerivedFrom(value, invocation.Result, make(map[ssa.Value]bool)) {
				continue
			}
			switch protocolInvocationTargetResolution(program.pass, program.target, invocation) {
			case protocolAliasMust:
				resolution = protocolAliasMust
			case protocolAliasAmbiguous:
				return ideEdgeFuncIdentity, pathOutcomeReasonAmbiguousIdentity
			case protocolAliasUnknown:
			}
		}
	}
	if resolution == protocolAliasMust {
		return ideEdgeFuncDeferredErrorValidation, pathOutcomeReasonNone
	}
	if loaded, ok := value.(*ssa.UnOp); ok && loaded.Op == token.MUL &&
		deferredSSAValueDependsOn(loaded.X, program.errorAddress, nil) {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	return ideEdgeFuncDeferredErrorOther, pathOutcomeReasonNone
}

func protocolDeferredContinuationNodes(graph interprocSupergraph) map[string]bool {
	continuations := make(map[string]bool)
	maxCallOrdinal := make(map[string]int)
	for _, node := range graph.Nodes {
		base := interprocNodeID{
			FuncKey: node.FuncKey, BlockIndex: node.BlockIndex,
			NodeIndex: node.NodeIndex, Kind: interprocNodeKindCFG,
		}
		baseKey := base.Key()
		switch node.Kind {
		case interprocNodeKindCFG:
			continuations[node.Key()] = true
		case interprocNodeKindCall, interprocNodeKindReturn:
			continuations[baseKey] = false
			if ordinal, exists := maxCallOrdinal[baseKey]; !exists || node.CallOrdinal > ordinal {
				maxCallOrdinal[baseKey] = node.CallOrdinal
			}
		case interprocNodeKindExit:
		}
	}
	for _, node := range graph.Nodes {
		if node.Kind != interprocNodeKindReturn {
			continue
		}
		base := interprocNodeID{
			FuncKey: node.FuncKey, BlockIndex: node.BlockIndex,
			NodeIndex: node.NodeIndex, Kind: interprocNodeKindCFG,
		}
		if node.CallOrdinal == maxCallOrdinal[base.Key()] {
			continuations[node.Key()] = true
		}
	}
	return continuations
}

func (program protocolDeferredErrorProgram) nodeTransfer(
	graph interprocSupergraph,
	nodeID interprocNodeID,
	node ast.Node,
	state protocolAbstractState,
	calleeSummaryCache *sync.Map,
) (ideEdgeFuncTag, pathOutcomeReason) {
	storeTag, storeReason := program.storeEffect(nodeID, node)
	if storeReason != pathOutcomeReasonNone {
		return ideEdgeFuncIdentity, storeReason
	}
	if program.validationProgram.nodeHasTargetInvocation(program.pass, node, program.target) {
		return storeTag, pathOutcomeReasonNone
	}
	effective := state
	if effective.DeferredError == protocolDeferredErrorValidation {
		effective.Validation = protocolValidationProven
	}
	effectTag, effectReason := ubvGraphNodeEdgeTag(
		graph,
		nodeID,
		program.pass,
		node,
		program.target,
		nil,
		nil,
		program.methodCalls,
		effective,
		nil,
		calleeSummaryCache,
	)
	if effectReason != pathOutcomeReasonNone {
		return ideEdgeFuncIdentity, effectReason
	}
	if effectTag != ideEdgeFuncIdentity && storeTag != ideEdgeFuncIdentity {
		return ideEdgeFuncIdentity, pathOutcomeReasonUnsupportedInstr
	}
	if storeTag != ideEdgeFuncIdentity {
		return storeTag, pathOutcomeReasonNone
	}
	return effectTag, pathOutcomeReasonNone
}

func (program protocolDeferredErrorProgram) storeEffect(
	nodeID interprocNodeID,
	node ast.Node,
) (ideEdgeFuncTag, pathOutcomeReason) {
	if node == nil || nodeID.FuncKey != program.procedure.Key || !program.continuations[nodeID.Key()] {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	var matches []protocolDeferredErrorStore
	for _, store := range program.stores {
		if store.position >= node.Pos() && store.position <= node.End() {
			matches = append(matches, store)
		}
	}
	if len(matches) == 0 {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	if len(matches) != 1 {
		return ideEdgeFuncIdentity, pathOutcomeReasonUnsupportedInstr
	}
	return matches[0].tag, matches[0].reason
}

func (program protocolDeferredErrorProgram) nodeReferencesError(node ast.Node) bool {
	if node == nil || program.errorResult == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(candidate ast.Node) bool {
		if found {
			return false
		}
		identifier, ok := candidate.(*ast.Ident)
		if ok && objectForIdent(program.pass, identifier) == program.errorResult {
			found = true
			return false
		}
		return true
	})
	return found
}

func (program protocolDeferredErrorProgram) pruneEdge(
	edge interprocEdge,
	state protocolAbstractState,
) bool {
	if state.DeferredError != protocolDeferredErrorNil || len(program.errorLoadKeys) == 0 {
		return false
	}
	for _, provenance := range edge.PredicateProvenance {
		formula := cfgSSAFormulaFromPredicateProvenance(provenance)
		if formula.unsupported {
			continue
		}
		for key := range program.errorLoadKeys {
			result, proven := protocolResultProvenByFormula(formula, key)
			if proven && result == protocolErrorResultNonNil {
				return true
			}
		}
	}
	return false
}
