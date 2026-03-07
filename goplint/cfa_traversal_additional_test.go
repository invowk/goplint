// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestCFGVisitKeyIncludesModeTargetAndState(t *testing.T) {
	t.Parallel()

	castCtx := newCFGTraversalContext(
		cfgTraversalModeCastPath,
		"pkg.Type",
		cfgValidationStateNeedsValidate,
		nil,
	)
	ubvCtx := newCFGTraversalContext(
		cfgTraversalModeUBVEscape,
		"pkg.Type",
		cfgValidationStateNeedsValidateBeforeUse,
		nil,
	)
	keyA := castCtx.key(10, 2)
	keyB := ubvCtx.key(10, 2)
	if keyA == keyB {
		t.Fatal("expected visit key to differ across mode/state dimensions")
	}
}

func TestAdaptiveBlockVisitBudgetScalesByCFGSize(t *testing.T) {
	t.Parallel()

	_, cfg := parseFuncBody(t, `
package p

func f(v string) {
	x := v
	if len(v) > 0 {
		x = v + "a"
	}
	if len(v) > 1 {
		x = v + "b"
	}
	if len(v) > 2 {
		x = v + "c"
	}
	if len(v) > 3 {
		x = v + "d"
	}
	_ = x
}
`)
	requested := blockVisitBudget{maxStates: 32, maxDepth: 8}
	effective := adaptiveBlockVisitBudget(cfg, requested)
	if effective.maxStates <= requested.maxStates {
		t.Fatalf("expected adaptive maxStates > %d, got %d", requested.maxStates, effective.maxStates)
	}
}

func TestBuildCFGSCCIndexFromBlocksDeterministic(t *testing.T) {
	t.Parallel()

	_, cfg := parseFuncBody(t, `
package p

func f(v int) int {
	for v > 0 {
		v--
	}
	return v
}
`)
	first := buildCFGSCCIndexFromBlocks(cfg.Blocks)
	second := buildCFGSCCIndexFromBlocks(cfg.Blocks)
	if len(first) == 0 {
		t.Fatal("expected non-empty SCC index")
	}
	if len(first) != len(second) {
		t.Fatalf("SCC index size mismatch: %d vs %d", len(first), len(second))
	}
	for block, sccID := range first {
		if second[block] != sccID {
			t.Fatalf("SCC mismatch for block %d: %d vs %d", block, sccID, second[block])
		}
	}
}

func TestDeepCFGBudgetDeterministicWitness(t *testing.T) {
	t.Parallel()

	body, cfg := parseFuncBody(t, `
package p

func f(v string) error {
	x := CommandName(v)
	if len(v) > 0 {
		if v[0] == 'a' {
			_ = v
		}
	}
	if len(v) > 1 {
		if v[1] == 'b' {
			_ = v
		}
	}
	if len(v) > 2 {
		if v[2] == 'c' {
			_ = v
		}
	}
	if len(v) > 3 {
		if v[3] == 'd' {
			_ = v
		}
	}
	return x.Validate()
}
`)
	assign := body.List[0].(*ast.AssignStmt)
	defBlock, defIdx := findDefiningBlock(cfg, assign)
	target := newCastTargetFromName("x")
	if defBlock == nil {
		t.Fatal("defBlock not found")
	}
	run := func() (pathOutcome, pathOutcomeReason, []int32) {
		return hasPathToReturnWithoutValidateOutcomeWithWitness(
			nil,
			cfg,
			defBlock,
			defIdx,
			target,
			nil,
			nil,
			nil,
			nil,
			3,
			1,
		)
	}
	outcomeA, reasonA, witnessA := run()
	outcomeB, reasonB, witnessB := run()
	if outcomeA != pathOutcomeInconclusive || reasonA != pathOutcomeReasonDepthBudget {
		t.Fatalf("first run outcome/reason = %v/%v, want inconclusive/depth-budget", outcomeA, reasonA)
	}
	if outcomeA != outcomeB || reasonA != reasonB {
		t.Fatalf("outcome mismatch across runs: %v/%v vs %v/%v", outcomeA, reasonA, outcomeB, reasonB)
	}
	if len(witnessA) != len(witnessB) {
		t.Fatalf("witness length mismatch: %d vs %d", len(witnessA), len(witnessB))
	}
	for idx := range witnessA {
		if witnessA[idx] != witnessB[idx] {
			t.Fatalf("witness mismatch at %d: %d vs %d", idx, witnessA[idx], witnessB[idx])
		}
	}
}
