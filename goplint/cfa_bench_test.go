// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	gocfg "golang.org/x/tools/go/cfg"
)

func BenchmarkCFGTraversalCastPath(b *testing.B) {
	body, cfg := parseBenchCFGBody(b, `
package bench
func heavy(input string) error {
	x := CommandName(input)
	if len(input) > 0 {
		if input[0] == 'a' {
			x = CommandName(input + "-a")
		} else {
			x = CommandName(input + "-b")
		}
	}
	if len(input) > 2 {
		if input[1] == 'z' {
			return x.Validate()
		}
	}
	if len(input) > 4 {
		if input[2] == 'x' {
			return x.Validate()
		}
	}
	if len(input) > 6 {
		if input[3] == 'y' {
			return x.Validate()
		}
	}
	if len(input) > 8 {
		if input[4] == 'q' {
			return x.Validate()
		}
	}
	return nil
}
`)
	assign := firstAssignStmt(b, body)
	target := newCastTargetFromName("x")
	syncLits := collectSynchronousClosureLits(body)
	noReturn := collectNoReturnFuncAliasEvents(nil, body)
	defBlock, defIdx := findDefiningBlock(cfg, assign)
	if defBlock == nil {
		b.Fatal("defBlock not found")
	}
	budget := adaptiveBlockVisitBudget(
		cfg,
		blockVisitBudget{maxStates: defaultCFGMaxStates, maxDepth: defaultCFGMaxDepth},
	)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = hasPathToReturnWithoutValidateOutcomeWithWitness(
			nil,
			cfg,
			defBlock,
			defIdx,
			target,
			syncLits,
			nil,
			nil,
			noReturn,
			budget.maxStates,
			budget.maxDepth,
		)
	}
}

func BenchmarkCFGTraversalUBVCrossBlock(b *testing.B) {
	body, cfg := parseBenchCFGBody(b, `
package bench
func heavy(input string) error {
	x := CommandName(input)
	if len(input) > 0 {
		if input[0] == 'a' {
			consume(x)
		}
	}
	if len(input) > 2 {
		if input[1] == 'z' {
			consume(x)
		}
	}
	if len(input) > 4 {
		if input[2] == 'x' {
			consume(x)
		}
	}
	return x.Validate()
}
`)
	assign := firstAssignStmt(b, body)
	target := newCastTargetFromName("x")
	defBlock, defIdx := findDefiningBlock(cfg, assign)
	if defBlock == nil {
		b.Fatal("defBlock not found")
	}
	syncLits := collectUBVClosureLits(body)
	budget := adaptiveBlockVisitBudget(
		cfg,
		blockVisitBudget{maxStates: defaultCFGMaxStates, maxDepth: defaultCFGMaxDepth},
	)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = hasUseBeforeValidateCrossBlockOutcomeModeWithWitness(
			nil,
			defBlock,
			defIdx,
			target,
			syncLits,
			nil,
			nil,
			ubvModeEscape,
			budget.maxStates,
			budget.maxDepth,
		)
	}
}

func parseBenchCFGBody(tb testing.TB, src string) (*ast.BlockStmt, *gocfg.CFG) {
	tb.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "bench.go", src, 0)
	if err != nil {
		tb.Fatalf("parse file: %v", err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		return fn.Body, gocfg.New(fn.Body, func(*ast.CallExpr) bool { return true })
	}
	tb.Fatal("func body not found")
	return nil, nil
}

func firstAssignStmt(tb testing.TB, body *ast.BlockStmt) *ast.AssignStmt {
	tb.Helper()
	for _, stmt := range body.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}
		return assign
	}
	tb.Fatal("assign stmt not found")
	return nil
}
