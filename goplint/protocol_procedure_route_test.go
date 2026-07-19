// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

func TestProcedureInventoryDirectValidationRemainsSafe(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value *Value) Validate() error { return nil }
func use(Value) {}
func Probe(raw string) {
	value := Value(raw)
	if err := value.Validate(); err != nil { return }
	use(value)
}`
	pass, file := buildTypedPassFromSource(t, source)
	var diagnostics []analysis.Diagnostic
	pass.Report = func(diagnostic analysis.Diagnostic) {
		diagnostics = append(diagnostics, diagnostic)
	}
	ssaResult := buildSSAForPass(pass)
	index := buildProtocolProcedureIndex(pass, ssaResult)
	if len(index.procedures()) != 3 {
		t.Fatalf("procedure inventory size = %d, want 3", len(index.procedures()))
	}
	declaration := findFuncDecl(t, file, "Probe")
	parentMap := buildParentMap(declaration.Body)
	assigned, _, closureCalls, methodValueCalls := collectCFACasts(
		pass,
		declaration.Body,
		parentMap,
		func(*ast.FuncLit, int) {},
	)
	if len(assigned) != 1 {
		t.Fatalf("assigned casts = %d, want 1", len(assigned))
	}
	enrichAssignedCastsWithSSA(pass, ssaResult, declaration, assigned)
	functionCFG := buildProtocolCFG(pass, declaration.Body, ssaResult)
	defBlock, defIndex := findDefiningBlock(functionCFG, assigned[0].assign)
	directResult := newInterprocSolverWithSSA(pass, ssaResult).EvaluateCastPath(interprocCastPathInput{
		Decl:            declaration,
		CFG:             functionCFG,
		DefBlock:        defBlock,
		DefIdx:          defIndex,
		Target:          assigned[0].target,
		TypeName:        assigned[0].typeName,
		SyncCalls:       collectSynchronousClosureVarCalls(closureCalls),
		MethodCalls:     collectMethodValueValidateCallSet(methodValueCalls),
		MaxStates:       defaultCFGMaxStates,
		SSAAvailability: ssaResult.Availability,
	})
	if directResult.Class != interprocOutcomeSafe {
		t.Fatalf("direct solver result = %+v, want safe", directResult)
	}
	if err := inspectUnvalidatedCastsCFA(
		pass,
		declaration,
		&ExceptionConfig{},
		&BaselineConfig{},
		false,
		defaultCFGMaxStates,
		defaultCFGWitnessMaxSteps,
		newCFGProtocolRefinementOptions(runConfig{}),
		ssaResult,
		&sync.Map{},
		ssaAvailability{Status: ssaAvailabilityReady},
	); err != nil {
		t.Fatalf("inspectUnvalidatedCastsCFA() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("direct checked validation diagnostics = %+v, want none", diagnostics)
	}
}

func TestUnresolvedClosureRootRemainsVisibleThroughExceptionAndInlineIgnore(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value *Value) Validate() error { return nil }
var Stored = func(raw string) {
	value := Value(raw) //goplint:ignore -- definite findings may be suppressed, proof uncertainty may not
	_ = value
}
`
	pass, file := buildTypedPassFromSource(t, source)
	var literal *ast.FuncLit
	ast.Inspect(file, func(node ast.Node) bool {
		if candidate, ok := node.(*ast.FuncLit); ok {
			literal = candidate
			return false
		}
		return true
	})
	if literal == nil {
		t.Fatal("stored function literal is missing")
	}
	var diagnostics []analysis.Diagnostic
	pass.Report = func(diagnostic analysis.Diagnostic) {
		diagnostics = append(diagnostics, diagnostic)
	}
	ssaResult := buildSSAForPass(pass)
	if err := inspectClosureCastsCFA(
		pass,
		literal,
		"probe.init",
		"stored",
		&ExceptionConfig{Exceptions: []Exception{{
			Pattern: "probe.init.cast-validation",
			Reason:  "definite finding exception",
		}}},
		&BaselineConfig{},
		false,
		defaultCFGMaxStates,
		defaultCFGWitnessMaxSteps,
		newCFGProtocolRefinementOptions(runConfig{}),
		ssaResult,
		&sync.Map{},
		ssaAvailability{
			Status: ssaAvailabilityMissingClosure,
			Detail: "test unresolved capture identity",
		},
	); err != nil {
		t.Fatalf("inspectClosureCastsCFA() error = %v", err)
	}
	count := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Category == CategoryUnvalidatedCastInconclusive {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("unresolved closure inconclusive count = %d, want 1; diagnostics=%+v", count, diagnostics)
	}
}

func TestAmbiguousLiteralRemainsInProcedureInventoryAsBlockingRoot(t *testing.T) {
	t.Parallel()

	const source = `package probe
var Stored = func() {}
`
	pass, file := buildTypedPassFromSource(t, source)
	var literal *ast.FuncLit
	ast.Inspect(file, func(node ast.Node) bool {
		if candidate, ok := node.(*ast.FuncLit); ok {
			literal = candidate
			return false
		}
		return true
	})
	if literal == nil {
		t.Fatal("stored function literal is missing")
	}
	index := protocolProcedureIndex{
		byFunction:       make(map[*ssa.Function]protocolProcedure),
		byKey:            make(map[string]protocolProcedure),
		byDecl:           make(map[*ast.FuncDecl]protocolProcedure),
		byLiteral:        make(map[*ast.FuncLit]protocolProcedure),
		ambiguousDecl:    make(map[*ast.FuncDecl]bool),
		ambiguousLiteral: map[*ast.FuncLit]bool{literal: true},
	}
	index.addUnresolvedSourceProcedures(
		pass,
		buildSSAForPass(pass),
		nil,
		map[token.Pos]*ast.FuncLit{literal.Pos(): literal},
	)
	procedures := index.procedures()
	if len(procedures) != 1 || procedures[0].Literal != literal ||
		procedures[0].Availability.Status != ssaAvailabilityMissingClosure ||
		procedures[0].Availability.Detail == "" {
		t.Fatalf("ambiguous procedure inventory = %+v, want one blocking literal root", procedures)
	}
}
