// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestBuildInterprocSupergraphOrdersNestedCalls(t *testing.T) {
	t.Parallel()

	const src = `package testpkg

func mutate(value *string) string {
	*value = "mutated"
	return *value
}

func consume(string) {}

func entry(value string) {
	consume(mutate(&value))
}
`
	pass, file := buildTypedPassFromSource(t, src)
	entry := findFuncDecl(t, file, "entry")
	graph := buildInterprocSupergraphForFunc(pass, entry, buildSSAForPass(pass))
	entryKey := interprocFunctionKey(pass, entry)

	var calls []interprocNodeID
	for _, node := range graph.Nodes {
		if node.FuncKey == entryKey && node.Kind == interprocNodeKindCall {
			calls = append(calls, node)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("entry call micro-node count = %d, want 2", len(calls))
	}

	inner := interprocNodeID{
		FuncKey:    entryKey,
		BlockIndex: calls[0].BlockIndex,
		NodeIndex:  calls[0].NodeIndex,
		Kind:       interprocNodeKindCall,
	}
	outer := inner
	inner.CallOrdinal = 0
	outer.CallOrdinal = 1
	innerReturn := inner
	innerReturn.Kind = interprocNodeKindReturn

	foundOrder := false
	for _, edge := range graph.Edges {
		if edge.From.Key() == innerReturn.Key() && edge.To.Key() == outer.Key() {
			foundOrder = true
			break
		}
	}
	if !foundOrder {
		t.Fatalf("nested calls are not sequenced inner-to-outer: %q -> %q", innerReturn.Key(), outer.Key())
	}
}

func TestBuildInterprocSupergraphKeepsCallAndNonCallEffectsDistinct(t *testing.T) {
	t.Parallel()

	const src = `package testpkg

func mutate(value *string) { *value = "mutated" }

func entry(value string) {
	go mutate(&value)
	_ = value
}
`
	pass, file := buildTypedPassFromSource(t, src)
	entry := findFuncDecl(t, file, "entry")
	graph := buildInterprocSupergraphForFunc(pass, entry, buildSSAForPass(pass))
	entryKey := interprocFunctionKey(pass, entry)

	var goCall interprocNodeID
	for _, node := range graph.Nodes {
		event, ok := graph.callEvent(node)
		if node.FuncKey == entryKey && node.Kind == interprocNodeKindCall && ok &&
			event.Phase == protocolCallEventGo {
			goCall = node
			break
		}
	}
	if goCall.FuncKey == "" {
		t.Fatal("go call has no exact call-event micro-node")
	}
	if _, ok := graph.astNode(goCall).(*ast.CallExpr); !ok {
		t.Fatalf("call micro-node AST = %T, want *ast.CallExpr", graph.astNode(goCall))
	}
	cfgNode := goCall
	cfgNode.Kind = interprocNodeKindCFG
	cfgNode.CallOrdinal = 0
	if _, ok := graph.astNode(cfgNode).(*ast.GoStmt); !ok {
		t.Fatalf("CFG non-call AST = %T, want *ast.GoStmt", graph.astNode(cfgNode))
	}
}

func TestBuildInterprocSupergraphClassifiesBuiltinCall(t *testing.T) {
	t.Parallel()

	const src = `package testpkg

func entry(v string) {
	_ = len(v)
}
`
	pass, file := buildTypedPassFromSource(t, src)
	entry := findFuncDecl(t, file, "entry")

	graph := buildInterprocSupergraphForFunc(pass, entry, buildSSAForPass(pass))

	found := false
	for _, edge := range graph.Edges {
		if edge.Kind != interprocEdgeCallToReturn {
			continue
		}
		if edge.Reason != pathOutcomeReasonNone {
			continue
		}
		found = true
		break
	}
	if !found {
		t.Fatal("expected classified builtin call-to-return edge")
	}
}

func TestBuildInterprocSupergraphLinksResolvedCalleeEntryAndReturn(t *testing.T) {
	t.Parallel()

	const src = `package testpkg

func helper(v string) {
	_ = v
}

func entry(v string) {
	helper(v)
}
`
	pass, file := buildTypedPassFromSource(t, src)
	entry := findFuncDecl(t, file, "entry")
	helper := findFuncDecl(t, file, "helper")

	graph := buildInterprocSupergraphForFunc(pass, entry, buildSSAForPass(pass))
	entryKey := interprocFunctionKey(pass, entry)
	helperKey := interprocFunctionKey(pass, helper)

	callSite := interprocNodeID{
		FuncKey:    entryKey,
		BlockIndex: 0,
		NodeIndex:  0,
		Kind:       interprocNodeKindCall,
	}
	calleeEntry := interprocNodeID{
		FuncKey:    helperKey,
		BlockIndex: 0,
		NodeIndex:  0,
		Kind:       interprocNodeKindCFG,
	}
	callToCallee := false
	for _, edge := range graph.outgoing(callSite) {
		if edge.Kind == interprocEdgeCall && edge.To.Key() == calleeEntry.Key() {
			callToCallee = true
			break
		}
	}
	if !callToCallee {
		t.Fatalf("expected call edge from %q to callee entry %q", callSite.Key(), calleeEntry.Key())
	}

	retSite := interprocNodeID{
		FuncKey:    entryKey,
		BlockIndex: 0,
		NodeIndex:  0,
		Kind:       interprocNodeKindReturn,
	}
	exitToReturn := false
	for _, edge := range graph.Edges {
		if edge.Kind != interprocEdgeReturn {
			continue
		}
		if edge.From.FuncKey != helperKey {
			continue
		}
		if edge.To.Key() == retSite.Key() {
			exitToReturn = true
			break
		}
	}
	if !exitToReturn {
		t.Fatalf("expected at least one callee return edge back to return site %q", retSite.Key())
	}

	for _, edge := range graph.outgoing(callSite) {
		if edge.Kind == interprocEdgeCallToReturn {
			t.Fatal("did not expect unresolved call-to-return fallback for resolved callee")
		}
	}
}

func TestBuildInterprocSupergraphSummarizesValidateCall(t *testing.T) {
	t.Parallel()

	const src = `package testpkg

type Value string

func (v Value) Validate() error {
	if v == "" {
		return nil
	}
	helper(v)
	return nil
}

func helper(Value) {}

func entry(v Value) error {
	if err := v.Validate(); err != nil {
		return err
	}
	return nil
}
`
	pass, file := buildTypedPassFromSource(t, src)
	entry := findFuncDecl(t, file, "entry")
	validate := findFuncDecl(t, file, "Validate")
	graph := buildInterprocSupergraphForFunc(pass, entry, buildSSAForPass(pass))
	validateKey := interprocFunctionKey(pass, validate)

	var validationCall interprocNodeID
	for _, node := range graph.Nodes {
		event, ok := graph.callEvent(node)
		if node.Kind != interprocNodeKindCall || !ok || event.Instruction == nil {
			continue
		}
		if _, ok := protocolValidateReceiver(event.Instruction.Common()); ok {
			validationCall = node
			break
		}
	}
	if validationCall.FuncKey == "" {
		t.Fatal("expected Validate call micro-node")
	}

	foundCallToReturn := false
	for _, edge := range graph.outgoing(validationCall) {
		switch edge.Kind {
		case interprocEdgeIntra, interprocEdgeReturn:
			// Only call-related edges carry assertions in this test.
		case interprocEdgeCallToReturn:
			foundCallToReturn = true
		case interprocEdgeCall:
			if edge.To.FuncKey == validateKey {
				t.Fatal("Validate body must not be expanded into the caller supergraph")
			}
		}
	}
	if !foundCallToReturn {
		t.Fatal("expected atomic Validate call-to-return edge")
	}
	for _, node := range graph.Nodes {
		if node.FuncKey == validateKey {
			t.Fatal("Validate implementation nodes must not be present in the caller supergraph")
		}
	}
}

func TestBuildInterprocSupergraphFromCFGWithResolutionLinksResolvedCallee(t *testing.T) {
	t.Parallel()

	const src = `package testpkg

func helper(v string) {
	_ = v
}

func entry(v string) {
	helper(v)
}
`
	pass, file := buildTypedPassFromSource(t, src)
	entry := findFuncDecl(t, file, "entry")
	helper := findFuncDecl(t, file, "helper")
	cfg := buildProtocolCFG(pass, entry.Body, buildSSAForPass(pass))
	if cfg == nil {
		t.Fatal("expected entry CFG")
	}

	entryKey := "cfg.entry"
	graph := buildInterprocSupergraphFromCFGWithResolution(pass, cfg, entryKey)
	helperKey := interprocFunctionKey(pass, helper)

	callSite := interprocNodeID{
		FuncKey:    entryKey,
		BlockIndex: 0,
		NodeIndex:  0,
		Kind:       interprocNodeKindCall,
	}
	calleeEntry := interprocNodeID{
		FuncKey:    helperKey,
		BlockIndex: 0,
		NodeIndex:  0,
		Kind:       interprocNodeKindCFG,
	}
	foundResolvedEdge := false
	for _, edge := range graph.outgoing(callSite) {
		if edge.Kind == interprocEdgeCall && edge.To.Key() == calleeEntry.Key() {
			foundResolvedEdge = true
		}
		if edge.Kind == interprocEdgeCallToReturn {
			t.Fatal("did not expect unresolved fallback edge for cfg graph with resolvable helper")
		}
	}
	if !foundResolvedEdge {
		t.Fatalf("expected resolved call edge from %q to %q", callSite.Key(), calleeEntry.Key())
	}
}

func TestBuildInterprocSupergraphLinksIIFEProcedure(t *testing.T) {
	t.Parallel()

	const src = `package testpkg

func entry(value string) {
	func() { _ = value }()
}
`
	pass, file := buildTypedPassFromSource(t, src)
	entry := findFuncDecl(t, file, "entry")
	ssaResult := buildSSAForPass(pass)
	graph := buildInterprocSupergraphForFunc(pass, entry, ssaResult)
	procedureIndex := buildProtocolProcedureIndex(pass, ssaResult)

	var closure protocolProcedure
	for _, candidate := range procedureIndex.byLiteral {
		closure = candidate
		break
	}
	if closure.Key == "" {
		t.Fatal("closure procedure was not indexed")
	}
	if !closure.CaptureExact || len(closure.Captures) != 1 {
		t.Fatalf("closure capture mapping = exact:%t bindings:%d, want one exact binding", closure.CaptureExact, len(closure.Captures))
	}

	linked := false
	for _, edge := range graph.Edges {
		if edge.Kind == interprocEdgeCall && edge.To.FuncKey == closure.Key {
			linked = true
			break
		}
	}
	if !linked {
		t.Fatalf("IIFE closure %q is not linked as an interprocedural callee; edges=%+v", closure.Key, graph.Edges)
	}
}
