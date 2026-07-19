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
) interprocSupergraph {
	if cfg == nil {
		return newInterprocSupergraph()
	}
	return buildInterprocSupergraphFromBlocksWithResolution(pass, cfg.Blocks, funcKey)
}

func buildInterprocSupergraphFromReachableBlocks(defBlock *gocfg.Block, funcKey string) interprocSupergraph {
	blocks := collectReachableBlocks(defBlock)
	return buildInterprocSupergraphFromBlocks(blocks, funcKey)
}

func buildInterprocSupergraphFromReachableBlocksWithResolution(
	pass *analysis.Pass,
	defBlock *gocfg.Block,
	funcKey string,
) interprocSupergraph {
	blocks := collectReachableBlocks(defBlock)
	return buildInterprocSupergraphFromBlocksWithResolution(pass, blocks, funcKey)
}

func buildInterprocSupergraphFromBlocks(blocks []*gocfg.Block, funcKey string) interprocSupergraph {
	return buildInterprocSupergraphFromBlocksCanonical(nil, blocks, funcKey, false)
}

func buildInterprocSupergraphFromBlocksWithResolution(
	pass *analysis.Pass,
	blocks []*gocfg.Block,
	funcKey string,
) interprocSupergraph {
	return buildInterprocSupergraphFromBlocksCanonical(pass, blocks, funcKey, true)
}

func buildInterprocSupergraphFromBlocksCanonical(
	pass *analysis.Pass,
	blocks []*gocfg.Block,
	funcKey string,
	resolve bool,
) interprocSupergraph {
	graph := newInterprocSupergraph()
	if len(blocks) == 0 {
		return graph
	}
	var ssaRes *ssaResult
	if resolve && pass != nil {
		ssaRes = buildSSAForPass(pass)
		graph.callEventIndex = buildProtocolCallEventIndex(pass, ssaRes)
		graph.procedureIndex = buildProtocolProcedureIndex(pass, ssaRes)
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
	noReturnCalls := noReturnCallResolver{ssa: ssaRes}
	predicateValues := buildCFGSSAValueIndexFromResult(noReturnCalls.ssa)
	mayReturn := computeProtocolMayReturn(pass, noReturnCalls)
	for key, returns := range mayReturn {
		if !returns {
			graph.nonReturningFunctions[key] = true
		}
	}
	for _, block := range blocks {
		if block == nil || len(block.Nodes) == 0 {
			continue
		}
		continuationNode := interprocNodeID{}
		canContinue := false
		for nodeIndex, node := range block.Nodes {
			nodeID := interprocNodeID{FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: nodeIndex, Kind: interprocNodeKindCFG}
			graph.addNode(nodeID, node)
			if canContinue {
				graph.addEdge(interprocEdge{From: continuationNode, To: nodeID, Kind: interprocEdgeIntra})
			}
			continuationNode = nodeID
			canContinue = true
			for ordinal, event := range graph.callEventIndex.eventsForNode(pass, funcKey, node) {
				callNode := interprocNodeID{
					FuncKey: funcKey, BlockIndex: block.Index, NodeIndex: nodeIndex,
					CallOrdinal: ordinal, Kind: interprocNodeKindCall,
				}
				returnNode := callNode
				returnNode.Kind = interprocNodeKindReturn
				graph.addCallNode(callNode, event)
				graph.addCallNode(returnNode, event)
				graph.addNode(returnNode, node)
				graph.addEdge(interprocEdge{From: continuationNode, To: callNode, Kind: interprocEdgeIntra})
				if resolve {
					canContinue = appendInterprocCallEvent(
						pass, event, callNode, returnNode, &graph, cache, mayReturn, noReturnCalls, predicateValues,
					)
				} else {
					reason := pathOutcomeReasonUnresolvedTarget
					if event.Phase != protocolCallEventSync || protocolCallIsBuiltin(event) {
						reason = pathOutcomeReasonNone
					}
					if !event.Mapped {
						reason = pathOutcomeReasonCallMapping
					}
					graph.addEdge(interprocEdge{
						From: callNode, To: returnNode, Kind: interprocEdgeCallToReturn,
						CallSite: callNode.Key(), Reason: reason,
					})
				}
				if !canContinue {
					break
				}
				continuationNode = returnNode
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
			graph.terminalCFGNodes[continuationNode.Key()] = true
			graph.functionExitNodes[continuationNode.Key()] = true
		}
	}
	return graph
}

func nonEmptySuccessorEntries(
	start *gocfg.Block,
	entries map[int32]interprocNodeID,
) []interprocNodeID {
	if start == nil {
		return nil
	}
	seenBlocks := make(map[int32]bool)
	seenEntries := make(map[string]bool)
	queue := []*gocfg.Block{start}
	result := make([]interprocNodeID, 0, 1)
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if block == nil || seenBlocks[block.Index] {
			continue
		}
		seenBlocks[block.Index] = true
		if entry, ok := entries[block.Index]; ok {
			if !seenEntries[entry.Key()] {
				seenEntries[entry.Key()] = true
				result = append(result, entry)
			}
			continue
		}
		queue = append(queue, block.Succs...)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Key() < result[right].Key()
	})
	return result
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
