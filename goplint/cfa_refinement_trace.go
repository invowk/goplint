// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const findingStreamKindRefinementTrace = "refinement-trace"

func writeRefinementTraceToSink(
	pass *analysis.Pass,
	pos token.Pos,
	result interprocPathResult,
) {
	if pass == nil || !result.PhaseC.Enabled {
		return
	}
	path := emitFindingsPathFromPass(pass)
	if path == "" {
		return
	}
	record := FindingStreamRecord{
		Kind:     findingStreamKindRefinementTrace,
		Category: result.WitnessRecord.Category,
		ID:       result.WitnessRecord.FindingID,
		Message:  result.PhaseC.RefinementStatus,
		Meta: compactFindingMeta(appendPhaseCMeta(map[string]string{
			"cfg_witness_kind": "cfg-path",
		}, result)),
	}
	if result.WitnessRecord.WitnessHash != "" {
		if record.Meta == nil {
			record.Meta = make(map[string]string)
		}
		record.Meta["cfg_refinement_witness_hash"] = result.WitnessRecord.WitnessHash
	}
	if len(result.WitnessRecord.CFGPath) > 0 {
		if record.Meta == nil {
			record.Meta = make(map[string]string)
		}
		record.Meta["cfg_witness_blocks"] = joinCFGBlocks(result.WitnessRecord.CFGPath)
	}
	if pass.Fset != nil && pos.IsValid() {
		record.Posn = pass.Fset.Position(pos).String()
	}
	writeFindingStreamRecord(path, record)
}

func joinCFGBlocks(path []int32) string {
	if len(path) == 0 {
		return ""
	}
	steps := make([]string, 0, len(path))
	for _, idx := range path {
		steps = append(steps, strconvItoa(int(idx)))
	}
	return strings.Join(steps, ",")
}
