// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"sort"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

func buildInterprocSupergraphFromCFG(cfg *gocfg.CFG, funcKey string) interprocSupergraph {
	if cfg == nil {
		return newInterprocSupergraph()
	}
	return buildInterprocSupergraphFromBlocks(cfg.Blocks, funcKey)
}

func buildInterprocSupergraphFromCFGWithResolution(
	pass *analysis.Pass,
	cfg *gocfg.CFG,
	funcKey string,
	backend string,
) interprocSupergraph {
	if cfg == nil {
		return newInterprocSupergraph()
	}
	return buildInterprocSupergraphFromBlocksWithResolution(pass, cfg.Blocks, funcKey, backend)
}

func buildInterprocSupergraphFromReachableBlocks(defBlock *gocfg.Block, funcKey string) interprocSupergraph {
	blocks := collectReachableBlocks(defBlock)
	return buildInterprocSupergraphFromBlocks(blocks, funcKey)
}

func buildInterprocSupergraphFromReachableBlocksWithResolution(
	pass *analysis.Pass,
	defBlock *gocfg.Block,
	funcKey string,
	backend string,
) interprocSupergraph {
	blocks := collectReachableBlocks(defBlock)
	return buildInterprocSupergraphFromBlocksWithResolution(pass, blocks, funcKey, backend)
}

func buildInterprocSupergraphFromBlocks(blocks []*gocfg.Block, funcKey string) interprocSupergraph {
	graph := newInterprocSupergraph()
	if len(blocks) == 0 {
		return graph
	}

	blockEntryNode := map[int32]interprocNodeID{}
	for _, block := range blocks {
		if block == nil || len(block.Nodes) == 0 {
			continue
		}
		first := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: 0, Kind: interprocNodeKindCFG}
		blockEntryNode[block.Index] = first
	}

	for _, block := range blocks {
		if block == nil || len(block.Nodes) == 0 {
			continue
		}
		for idx := range block.Nodes {
			nodeID := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: idx, Kind: interprocNodeKindCFG}
			graph.addNode(nodeID, block.Nodes[idx])
			if idx > 0 {
				prev := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: idx - 1, Kind: interprocNodeKindCFG}
				graph.addEdge(interprocEdge{From: prev, To: nodeID, Kind: interprocEdgeIntra})
			}
			if !nodeContainsCallExpr(block.Nodes[idx]) {
				continue
			}
			callNode := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: idx, Kind: interprocNodeKindCall}
			retNode := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: idx, Kind: interprocNodeKindReturn}
			graph.addEdge(interprocEdge{From: nodeID, To: callNode, Kind: interprocEdgeCall})
			graph.addEdge(interprocEdge{From: callNode, To: retNode, Kind: interprocEdgeCallToReturn, Reason: pathOutcomeReasonUnresolvedTarget})
			if idx+1 < len(block.Nodes) {
				next := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: idx + 1, Kind: interprocNodeKindCFG}
				graph.addEdge(interprocEdge{From: retNode, To: next, Kind: interprocEdgeReturn})
			}
		}

		lastNode := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: len(block.Nodes) - 1, Kind: interprocNodeKindCFG}
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
			graph.terminalCFGNodes[lastNode.Key()] = true
		}
	}
	return graph
}

func buildInterprocSupergraphFromBlocksWithResolution(
	pass *analysis.Pass,
	blocks []*gocfg.Block,
	funcKey string,
	backend string,
) interprocSupergraph {
	graph := newInterprocSupergraph()
	if len(blocks) == 0 {
		return graph
	}

	blockEntryNode := map[int32]interprocNodeID{}
	for _, block := range blocks {
		if block == nil || len(block.Nodes) == 0 {
			continue
		}
		first := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: 0, Kind: interprocNodeKindCFG}
		blockEntryNode[block.Index] = first
	}

	cache := make(map[string]*interprocFunctionSummary)
	for _, block := range blocks {
		if block == nil || len(block.Nodes) == 0 {
			continue
		}
		for idx := range block.Nodes {
			nodeID := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: idx, Kind: interprocNodeKindCFG}
			graph.addNode(nodeID, block.Nodes[idx])
			if idx > 0 {
				prev := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: idx - 1, Kind: interprocNodeKindCFG}
				graph.addEdge(interprocEdge{From: prev, To: nodeID, Kind: interprocEdgeIntra})
			}
			callExpr := firstCallExprInNode(block.Nodes[idx])
			if callExpr == nil {
				continue
			}
			callNode := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: idx, Kind: interprocNodeKindCall}
			retNode := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: idx, Kind: interprocNodeKindReturn}
			graph.addEdge(interprocEdge{From: nodeID, To: callNode, Kind: interprocEdgeCall})

			resolved := false
			if pass != nil {
				if fnObj := calledFunctionObject(pass, callExpr); fnObj != nil {
					if calleeDecl := findFuncDeclForObject(pass, fnObj); calleeDecl != nil && calleeDecl.Body != nil {
						if calleeSummary, ok := appendInterprocFunctionGraph(pass, calleeDecl, backend, &graph, cache); ok {
							graph.addEdge(interprocEdge{From: callNode, To: calleeSummary.entry, Kind: interprocEdgeCall})
							for _, exitNode := range calleeSummary.exits {
								graph.addEdge(interprocEdge{From: exitNode, To: retNode, Kind: interprocEdgeReturn})
							}
							resolved = len(calleeSummary.exits) > 0
						}
					}
				}
			}
			if !resolved {
				graph.addEdge(interprocEdge{From: callNode, To: retNode, Kind: interprocEdgeCallToReturn, Reason: pathOutcomeReasonUnresolvedTarget})
			}
			if idx+1 < len(block.Nodes) {
				next := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: idx + 1, Kind: interprocNodeKindCFG}
				graph.addEdge(interprocEdge{From: retNode, To: next, Kind: interprocEdgeReturn})
			}
		}

		lastNode := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: len(block.Nodes) - 1, Kind: interprocNodeKindCFG}
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
			graph.terminalCFGNodes[lastNode.Key()] = true
		}
	}
	return graph
}

func collectReachableBlocks(start *gocfg.Block) []*gocfg.Block {
	if start == nil {
		return nil
	}
	seen := map[int32]bool{}
	queue := []*gocfg.Block{start}
	blocks := make([]*gocfg.Block, 0)
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if block == nil || seen[block.Index] {
			continue
		}
		seen[block.Index] = true
		blocks = append(blocks, block)
		for _, succ := range block.Succs {
			if succ != nil && !seen[succ.Index] {
				queue = append(queue, succ)
			}
		}
	}
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].Index < blocks[j].Index
	})
	return blocks
}
