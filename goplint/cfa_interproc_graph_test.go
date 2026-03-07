// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestBuildInterprocSupergraphAddsUnresolvedCallFallback(t *testing.T) {
	t.Parallel()

	const src = `package testpkg

func entry(v string) {
	_ = len(v)
}
`
	pass, file := buildTypedPassFromSource(t, src)
	entry := findFuncDecl(t, file, "entry")

	graph := buildInterprocSupergraphForFunc(pass, entry, cfgBackendSSA)

	found := false
	for _, edge := range graph.Edges {
		if edge.Kind != interprocEdgeCallToReturn {
			continue
		}
		if edge.Reason != pathOutcomeReasonUnresolvedTarget {
			continue
		}
		found = true
		break
	}
	if !found {
		t.Fatal("expected unresolved call-to-return fallback edge")
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

	graph := buildInterprocSupergraphForFunc(pass, entry, cfgBackendSSA)
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
	cfg := buildFuncCFGForBackend(pass, entry.Body, cfgBackendSSA)
	if cfg == nil {
		t.Fatal("expected entry CFG")
	}

	entryKey := "cfg.entry"
	graph := buildInterprocSupergraphFromCFGWithResolution(pass, cfg, entryKey, cfgBackendSSA)
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
