// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

type interprocNodeKind string

const (
	interprocNodeKindCFG    interprocNodeKind = "cfg-node"
	interprocNodeKindCall   interprocNodeKind = "call-site"
	interprocNodeKindReturn interprocNodeKind = "return-site"
)

type interprocNodeID struct {
	FuncKey    string
	BlockIndex int32
	NodeIndex  int
	Kind       interprocNodeKind
}

func (id interprocNodeID) Key() string {
	return fmt.Sprintf("%s|%s|%d|%d", id.FuncKey, id.Kind, id.BlockIndex, id.NodeIndex)
}

type interprocEdgeKind string

const (
	interprocEdgeIntra        interprocEdgeKind = "intra"
	interprocEdgeCall         interprocEdgeKind = "call"
	interprocEdgeReturn       interprocEdgeKind = "return"
	interprocEdgeCallToReturn interprocEdgeKind = "call-to-return"
)

type interprocEdge struct {
	From   interprocNodeID
	To     interprocNodeID
	Kind   interprocEdgeKind
	Reason pathOutcomeReason
}

type interprocSupergraph struct {
	Nodes            map[string]interprocNodeID
	NodeAST          map[string]ast.Node
	Edges            []interprocEdge
	terminalCFGNodes map[string]bool
}

func newInterprocSupergraph() interprocSupergraph {
	return interprocSupergraph{
		Nodes:            make(map[string]interprocNodeID),
		NodeAST:          make(map[string]ast.Node),
		terminalCFGNodes: make(map[string]bool),
	}
}

func (g *interprocSupergraph) addNode(id interprocNodeID, node ast.Node) {
	if g == nil {
		return
	}
	if g.Nodes == nil {
		g.Nodes = make(map[string]interprocNodeID)
	}
	key := id.Key()
	g.Nodes[key] = id
	if node != nil {
		if g.NodeAST == nil {
			g.NodeAST = make(map[string]ast.Node)
		}
		g.NodeAST[key] = node
	}
}

func (g *interprocSupergraph) addEdge(edge interprocEdge) {
	if g == nil {
		return
	}
	g.addNode(edge.From, nil)
	g.addNode(edge.To, nil)
	g.Edges = append(g.Edges, edge)
}

func (g interprocSupergraph) outgoing(from interprocNodeID) []interprocEdge {
	if len(g.Edges) == 0 {
		return nil
	}
	key := from.Key()
	out := make([]interprocEdge, 0)
	for _, edge := range g.Edges {
		if edge.From.Key() == key {
			out = append(out, edge)
		}
	}
	return out
}

func (g interprocSupergraph) astNode(id interprocNodeID) ast.Node {
	return g.NodeAST[id.Key()]
}

func (g interprocSupergraph) isTerminalCFGNode(id interprocNodeID) bool {
	if id.Kind != interprocNodeKindCFG {
		return false
	}
	return g.terminalCFGNodes[id.Key()]
}

type interprocFunctionSummary struct {
	entry    interprocNodeID
	exits    []interprocNodeID
	building bool
	built    bool
}

func buildInterprocSupergraphForFunc(pass *analysis.Pass, fnDecl *ast.FuncDecl, backend string) interprocSupergraph {
	graph := newInterprocSupergraph()
	cache := make(map[string]*interprocFunctionSummary)
	_, _ = appendInterprocFunctionGraph(pass, fnDecl, backend, &graph, cache)
	return graph
}

func appendInterprocFunctionGraph(
	pass *analysis.Pass,
	fnDecl *ast.FuncDecl,
	backend string,
	graph *interprocSupergraph,
	cache map[string]*interprocFunctionSummary,
) (interprocFunctionSummary, bool) {
	if graph == nil || fnDecl == nil || fnDecl.Body == nil {
		return interprocFunctionSummary{}, false
	}
	funcKey := interprocFunctionKey(pass, fnDecl)
	if funcKey == "" {
		return interprocFunctionSummary{}, false
	}
	if cached, ok := cache[funcKey]; ok {
		if cached.building {
			// Recursive/self calls are modeled conservatively via unresolved fallback.
			return interprocFunctionSummary{}, false
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

	cfg := buildFuncCFGForBackend(pass, fnDecl.Body, backend)
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

	for _, block := range cfg.Blocks {
		if block == nil || len(block.Nodes) == 0 {
			continue
		}
		for idx := range block.Nodes {
			nodeID := interprocNodeID{
				FuncKey:    funcKey,
				BlockIndex: block.Index,
				NodeIndex:  idx,
				Kind:       interprocNodeKindCFG,
			}
			graph.addNode(nodeID, block.Nodes[idx])
			if idx > 0 {
				prev := interprocNodeID{
					FuncKey:    funcKey,
					BlockIndex: block.Index,
					NodeIndex:  idx - 1,
					Kind:       interprocNodeKindCFG,
				}
				graph.addEdge(interprocEdge{From: prev, To: nodeID, Kind: interprocEdgeIntra})
			}

			callExpr := firstCallExprInNode(block.Nodes[idx])
			if callExpr == nil {
				continue
			}
			callNode := interprocNodeID{
				FuncKey:    funcKey,
				BlockIndex: block.Index,
				NodeIndex:  idx,
				Kind:       interprocNodeKindCall,
			}
			retNode := interprocNodeID{
				FuncKey:    funcKey,
				BlockIndex: block.Index,
				NodeIndex:  idx,
				Kind:       interprocNodeKindReturn,
			}
			graph.addEdge(interprocEdge{From: nodeID, To: callNode, Kind: interprocEdgeCall})
			resolved := false
			if fnObj := calledFunctionObject(pass, callExpr); fnObj != nil {
				if calleeDecl := findFuncDeclForObject(pass, fnObj); calleeDecl != nil && calleeDecl.Body != nil {
					if calleeSummary, ok := appendInterprocFunctionGraph(pass, calleeDecl, backend, graph, cache); ok {
						graph.addEdge(interprocEdge{From: callNode, To: calleeSummary.entry, Kind: interprocEdgeCall})
						for _, exitNode := range calleeSummary.exits {
							graph.addEdge(interprocEdge{From: exitNode, To: retNode, Kind: interprocEdgeReturn})
						}
						resolved = len(calleeSummary.exits) > 0
					}
				}
			}
			if !resolved {
				graph.addEdge(interprocEdge{
					From:   callNode,
					To:     retNode,
					Kind:   interprocEdgeCallToReturn,
					Reason: pathOutcomeReasonUnresolvedTarget,
				})
			}
			if idx+1 < len(block.Nodes) {
				next := interprocNodeID{
					FuncKey:    funcKey,
					BlockIndex: block.Index,
					NodeIndex:  idx + 1,
					Kind:       interprocNodeKindCFG,
				}
				graph.addEdge(interprocEdge{From: retNode, To: next, Kind: interprocEdgeReturn})
			}
		}

		lastNode := interprocNodeID{
			FuncKey:    funcKey,
			BlockIndex: block.Index,
			NodeIndex:  len(block.Nodes) - 1,
			Kind:       interprocNodeKindCFG,
		}
		hasMappedSucc := false
		for _, succ := range block.Succs {
			if succ == nil {
				continue
			}
			entry, ok := blockEntryNode[succ.Index]
			if !ok {
				continue
			}
			hasMappedSucc = true
			graph.addEdge(interprocEdge{From: lastNode, To: entry, Kind: interprocEdgeIntra})
		}
		if !hasMappedSucc {
			if graph.terminalCFGNodes == nil {
				graph.terminalCFGNodes = make(map[string]bool)
			}
			graph.terminalCFGNodes[lastNode.Key()] = true
		}
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

	exits := make([]interprocNodeID, 0)
	for _, block := range cfg.Blocks {
		if block == nil || len(block.Nodes) == 0 {
			continue
		}
		hasMappedSucc := false
		for _, succ := range block.Succs {
			if succ == nil {
				continue
			}
			if _, ok := blockEntryNode[succ.Index]; ok {
				hasMappedSucc = true
				break
			}
		}
		if hasMappedSucc {
			continue
		}
		exits = append(exits, interprocNodeID{
			FuncKey:    funcKey,
			BlockIndex: block.Index,
			NodeIndex:  len(block.Nodes) - 1,
			Kind:       interprocNodeKindCFG,
		})
	}
	summary.entry = entry
	summary.exits = exits
	summary.built = true
	return *summary, true
}

func nodeContainsCallExpr(node ast.Node) bool {
	return firstCallExprInNode(node) != nil
}

func firstCallExprInNode(node ast.Node) *ast.CallExpr {
	if node == nil {
		return nil
	}
	var callExpr *ast.CallExpr
	ast.Inspect(node, func(n ast.Node) bool {
		if callExpr != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if ok {
			callExpr = call
			return false
		}
		return true
	})
	return callExpr
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
		return fmt.Sprintf("%s.%s@%d", pass.Pkg.Path(), fnDecl.Name.Name, fnDecl.Pos())
	}
	return fmt.Sprintf("%s@%d", fnDecl.Name.Name, fnDecl.Pos())
}
