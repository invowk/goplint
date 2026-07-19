// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/ssa"
)

type interprocNodeKind string

const (
	interprocNodeKindCFG    interprocNodeKind = "cfg-node"
	interprocNodeKindCall   interprocNodeKind = "call-site"
	interprocNodeKindReturn interprocNodeKind = "return-site"
	interprocNodeKindExit   interprocNodeKind = "function-exit"
)

type interprocNodeID struct {
	FuncKey     string
	BlockIndex  int32
	NodeIndex   int
	CallOrdinal int
	Kind        interprocNodeKind
}

func (id interprocNodeID) Key() string {
	return fmt.Sprintf("%s|%s|%d|%d|%d", id.FuncKey, id.Kind, id.BlockIndex, id.NodeIndex, id.CallOrdinal)
}

type interprocEdgeKind string

const (
	interprocEdgeIntra        interprocEdgeKind = "intra"
	interprocEdgeCall         interprocEdgeKind = "call"
	interprocEdgeReturn       interprocEdgeKind = "return"
	interprocEdgeCallToReturn interprocEdgeKind = "call-to-return"
)

type interprocEdge struct {
	From                interprocNodeID
	To                  interprocNodeID
	Kind                interprocEdgeKind
	CallSite            string
	Reason              pathOutcomeReason
	PredicateProvenance []string
}

type interprocSupergraph struct {
	Nodes                 map[string]interprocNodeID
	NodeAST               map[string]ast.Node
	CallEvents            map[string]protocolCallEvent
	Edges                 []interprocEdge
	terminalCFGNodes      map[string]bool
	functionExitNodes     map[string]bool
	nonReturningNodes     map[string]bool
	nonReturningFunctions map[string]bool
	callEventIndex        protocolCallEventIndex
	procedureIndex        protocolProcedureIndex
}

func newInterprocSupergraph() interprocSupergraph {
	return interprocSupergraph{
		Nodes:                 make(map[string]interprocNodeID),
		NodeAST:               make(map[string]ast.Node),
		CallEvents:            make(map[string]protocolCallEvent),
		terminalCFGNodes:      make(map[string]bool),
		functionExitNodes:     make(map[string]bool),
		nonReturningNodes:     make(map[string]bool),
		nonReturningFunctions: make(map[string]bool),
	}
}

func (graph *interprocSupergraph) addCallNode(id interprocNodeID, event protocolCallEvent) {
	if graph == nil {
		return
	}
	graph.addNode(id, event.Call)
	if graph.CallEvents == nil {
		graph.CallEvents = make(map[string]protocolCallEvent)
	}
	graph.CallEvents[id.Key()] = event
}

func (graph *interprocSupergraph) addNode(id interprocNodeID, node ast.Node) {
	if graph == nil {
		return
	}
	if graph.Nodes == nil {
		graph.Nodes = make(map[string]interprocNodeID)
	}
	key := id.Key()
	graph.Nodes[key] = id
	if node != nil {
		if graph.NodeAST == nil {
			graph.NodeAST = make(map[string]ast.Node)
		}
		graph.NodeAST[key] = node
	}
}

func (graph *interprocSupergraph) addEdge(edge interprocEdge) {
	if graph == nil {
		return
	}
	graph.addNode(edge.From, nil)
	graph.addNode(edge.To, nil)
	graph.Edges = append(graph.Edges, edge)
}

func (graph interprocSupergraph) outgoing(from interprocNodeID) []interprocEdge {
	out, _ := graph.outgoingWithControl(from, nil)
	return out
}

func (graph interprocSupergraph) outgoingWithControl(
	from interprocNodeID,
	control protocolAnalysisControl,
) ([]interprocEdge, bool) {
	if feasibilityDeadlineReached(control) {
		return nil, true
	}
	if len(graph.Edges) == 0 {
		return nil, false
	}
	key := from.Key()
	out := make([]interprocEdge, 0)
	for _, edge := range graph.Edges {
		if feasibilityDeadlineReached(control) {
			return nil, true
		}
		if edge.From.Key() == key {
			out = append(out, edge)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left := string(out[i].Kind) + "|" + out[i].CallSite + "|" + out[i].To.Key() + "|" + string(out[i].Reason)
		right := string(out[j].Kind) + "|" + out[j].CallSite + "|" + out[j].To.Key() + "|" + string(out[j].Reason)
		left += "|" + strings.Join(out[i].PredicateProvenance, ",")
		right += "|" + strings.Join(out[j].PredicateProvenance, ",")
		return left < right
	})
	return out, feasibilityDeadlineReached(control)
}

func (graph interprocSupergraph) astNode(id interprocNodeID) ast.Node {
	return graph.NodeAST[id.Key()]
}

func (graph interprocSupergraph) callEvent(id interprocNodeID) (protocolCallEvent, bool) {
	event, ok := graph.CallEvents[id.Key()]
	return event, ok
}

func (graph interprocSupergraph) isTerminalCFGNode(id interprocNodeID) bool {
	return graph.terminalCFGNodes[id.Key()]
}

func (graph interprocSupergraph) isFunctionExitNode(id interprocNodeID) bool {
	return graph.functionExitNodes[id.Key()]
}

func (graph interprocSupergraph) isNonReturningNode(id interprocNodeID) bool {
	return graph.nonReturningNodes[id.Key()]
}

func (graph interprocSupergraph) firstCFGNode() (interprocNodeID, bool) {
	var first interprocNodeID
	found := false
	for _, node := range graph.Nodes {
		if node.Kind != interprocNodeKindCFG || (found && node.Key() >= first.Key()) {
			continue
		}
		first = node
		found = true
	}
	return first, found
}

type interprocFunctionSummary struct {
	entry    interprocNodeID
	exits    []interprocNodeID
	building bool
	built    bool
}

func buildInterprocSupergraphForFunc(
	pass *analysis.Pass,
	fnDecl *ast.FuncDecl,
	ssaResult *ssaResult,
) interprocSupergraph {
	if pass == nil || fnDecl == nil || fnDecl.Body == nil {
		return newInterprocSupergraph()
	}
	procedureIndex := buildProtocolProcedureIndex(pass, ssaResult)
	procedure, ok := procedureIndex.byDecl[fnDecl]
	if !ok {
		procedure = protocolProcedure{
			Key:         interprocFunctionKey(pass, fnDecl),
			Declaration: fnDecl,
			Body:        fnDecl.Body,
		}
	}
	graph, _, _ := buildInterprocSupergraphForProcedure(pass, procedure, ssaResult)
	return graph
}

func buildInterprocSupergraphForProcedure(
	pass *analysis.Pass,
	procedure protocolProcedure,
	ssaResult *ssaResult,
) (interprocSupergraph, interprocNodeID, bool) {
	graph := newInterprocSupergraph()
	if pass == nil || procedure.Body == nil || procedure.Key == "" {
		return graph, interprocNodeID{}, false
	}
	cache := make(map[string]*interprocFunctionSummary)
	graph.callEventIndex = buildProtocolCallEventIndex(pass, ssaResult)
	graph.procedureIndex = buildProtocolProcedureIndex(pass, ssaResult)
	noReturnCalls := newNoReturnCallResolver(pass, procedure.Body, ssaResult)
	predicateValues := buildCFGSSAValueIndexFromResult(noReturnCalls.ssa)
	mayReturn := computeProtocolMayReturn(pass, noReturnCalls)
	for key, returns := range mayReturn {
		if !returns {
			graph.nonReturningFunctions[key] = true
		}
	}
	summary, ok := appendInterprocProcedureGraph(
		pass, procedure, &graph, cache, mayReturn, noReturnCalls, predicateValues,
	)
	return graph, summary.entry, ok
}

func appendInterprocFunctionGraph(
	pass *analysis.Pass,
	fnDecl *ast.FuncDecl,
	graph *interprocSupergraph,
	cache map[string]*interprocFunctionSummary,
	mayReturn map[string]bool,
	noReturnCalls noReturnCallResolver,
	predicateValues cfgSSAValueIndex,
) (interprocFunctionSummary, bool) {
	if graph == nil || fnDecl == nil || fnDecl.Body == nil {
		return interprocFunctionSummary{}, false
	}
	procedure, ok := graph.procedureIndex.byDecl[fnDecl]
	if !ok {
		procedure = protocolProcedure{
			Key:         interprocFunctionKey(pass, fnDecl),
			Declaration: fnDecl,
			Body:        fnDecl.Body,
		}
	}
	return appendInterprocProcedureGraph(
		pass, procedure, graph, cache, mayReturn, noReturnCalls, predicateValues,
	)
}

func appendInterprocProcedureGraph(
	pass *analysis.Pass,
	procedure protocolProcedure,
	graph *interprocSupergraph,
	cache map[string]*interprocFunctionSummary,
	mayReturn map[string]bool,
	noReturnCalls noReturnCallResolver,
	predicateValues cfgSSAValueIndex,
) (interprocFunctionSummary, bool) {
	if graph == nil || procedure.Body == nil {
		return interprocFunctionSummary{}, false
	}
	funcKey := procedure.Key
	if funcKey == "" {
		return interprocFunctionSummary{}, false
	}
	if cached, ok := cache[funcKey]; ok {
		if cached.building {
			return *cached, cached.entry.FuncKey != ""
		}
		if cached.built {
			return *cached, true
		}
	}
	summary := &interprocFunctionSummary{building: true}
	cache[funcKey] = summary
	defer func() {
		summary.building = false
	}()

	cfg := buildProtocolCFG(pass, procedure.Body, noReturnCalls.ssa)
	if cfg == nil || len(cfg.Blocks) == 0 {
		return interprocFunctionSummary{}, false
	}

	blockEntryNode := map[int32]interprocNodeID{}
	for _, block := range cfg.Blocks {
		if block == nil || len(block.Nodes) == 0 {
			continue
		}
		first := interprocNodeID{
			FuncKey:    funcKey,
			BlockIndex: block.Index,
			NodeIndex:  0,
			Kind:       interprocNodeKindCFG,
		}
		blockEntryNode[block.Index] = first
	}
	if len(blockEntryNode) == 0 {
		return interprocFunctionSummary{}, false
	}
	entry, ok := blockEntryNode[cfg.Blocks[0].Index]
	if !ok {
		for _, block := range cfg.Blocks {
			if block == nil {
				continue
			}
			entry, ok = blockEntryNode[block.Index]
			if ok {
				break
			}
		}
	}
	if !ok {
		return interprocFunctionSummary{}, false
	}
	var exits []interprocNodeID
	if returns, known := mayReturn[funcKey]; !known || returns {
		exits = interprocFunctionCFGExits(
			pass, funcKey, procedure.Function, cfg.Blocks, blockEntryNode, noReturnCalls, graph.callEventIndex,
		)
	}
	summary.entry = entry
	summary.exits = exits

	for _, block := range cfg.Blocks {
		if block == nil || len(block.Nodes) == 0 {
			continue
		}
		continuationNode := interprocNodeID{}
		canContinue := false
		for idx, node := range block.Nodes {
			nodeID := interprocNodeID{
				FuncKey:    funcKey,
				BlockIndex: block.Index,
				NodeIndex:  idx,
				Kind:       interprocNodeKindCFG,
			}
			graph.addNode(nodeID, node)
			if canContinue {
				graph.addEdge(interprocEdge{From: continuationNode, To: nodeID, Kind: interprocEdgeIntra})
			}
			continuationNode = nodeID
			canContinue = true

			events := graph.callEventIndex.eventsForProcedureNode(pass, funcKey, procedure.Function, node)
			for ordinal, event := range events {
				callNode := interprocNodeID{
					FuncKey:     funcKey,
					BlockIndex:  block.Index,
					NodeIndex:   idx,
					CallOrdinal: ordinal,
					Kind:        interprocNodeKindCall,
				}
				retNode := callNode
				retNode.Kind = interprocNodeKindReturn
				graph.addCallNode(callNode, event)
				graph.addCallNode(retNode, event)
				graph.addNode(retNode, node)
				graph.addEdge(interprocEdge{From: continuationNode, To: callNode, Kind: interprocEdgeIntra})

				returns := appendInterprocCallEvent(
					pass,
					event,
					callNode,
					retNode,
					graph,
					cache,
					mayReturn,
					noReturnCalls,
					predicateValues,
				)
				if !returns {
					canContinue = false
					break
				}
				continuationNode = retNode
			}
			if !canContinue {
				break
			}
		}

		hasMappedSucc := false
		for _, succ := range block.Succs {
			if !canContinue {
				break
			}
			for _, entry := range nonEmptySuccessorEntries(succ, blockEntryNode) {
				hasMappedSucc = true
				graph.addEdge(interprocEdge{
					From:                continuationNode,
					To:                  entry,
					Kind:                interprocEdgeIntra,
					PredicateProvenance: cfgSSAPredicateProvenance(pass, predicateValues, block, succ),
				})
			}
		}
		if canContinue && !hasMappedSucc {
			if graph.terminalCFGNodes == nil {
				graph.terminalCFGNodes = make(map[string]bool)
			}
			graph.terminalCFGNodes[continuationNode.Key()] = true
			graph.functionExitNodes[continuationNode.Key()] = true
		}
	}
	summary.built = true
	return *summary, true
}

func appendInterprocCallEvent(
	pass *analysis.Pass,
	event protocolCallEvent,
	callNode interprocNodeID,
	returnNode interprocNodeID,
	graph *interprocSupergraph,
	cache map[string]*interprocFunctionSummary,
	mayReturn map[string]bool,
	noReturnCalls noReturnCallResolver,
	predicateValues cfgSSAValueIndex,
) bool {
	if graph == nil || event.Call == nil {
		return false
	}
	callSite := callNode.Key()
	if event.Phase == protocolCallEventDeferRegistration || event.Phase == protocolCallEventGo {
		graph.addEdge(interprocEdge{
			From: callNode, To: returnNode, Kind: interprocEdgeCallToReturn, CallSite: callSite,
		})
		return true
	}
	if protocolCallIsBuiltin(event) {
		if event.Builtin == "panic" || !callMayReturn(pass, event.Call, noReturnCalls) {
			graph.nonReturningNodes[callNode.Key()] = true
			return false
		}
		graph.addEdge(interprocEdge{
			From: callNode, To: returnNode, Kind: interprocEdgeCallToReturn, CallSite: callSite,
		})
		return true
	}
	if !event.Mapped {
		graph.addEdge(interprocEdge{
			From: callNode, To: returnNode, Kind: interprocEdgeCallToReturn, CallSite: callSite,
			Reason: pathOutcomeReasonCallMapping,
		})
		return true
	}
	if !callMayReturn(pass, event.Call, noReturnCalls) {
		graph.nonReturningNodes[callNode.Key()] = true
		return false
	}
	// Checked Validate() calls are protocol effects, not ordinary callees.
	// buildProtocolValidationProgram models their nil-error edge transfer
	// directly. Expanding the validator body here as well would import its
	// implementation details into the caller's obligation and could turn a
	// canonical checked validation into alias, escape, or budget uncertainty.
	if event.Instruction != nil {
		if _, ok := protocolValidateReceiver(event.Instruction.Common()); ok {
			graph.addEdge(interprocEdge{
				From: callNode, To: returnNode, Kind: interprocEdgeCallToReturn, CallSite: callSite,
			})
			return true
		}
	}
	if event.Instruction != nil {
		if procedure, ok := graph.procedureIndex.resolveCall(event.Instruction); ok && procedure.Literal != nil {
			if !procedure.CaptureExact {
				graph.addEdge(interprocEdge{
					From: callNode, To: returnNode, Kind: interprocEdgeCallToReturn, CallSite: callSite,
					Reason: pathOutcomeReasonAmbiguousIdentity,
				})
				return true
			}
			calleeSummary, appended := appendInterprocProcedureGraph(
				pass, procedure, graph, cache, mayReturn, noReturnCalls, predicateValues,
			)
			if appended {
				graph.addEdge(interprocEdge{
					From: callNode, To: calleeSummary.entry, Kind: interprocEdgeCall, CallSite: callSite,
				})
				for _, exitNode := range calleeSummary.exits {
					graph.addEdge(interprocEdge{
						From: exitNode, To: returnNode, Kind: interprocEdgeReturn, CallSite: callSite,
					})
				}
				if len(calleeSummary.exits) == 0 {
					graph.nonReturningNodes[callNode.Key()] = true
					return false
				}
				return true
			}
		}
	}
	if function := calledFunctionObject(pass, event.Call); function != nil {
		if declaration := findFuncDeclForObject(pass, function); declaration != nil && declaration.Body != nil {
			calleeSummary, ok := appendInterprocFunctionGraph(
				pass, declaration, graph, cache, mayReturn, noReturnCalls, predicateValues,
			)
			if ok {
				graph.addEdge(interprocEdge{From: callNode, To: calleeSummary.entry, Kind: interprocEdgeCall, CallSite: callSite})
				for _, exitNode := range calleeSummary.exits {
					graph.addEdge(interprocEdge{From: exitNode, To: returnNode, Kind: interprocEdgeReturn, CallSite: callSite})
				}
				if len(calleeSummary.exits) == 0 {
					graph.nonReturningNodes[callNode.Key()] = true
					return false
				}
				return true
			}
		}
	}
	graph.addEdge(interprocEdge{
		From: callNode, To: returnNode, Kind: interprocEdgeCallToReturn, CallSite: callSite,
		Reason: unresolvedCallOutcomeReason(pass, event.Call),
	})
	return true
}

func unresolvedCallOutcomeReason(pass *analysis.Pass, call *ast.CallExpr) pathOutcomeReason {
	packagePath := ""
	if function := calledFunctionObject(pass, call); function != nil && function.Pkg() != nil {
		packagePath = function.Pkg().Path()
		if pass != nil && pass.ImportObjectFact != nil && pass.Pkg != nil && function.Pkg() != pass.Pkg {
			fact := &ProtocolSummaryFact{}
			if pass.ImportObjectFact(function, fact) && validateProtocolSummaryFact(fact, function) == 0 {
				return pathOutcomeReasonNone
			}
		}
	}
	if packagePath == "" && pass != nil && pass.TypesInfo != nil && call != nil {
		selector, ok := stripParens(call.Fun).(*ast.SelectorExpr)
		if ok {
			identifier, identOK := stripParens(selector.X).(*ast.Ident)
			if identOK {
				if packageName, pkgOK := pass.TypesInfo.Uses[identifier].(*types.PkgName); pkgOK && packageName.Imported() != nil {
					packagePath = packageName.Imported().Path()
				}
			}
		}
	}
	switch packagePath {
	case "reflect":
		return pathOutcomeReasonReflection
	case "unsafe":
		return pathOutcomeReasonUnsafe
	default:
		return pathOutcomeReasonUnresolvedTarget
	}
}

func computeProtocolMayReturn(pass *analysis.Pass, noReturnCalls noReturnCallResolver) map[string]bool {
	functions := make(map[string]*ast.FuncDecl)
	if pass == nil {
		return map[string]bool{}
	}
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			key := interprocFunctionKey(pass, fn)
			if key != "" {
				functions[key] = fn
			}
		}
	}
	mayReturn := make(map[string]bool, len(functions))
	for changed := true; changed; {
		changed = false
		for key, fn := range functions {
			if mayReturn[key] || !protocolFunctionHasReturningPath(pass, fn, mayReturn, noReturnCalls) {
				continue
			}
			mayReturn[key] = true
			changed = true
		}
	}
	for key := range functions {
		if _, ok := mayReturn[key]; !ok {
			mayReturn[key] = false
		}
	}
	return mayReturn
}

func protocolFunctionHasReturningPath(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	mayReturn map[string]bool,
	noReturnCalls noReturnCallResolver,
) bool {
	if pass == nil || fn == nil || fn.Body == nil {
		return false
	}
	callReturns := func(call *ast.CallExpr) bool {
		if !callMayReturn(pass, call, noReturnCalls) {
			return false
		}
		if object := calledFunctionObject(pass, call); object != nil {
			if callee := findFuncDeclForObject(pass, object); callee != nil {
				return mayReturn[interprocFunctionKey(pass, callee)]
			}
		}
		return true
	}
	cfg := gocfg.New(fn.Body, callReturns)
	for _, block := range cfg.Blocks {
		if block == nil || len(block.Nodes) == 0 || len(block.Succs) != 0 {
			continue
		}
		returns := true
		for _, call := range protocolOrderedCallsInNode(block.Nodes[len(block.Nodes)-1]) {
			if !callReturns(call) {
				returns = false
				break
			}
		}
		if !returns {
			continue
		}
		return true
	}
	return false
}

func interprocFunctionCFGExits(
	pass *analysis.Pass,
	funcKey string,
	caller *ssa.Function,
	blocks []*gocfg.Block,
	blockEntryNode map[int32]interprocNodeID,
	noReturnCalls noReturnCallResolver,
	callEvents protocolCallEventIndex,
) []interprocNodeID {
	exits := make([]interprocNodeID, 0)
	for _, block := range blocks {
		if block == nil || len(block.Nodes) == 0 {
			continue
		}
		hasMappedSuccessor := false
		for _, successor := range block.Succs {
			if successor == nil {
				continue
			}
			if _, ok := blockEntryNode[successor.Index]; ok {
				hasMappedSuccessor = true
				break
			}
		}
		if !hasMappedSuccessor {
			lastIndex := len(block.Nodes) - 1
			kind := interprocNodeKindCFG
			callOrdinal := 0
			nonReturning := false
			events := callEvents.eventsForProcedureNode(pass, funcKey, caller, block.Nodes[lastIndex])
			for ordinal, event := range events {
				if event.Phase == protocolCallEventSync && !callMayReturn(pass, event.Call, noReturnCalls) {
					nonReturning = true
					break
				}
				kind = interprocNodeKindReturn
				callOrdinal = ordinal
			}
			if nonReturning {
				continue
			}
			exits = append(exits, interprocNodeID{
				FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: lastIndex, CallOrdinal: callOrdinal, Kind: kind,
			})
		}
	}
	return exits
}

func interprocFunctionKey(pass *analysis.Pass, fnDecl *ast.FuncDecl) string {
	if fnDecl == nil || fnDecl.Name == nil {
		return ""
	}
	if pass != nil && pass.TypesInfo != nil {
		if obj := pass.TypesInfo.Defs[fnDecl.Name]; obj != nil {
			if key := objectKey(obj); key != "" {
				return key
			}
		}
	}
	if pass != nil && pass.Pkg != nil {
		return protocolSourceProcedureKey(pass, fnDecl.Pos(), "func-decl")
	}
	return "func-decl@" + semanticASTNodeKey(fnDecl, fnDecl)
}
