// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestCallEventsUseSSAInstructionOrderRatherThanASTPreorder(t *testing.T) {
	t.Parallel()

	const source = `package probe
func first() int { return 1 }
func second() int { return 2 }
func outer(int, int) {}
func entry() { outer(first(), second()) }
`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "entry")
	calls := protocolSourceCallsInNode(declaration.Body.List[0])
	if len(calls) != 3 {
		t.Fatalf("source call count = %d, want 3", len(calls))
	}
	byName := make(map[string]*ast.CallExpr)
	for _, call := range calls {
		identifier, ok := stripParens(call.Fun).(*ast.Ident)
		if !ok {
			t.Fatalf("call target = %T, want identifier", call.Fun)
		}
		byName[identifier.Name] = call
	}
	caller := &ssa.Function{}
	index := protocolCallEventIndex{
		byLparen:    make(map[token.Pos][]protocolCallEvent),
		phaseByCall: make(map[*ast.CallExpr]protocolCallEventPhase),
	}
	for name, instructionIndex := range map[string]int{"first": 10, "second": 20, "outer": 30} {
		call := byName[name]
		event := protocolCallEvent{
			ID: protocolCallSiteID{
				Procedure: "probe.entry", Position: call.Lparen,
				BlockIndex: 0, InstructionIndex: instructionIndex, Phase: protocolCallEventSync,
			},
			Call: call, Caller: caller, Phase: protocolCallEventSync, Mapped: true,
		}
		index.byLparen[call.Lparen] = []protocolCallEvent{event}
	}
	events := index.eventsForProcedureNode(pass, "probe.entry", caller, declaration.Body.List[0])
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	got := make([]string, 0, len(events))
	for _, event := range events {
		identifier, _ := stripParens(event.Call.Fun).(*ast.Ident)
		got = append(got, identifier.Name)
	}
	if want := []string{"first", "second", "outer"}; !slices.Equal(got, want) {
		t.Fatalf("event order = %v, want SSA order %v", got, want)
	}
}

func TestCallEventsFailClosedForDuplicateSourceAssociation(t *testing.T) {
	t.Parallel()

	const source = `package probe
func effect() {}
func entry() { effect() }
`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "entry")
	call := protocolSourceCallsInNode(declaration.Body.List[0])[0]
	caller := &ssa.Function{}
	base := protocolCallEvent{
		ID: protocolCallSiteID{
			Procedure: "probe.entry", Position: call.Lparen,
			BlockIndex: 0, InstructionIndex: 1, Phase: protocolCallEventSync,
		},
		Call: call, Caller: caller, Phase: protocolCallEventSync, Mapped: true,
	}
	duplicate := base
	duplicate.ID.InstructionIndex = 2
	index := protocolCallEventIndex{
		byLparen:    map[token.Pos][]protocolCallEvent{call.Lparen: {base, duplicate}},
		phaseByCall: map[*ast.CallExpr]protocolCallEventPhase{call: protocolCallEventSync},
	}
	events := index.eventsForProcedureNode(pass, "probe.entry", caller, declaration.Body.List[0])
	if len(events) != 1 || events[0].Mapped {
		t.Fatalf("duplicate source association = %+v, want one unmapped event", events)
	}
}

func TestCallEventsFailClosedForAmbiguousGenericInstantiationCaller(t *testing.T) {
	t.Parallel()

	const source = `package probe
func effect() {}
func entry() { effect() }
`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "entry")
	call := protocolSourceCallsInNode(declaration.Body.List[0])[0]
	event := func(caller *ssa.Function, instructionIndex int) protocolCallEvent {
		return protocolCallEvent{
			ID: protocolCallSiteID{
				Procedure: "probe.entry", Position: call.Lparen,
				BlockIndex: 0, InstructionIndex: instructionIndex, Phase: protocolCallEventSync,
			},
			Call: call, Caller: caller, Phase: protocolCallEventSync, Mapped: true,
		}
	}
	index := protocolCallEventIndex{
		byLparen: map[token.Pos][]protocolCallEvent{
			call.Lparen: {event(&ssa.Function{}, 1), event(&ssa.Function{}, 1)},
		},
		phaseByCall: map[*ast.CallExpr]protocolCallEventPhase{call: protocolCallEventSync},
	}
	events := index.eventsForNode(pass, "probe.entry", declaration.Body.List[0])
	if len(events) != 1 || events[0].Mapped {
		t.Fatalf("ambiguous caller association = %+v, want one unmapped event", events)
	}
}

func TestRecursiveValidationCallEventsPreserveTargetUntilBaseCase(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value Value) Validate() error { return nil }
func recursiveValidate(value Value, depth int) error {
	if depth <= 0 { return value.Validate() }
	return recursiveValidate(value, depth-1)
}
`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "recursiveValidate")
	function, ok := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
	if !ok {
		t.Fatal("recursiveValidate definition is not a function")
	}
	target, ok := functionTargetForSlot(pass, declaration, calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0})
	if !ok {
		t.Fatal("recursiveValidate target is unavailable")
	}
	ssaResult := buildSSAForPass(pass)
	cfg := buildProtocolCFG(pass, declaration.Body, ssaResult)
	cache := &sync.Map{}
	solver := newInterprocSolverWithSSA(pass, ssaResult, cache)
	stack := map[string]bool{objectKey(function) + "|arg:0": true}
	methodCalls := collectCalleeValidatedCalls(
		pass, declaration.Body, ssaResult, stackScopeFromMap(stack), cache,
	)
	castResult := solver.EvaluateCastPath(interprocCastPathInput{
		Decl: declaration, CFG: cfg, DefBlock: cfgEntryBlock(cfg), DefIdx: 0, Target: target,
		TypeName: "Value", OriginKey: "recursive-call-event-cast", MaxStates: defaultCFGMaxStates,
		MethodCalls:     methodCalls,
		SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaResult, declaration),
	})
	if castResult.Class != interprocOutcomeSafe {
		var terminalNode ast.Node
		if castResult.witnessGraph != nil {
			terminalNode = castResult.witnessGraph.astNode(castResult.WitnessTerminal)
		}
		t.Fatalf(
			"recursive validation cast path = %s (%s), want safe; terminal=%+v node=%T@%d",
			castResult.Class, castResult.Reason, castResult.WitnessTerminal, terminalNode, terminalNode.Pos(),
		)
	}
	result := solver.EvaluateUBVCrossBlock(interprocUBVCrossBlockInput{
		Target: target, DefBlock: cfgEntryBlock(cfg), DefIdx: -1, OriginAtEntry: true,
		OriginKey: "recursive-call-event", TypeName: "Value", MaxStates: defaultCFGMaxStates,
		SummaryStack:    stack,
		MethodCalls:     methodCalls,
		SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaResult, declaration),
	})
	if result.Class != interprocOutcomeSafe {
		var terminalNode ast.Node
		if result.witnessGraph != nil {
			terminalNode = result.witnessGraph.astNode(result.WitnessTerminal)
		}
		t.Fatalf(
			"recursive validation UBV = %s (%s), want safe; terminal=%+v node=%T@%d",
			result.Class, result.Reason, result.WitnessTerminal, terminalNode, terminalNode.Pos(),
		)
	}
}

func TestGoCallEventOwnsConcurrentTargetEffect(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value *Value) Validate() error { return nil }
func mutate(value *Value) { *value = "changed" }
func entry(raw string) {
	value := Value(raw)
	if err := value.Validate(); err != nil { return }
	if raw != "" { go mutate(&value) }
	_ = value
}
`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "entry")
	assigned, _, _, _ := collectCFACasts(
		pass, declaration.Body, buildParentMap(declaration.Body), func(*ast.FuncLit, int) {},
	)
	if len(assigned) != 1 {
		t.Fatalf("assigned cast count = %d, want 1", len(assigned))
	}
	ssaResult := buildSSAForPass(pass)
	if availability := enrichAssignedCastsWithSSA(pass, ssaResult, declaration, assigned); !availability.ready() {
		t.Fatalf("SSA enrichment = %+v, want ready", availability)
	}
	cfg := buildProtocolCFG(pass, declaration.Body, ssaResult)
	definitionBlock, definitionIndex := findDefiningBlock(cfg, assigned[0].assign)
	graph := buildInterprocSupergraphFromReachableBlocksWithResolution(pass, definitionBlock, "cfg.ubv.probe")
	for _, node := range graph.Nodes {
		event, ok := graph.callEvent(node)
		if !ok || node.Kind != interprocNodeKindCall || event.Phase != protocolCallEventGo {
			continue
		}
		tag, reason := ubvGraphNodeEdgeTag(
			graph,
			node,
			pass,
			graph.astNode(node),
			assigned[0].target,
			nil,
			nil,
			nil,
			protocolAbstractState{Validation: protocolValidationProven},
			nil,
			&sync.Map{},
		)
		if tag != ideEdgeFuncIdentity || reason != pathOutcomeReasonConcurrentMutation {
			t.Fatalf("go call transfer = (%q, %q), want identity/concurrent-mutation", tag, reason)
		}
		var sinkNodes []interprocNodeID
		for _, candidate := range graph.Nodes {
			if candidate.Kind != interprocNodeKindCFG {
				continue
			}
			if ubvNonCallNodeIsObligationSink(
				pass, graph.astNode(candidate), assigned[0].target, nil, nil, nil,
			) {
				sinkNodes = append(sinkNodes, candidate)
			}
		}
		if len(sinkNodes) == 0 {
			t.Fatal("later non-call sink was not represented in the graph")
		}
		reachable := map[string]bool{node.Key(): true}
		queue := []interprocNodeID{node}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			for _, edge := range graph.outgoing(current) {
				if reachable[edge.To.Key()] {
					continue
				}
				reachable[edge.To.Key()] = true
				queue = append(queue, edge.To)
			}
		}
		reachableSink := false
		for _, sinkNode := range sinkNodes {
			if reachable[sinkNode.Key()] {
				reachableSink = true
				break
			}
		}
		if !reachableSink {
			nodes := make([]string, 0, len(graph.Nodes))
			for _, candidate := range graph.Nodes {
				nodes = append(nodes, fmt.Sprintf("%s=%T@%d", candidate.Key(), graph.astNode(candidate), graph.astNode(candidate).Pos()))
			}
			t.Fatalf("later sinks %+v are unreachable from go call %q; nodes=%v", sinkNodes, node.Key(), nodes)
		}
		result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateUBVCrossBlock(interprocUBVCrossBlockInput{
			Target: assigned[0].target, DefBlock: definitionBlock, DefIdx: definitionIndex,
			OriginKey: "go-event-probe", TypeName: assigned[0].typeName,
			MaxStates: defaultCFGMaxStates, SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaResult, declaration),
		})
		if result.Class != interprocOutcomeInconclusive || result.Reason != pathOutcomeReasonConcurrentMutation {
			t.Fatalf("go call to later sink result = %s (%s), want inconclusive concurrent-mutation; witness=%+v edges=%+v", result.Class, result.Reason, result.WitnessEdges, graph.Edges)
		}
		return
	}
	t.Fatal("go call event not found")
}

func TestIIFEProcedurePropagatesCapturedTargetUse(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value Value) Validate() error { return nil }
func use(value Value) { _ = value }
func entry(raw string) error {
	value := Value(raw)
	func() { use(value) }()
	return value.Validate()
}
`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "entry")
	assigned, _, closureCalls, methodValueCalls := collectCFACasts(
		pass, declaration.Body, buildParentMap(declaration.Body), func(*ast.FuncLit, int) {},
	)
	if len(assigned) != 1 {
		t.Fatalf("assigned cast count = %d, want 1", len(assigned))
	}
	ssaResult := buildSSAForPass(pass)
	if availability := enrichAssignedCastsWithSSA(pass, ssaResult, declaration, assigned); !availability.ready() {
		t.Fatalf("SSA enrichment = %+v, want ready", availability)
	}
	cfg := buildProtocolCFG(pass, declaration.Body, ssaResult)
	definitionBlock, definitionIndex := findDefiningBlock(cfg, assigned[0].assign)
	methodCalls := collectMethodValueValidateCallSet(methodValueCalls)
	methodCalls = mergeMethodValueValidateCallSets(
		methodCalls,
		collectCalleeValidatedCalls(pass, declaration.Body, ssaResult, stackScopeFromMap(nil), &sync.Map{}),
	)
	result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateUBVCrossBlock(interprocUBVCrossBlockInput{
		Target: assigned[0].target, DefBlock: definitionBlock, DefIdx: definitionIndex,
		OriginKey: "iife-capture-probe", TypeName: assigned[0].typeName,
		SyncLits:  collectUBVClosureLits(declaration.Body),
		SyncCalls: collectUBVClosureVarCalls(closureCalls), MethodCalls: methodCalls,
		MaxStates: defaultCFGMaxStates, SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaResult, declaration),
	})
	if result.Class != interprocOutcomeUnsafe {
		graph := buildInterprocSupergraphFromReachableBlocksWithResolution(pass, definitionBlock, "cfg.ubv.iife-capture-probe")
		transfers := make([]string, 0)
		for _, node := range graph.Nodes {
			if node.Kind != interprocNodeKindCall {
				continue
			}
			tag, reason := ubvGraphNodeEdgeTag(
				graph, node, pass, graph.astNode(node), assigned[0].target,
				nil, nil, methodCalls, ideStateNeedsValidate, nil, &sync.Map{},
			)
			call, _ := graph.astNode(node).(*ast.CallExpr)
			transfers = append(transfers, fmt.Sprintf(
				"%s:%T@%d use=%t transfer=%s/%s",
				node.FuncKey, graph.astNode(node), graph.astNode(node).Pos(),
				exactCallUsesTarget(pass, call, assigned[0].target, methodCalls), tag, reason,
			))
		}
		t.Fatalf("IIFE captured use result = %s (%s), want unsafe; terminal=%+v witness=%+v transfers=%v edges=%+v", result.Class, result.Reason, result.WitnessTerminal, result.WitnessEdges, transfers, graph.Edges)
	}
}
