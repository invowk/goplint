// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"sort"

	gocfg "golang.org/x/tools/go/cfg"
)

func cloneCFGPath(path []int32) []int32 {
	return append([]int32(nil), path...)
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
