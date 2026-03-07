// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	gocfg "golang.org/x/tools/go/cfg"
)

const (
	maxAdaptiveBudgetMultiplier = 4
	minAdaptiveStatesPerBlock   = 16
	minAdaptiveDepthPerBlock    = 2
)

func adaptiveBlockVisitBudget(cfg *gocfg.CFG, requested blockVisitBudget) blockVisitBudget {
	if cfg == nil || len(cfg.Blocks) == 0 {
		return requested
	}
	blockCount := len(collectReachableCFGBlocks(cfg.Blocks))
	if blockCount == 0 {
		blockCount = len(cfg.Blocks)
	}
	return adaptiveBlockVisitBudgetByBlockCount(blockCount, requested)
}

func adaptiveBlockVisitBudgetForBlocks(starts []*gocfg.Block, requested blockVisitBudget) blockVisitBudget {
	blockCount := len(collectReachableCFGBlocks(starts))
	if blockCount == 0 {
		return requested
	}
	return adaptiveBlockVisitBudgetByBlockCount(blockCount, requested)
}

func adaptiveBlockVisitBudgetByBlockCount(blockCount int, requested blockVisitBudget) blockVisitBudget {
	effective := requested
	effective.maxStates = adaptiveBudgetValue(
		requested.maxStates,
		blockCount*minAdaptiveStatesPerBlock,
	)
	effective.maxDepth = adaptiveBudgetValue(
		requested.maxDepth,
		blockCount*minAdaptiveDepthPerBlock,
	)
	return effective
}

func adaptiveBudgetValue(requested int, sizeFloor int) int {
	if requested <= 0 {
		return requested
	}
	// Keep intentionally tiny budgets intact so targeted tests/diagnostics can
	// still force inconclusive outcomes deterministically.
	if requested <= 8 {
		return requested
	}
	effective := max(requested, sizeFloor)
	hardCap := requested * maxAdaptiveBudgetMultiplier
	if effective > hardCap {
		effective = hardCap
	}
	return effective
}
