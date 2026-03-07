// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"sort"

	gocfg "golang.org/x/tools/go/cfg"
)

type cfgTraversalMode string

const (
	cfgTraversalModeLegacy          cfgTraversalMode = "legacy"
	cfgTraversalModeCastPath        cfgTraversalMode = "cast-path"
	cfgTraversalModeUBVOrder        cfgTraversalMode = "ubv-order"
	cfgTraversalModeUBVEscape       cfgTraversalMode = "ubv-escape"
	cfgTraversalModeConstructorPath cfgTraversalMode = "constructor-path"
)

type cfgValidationUseState string

const (
	cfgValidationStateNeedsValidate          cfgValidationUseState = "needs-validate"
	cfgValidationStateNeedsValidateBeforeUse cfgValidationUseState = "needs-validate-before-use"
)

const cfgVisitAnyPredecessor int32 = -1

type cfgVisitKey struct {
	blockIndex       int32
	predecessorIndex int32
	mode             cfgTraversalMode
	targetKey        string
	state            cfgValidationUseState
}

type cfgTraversalMemoEntry struct {
	outcome pathOutcome
	reason  pathOutcomeReason
	witness []int32 // suffix witness from the state key's block
}

type cfgTraversalContext struct {
	mode      cfgTraversalMode
	targetKey string
	state     cfgValidationUseState
	visited   map[cfgVisitKey]bool
	active    map[cfgVisitKey]bool
	memo      map[cfgVisitKey]cfgTraversalMemoEntry
	sccByID   map[int32]int
}

func newCFGTraversalContext(
	mode cfgTraversalMode,
	targetKey string,
	state cfgValidationUseState,
	cfg *gocfg.CFG,
) *cfgTraversalContext {
	var scc map[int32]int
	if cfg != nil {
		scc = buildCFGSCCIndexFromCFG(cfg)
	}
	return &cfgTraversalContext{
		mode:      mode,
		targetKey: targetKey,
		state:     state,
		visited:   make(map[cfgVisitKey]bool),
		active:    make(map[cfgVisitKey]bool),
		memo:      make(map[cfgVisitKey]cfgTraversalMemoEntry),
		sccByID:   scc,
	}
}

func newCFGTraversalContextFromBlocks(
	mode cfgTraversalMode,
	targetKey string,
	state cfgValidationUseState,
	starts []*gocfg.Block,
) *cfgTraversalContext {
	return &cfgTraversalContext{
		mode:      mode,
		targetKey: targetKey,
		state:     state,
		visited:   make(map[cfgVisitKey]bool),
		active:    make(map[cfgVisitKey]bool),
		memo:      make(map[cfgVisitKey]cfgTraversalMemoEntry),
		sccByID:   buildCFGSCCIndexFromBlocks(starts),
	}
}

func (ctx *cfgTraversalContext) key(blockIndex int32, predecessorIndex int32) cfgVisitKey {
	if ctx == nil {
		return cfgVisitKey{
			blockIndex:       blockIndex,
			predecessorIndex: predecessorIndex,
			mode:             cfgTraversalModeLegacy,
			state:            cfgValidationStateNeedsValidate,
		}
	}
	return cfgVisitKey{
		blockIndex:       blockIndex,
		predecessorIndex: predecessorIndex,
		mode:             ctx.mode,
		targetKey:        ctx.targetKey,
		state:            ctx.state,
	}
}

func (ctx *cfgTraversalContext) hasVisitState(blockIndex int32, predecessorIndex int32) bool {
	if ctx == nil || ctx.visited == nil {
		return false
	}
	if ctx.visited[ctx.key(blockIndex, predecessorIndex)] {
		return true
	}
	// Legacy behavior: state converted from map[int32]bool has wildcard predecessor.
	if ctx.mode == cfgTraversalModeLegacy {
		return ctx.visited[ctx.key(blockIndex, cfgVisitAnyPredecessor)]
	}
	return false
}

func (ctx *cfgTraversalContext) markVisitState(blockIndex int32, predecessorIndex int32) {
	if ctx == nil || ctx.visited == nil {
		return
	}
	ctx.visited[ctx.key(blockIndex, predecessorIndex)] = true
}

func (ctx *cfgTraversalContext) shouldSkip(blockIndex int32, predecessorIndex int32) bool {
	if ctx == nil {
		return false
	}
	key := ctx.key(blockIndex, predecessorIndex)
	if ctx.active[key] {
		return true
	}
	if !ctx.hasVisitState(blockIndex, predecessorIndex) {
		return false
	}
	// Cycle-aware relaxation: permit revisiting inside the same SCC so the
	// traversal can still discover alternate exits within cyclic regions.
	if predecessorIndex != cfgVisitAnyPredecessor && ctx.sameSCC(blockIndex, predecessorIndex) {
		return false
	}
	return true
}

func (ctx *cfgTraversalContext) sameSCC(a int32, b int32) bool {
	if ctx == nil || len(ctx.sccByID) == 0 {
		return false
	}
	sccA, okA := ctx.sccByID[a]
	sccB, okB := ctx.sccByID[b]
	return okA && okB && sccA == sccB
}

func (ctx *cfgTraversalContext) memoLookup(blockIndex int32, predecessorIndex int32) (cfgTraversalMemoEntry, bool) {
	if ctx == nil || ctx.memo == nil {
		return cfgTraversalMemoEntry{}, false
	}
	entry, ok := ctx.memo[ctx.key(blockIndex, predecessorIndex)]
	return entry, ok
}

func (ctx *cfgTraversalContext) memoStore(
	blockIndex int32,
	predecessorIndex int32,
	outcome pathOutcome,
	reason pathOutcomeReason,
	witness []int32,
) {
	if ctx == nil || ctx.memo == nil {
		return
	}
	ctx.memo[ctx.key(blockIndex, predecessorIndex)] = cfgTraversalMemoEntry{
		outcome: outcome,
		reason:  reason,
		witness: cfgWitnessSuffixFromBlock(witness, blockIndex),
	}
}

func (ctx *cfgTraversalContext) pushActive(blockIndex int32, predecessorIndex int32) cfgVisitKey {
	key := ctx.key(blockIndex, predecessorIndex)
	if ctx != nil && ctx.active != nil {
		ctx.active[key] = true
	}
	return key
}

func (ctx *cfgTraversalContext) popActive(key cfgVisitKey) {
	if ctx == nil || ctx.active == nil {
		return
	}
	delete(ctx.active, key)
}

func cfgVisitStateFromBlockVisited(
	visited map[int32]bool,
	mode cfgTraversalMode,
	targetKey string,
	state cfgValidationUseState,
) map[cfgVisitKey]bool {
	out := make(map[cfgVisitKey]bool, len(visited))
	for blockIndex, seen := range visited {
		if !seen {
			continue
		}
		out[cfgVisitKey{
			blockIndex:       blockIndex,
			predecessorIndex: cfgVisitAnyPredecessor,
			mode:             mode,
			targetKey:        targetKey,
			state:            state,
		}] = true
	}
	return out
}

func mergeCFGWitness(prefix []int32, suffix []int32) []int32 {
	if len(suffix) == 0 {
		return cloneCFGPath(prefix)
	}
	out := cloneCFGPath(prefix)
	out = append(out, suffix...)
	return out
}

func cfgWitnessSuffixFromBlock(witness []int32, blockIndex int32) []int32 {
	if len(witness) == 0 {
		return nil
	}
	for idx, current := range witness {
		if current == blockIndex {
			return cloneCFGPath(witness[idx:])
		}
	}
	return cloneCFGPath(witness)
}

func buildCFGSCCIndexFromCFG(cfg *gocfg.CFG) map[int32]int {
	if cfg == nil || len(cfg.Blocks) == 0 {
		return nil
	}
	return buildCFGSCCIndexFromBlocks(cfg.Blocks)
}

func buildCFGSCCIndexFromBlocks(starts []*gocfg.Block) map[int32]int {
	if len(starts) == 0 {
		return nil
	}
	nodes := collectReachableCFGBlocks(starts)
	if len(nodes) == 0 {
		return nil
	}

	indexByBlock := make(map[int32]int, len(nodes))
	lowlinkByBlock := make(map[int32]int, len(nodes))
	onStack := make(map[int32]bool, len(nodes))
	stack := make([]int32, 0, len(nodes))
	sccByBlock := make(map[int32]int, len(nodes))
	nextIndex := 0
	nextSCC := 0

	var strongConnect func(block *gocfg.Block)
	strongConnect = func(block *gocfg.Block) {
		blockIdx := block.Index
		indexByBlock[blockIdx] = nextIndex
		lowlinkByBlock[blockIdx] = nextIndex
		nextIndex++
		stack = append(stack, blockIdx)
		onStack[blockIdx] = true

		succs := sortedCFGSuccs(block.Succs)
		for _, succ := range succs {
			if succ == nil {
				continue
			}
			succIdx := succ.Index
			if _, seen := indexByBlock[succIdx]; !seen {
				strongConnect(succ)
				if lowlinkByBlock[succIdx] < lowlinkByBlock[blockIdx] {
					lowlinkByBlock[blockIdx] = lowlinkByBlock[succIdx]
				}
				continue
			}
			if onStack[succIdx] && indexByBlock[succIdx] < lowlinkByBlock[blockIdx] {
				lowlinkByBlock[blockIdx] = indexByBlock[succIdx]
			}
		}

		if lowlinkByBlock[blockIdx] != indexByBlock[blockIdx] {
			return
		}
		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[top] = false
			sccByBlock[top] = nextSCC
			if top == blockIdx {
				break
			}
		}
		nextSCC++
	}

	for _, block := range nodes {
		if _, seen := indexByBlock[block.Index]; seen {
			continue
		}
		strongConnect(block)
	}
	return sccByBlock
}

func collectReachableCFGBlocks(starts []*gocfg.Block) []*gocfg.Block {
	if len(starts) == 0 {
		return nil
	}
	seen := make(map[int32]*gocfg.Block, len(starts))
	queue := make([]*gocfg.Block, 0, len(starts))
	for _, block := range starts {
		if block == nil {
			continue
		}
		if _, exists := seen[block.Index]; exists {
			continue
		}
		seen[block.Index] = block
		queue = append(queue, block)
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, succ := range current.Succs {
			if succ == nil {
				continue
			}
			if _, exists := seen[succ.Index]; exists {
				continue
			}
			seen[succ.Index] = succ
			queue = append(queue, succ)
		}
	}
	blocks := make([]*gocfg.Block, 0, len(seen))
	for _, block := range seen {
		blocks = append(blocks, block)
	}
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].Index < blocks[j].Index
	})
	return blocks
}

func sortedCFGSuccs(succs []*gocfg.Block) []*gocfg.Block {
	if len(succs) < 2 {
		return succs
	}
	out := make([]*gocfg.Block, 0, len(succs))
	out = append(out, succs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i] == nil {
			return false
		}
		if out[j] == nil {
			return true
		}
		return out[i].Index < out[j].Index
	})
	return out
}
