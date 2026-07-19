// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestProtocolValidationContinueGuardsCurrentIteration(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (v Value) Validate() error { return nil }
func consume(Value) {}
func Loop(values []string) {
	for _, raw := range values {
		value := Value(raw)
		if err := value.Validate(); err != nil {
			continue
		}
		consume(value)
	}
}
`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "Loop")
	ssaResult := buildSSAForPass(pass)
	program := buildProtocolValidationProgram(pass, ssaResult, nil)
	var validation *ast.CallExpr
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == validateMethodName {
			validation = call
		}
		return true
	})
	if validation == nil || !program.callHasCheckedSuccess(validation) {
		t.Fatal("SSA validation relation guarded by continue was not recognized for the current iteration")
	}

	assigned, _, _, _ := collectCFACasts(
		pass,
		declaration.Body,
		buildParentMap(declaration.Body),
		func(*ast.FuncLit, int) {},
	)
	if len(assigned) != 1 {
		t.Fatalf("assigned casts = %d, want 1", len(assigned))
	}
	if availability := enrichAssignedCastsWithSSA(pass, ssaResult, declaration, assigned); !availability.ready() {
		t.Fatalf("SSA alias enrichment unavailable: %+v", availability)
	}
	functionCFG := buildProtocolCFG(pass, declaration.Body, ssaResult)
	defBlock, defIndex := findDefiningBlock(functionCFG, assigned[0].assign)
	result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateCastPath(interprocCastPathInput{
		Decl: declaration, CFG: functionCFG, DefBlock: defBlock, DefIdx: defIndex,
		Target: assigned[0].target, TypeName: "Value", OriginKey: "loop-current-iteration-test",
		MaxStates:       defaultCFGMaxStates,
		SSAAvailability: ssaResult.Availability,
	})
	if result.Class != interprocOutcomeSafe {
		graph := buildInterprocSupergraphForFunc(pass, declaration, ssaResult)
		terminal := graph.astNode(result.WitnessTerminal)
		t.Fatalf(
			"current-iteration validation result = %+v, want safe; terminal=%T(%v) outgoing=%+v\nCFG:\n%s",
			result,
			terminal,
			terminal,
			graph.outgoing(result.WitnessTerminal),
			functionCFG.Format(pass.Fset),
		)
	}
}

func TestProtocolValidationContinueDoesNotHideFailureBranchUse(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (v Value) Validate() error { return nil }
func consume(Value) {}
func Loop(values []string) {
	for _, raw := range values {
		value := Value(raw)
		if err := value.Validate(); err != nil {
			consume(value)
			continue
		}
	}
}
`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "Loop")
	ssaResult := buildSSAForPass(pass)
	assigned, _, _, _ := collectCFACasts(
		pass,
		declaration.Body,
		buildParentMap(declaration.Body),
		func(*ast.FuncLit, int) {},
	)
	if len(assigned) != 1 {
		t.Fatalf("assigned casts = %d, want 1", len(assigned))
	}
	if availability := enrichAssignedCastsWithSSA(pass, ssaResult, declaration, assigned); !availability.ready() {
		t.Fatalf("SSA alias enrichment unavailable: %+v", availability)
	}
	functionCFG := buildProtocolCFG(pass, declaration.Body, ssaResult)
	defBlock, defIndex := findDefiningBlock(functionCFG, assigned[0].assign)
	result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateCastPath(interprocCastPathInput{
		Decl: declaration, CFG: functionCFG, DefBlock: defBlock, DefIdx: defIndex,
		Target: assigned[0].target, TypeName: "Value", OriginKey: "loop-failure-use-test",
		MaxStates:       defaultCFGMaxStates,
		SSAAvailability: ssaResult.Availability,
	})
	if result.Class != interprocOutcomeUnsafe {
		t.Fatalf("failure-branch target use result = %+v, want unsafe", result)
	}
}
