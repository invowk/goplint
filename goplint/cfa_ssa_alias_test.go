// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/ssa"
)

// TestSSABuilderProducesDebugRefs verifies that the on-demand SSA builder
// emits DebugRef instructions needed for alias tracking.
func TestSSABuilderProducesDebugRefs(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()

	probeAnalyzer := &analysis.Analyzer{
		Name: "ssaprobe",
		Doc:  "probe SSA for debug refs",
		Run: func(pass *analysis.Pass) (any, error) {
			res := buildSSAForPass(pass)
			if res == nil || res.Pkg == nil {
				t.Fatal("buildSSAForPass() returned nil for probe fixture")
			}

			var foundDebugRef bool
			for _, mem := range res.Pkg.Members {
				fn, ok := mem.(*ssa.Function)
				if !ok || fn.Blocks == nil {
					continue
				}
				for _, block := range fn.Blocks {
					for _, instr := range block.Instrs {
						if _, ok := instr.(*ssa.DebugRef); ok {
							foundDebugRef = true
						}
					}
				}
			}
			if !foundDebugRef {
				t.Error("no DebugRef instructions found; GlobalDebug mode may not be active")
			}
			return nil, nil //nolint:nilnil // probe analyzer has no meaningful result
		},
	}

	analysistestParallelLimiter <- struct{}{}
	defer func() { <-analysistestParallelLimiter }()

	analysistest.Run(t, testdata, probeAnalyzer, "cfa_ssa_alias_probe")
}

// TestComputeMustAliasKeys_CopyAlias verifies that y := x produces an
// alias set containing y's objectKey via a probe fixture without annotations.
func TestComputeMustAliasKeys_CopyAlias(t *testing.T) {
	t.Parallel()

	runAliasProbeAnalysis(t, "copyaliasprobe", func(pass *analysis.Pass, res *ssaResult) {
		ssaFn, cast := probeAssignedCast(t, pass, res, "CopyAlias")
		aliases := computeMustAliasKeys(ssaFn, cast.pos)
		assertAliasContains(t, aliases, "x", "y")
	})
}

// TestComputeMustAliasKeys_Reassignment verifies that y := x; y = other
// excludes y from the alias set.
func TestComputeMustAliasKeys_Reassignment(t *testing.T) {
	t.Parallel()

	runAliasProbeAnalysis(t, "reassignprobe", func(pass *analysis.Pass, res *ssaResult) {
		ssaFn, cast := probeAssignedCast(t, pass, res, "ReassignedAlias")
		aliases := computeMustAliasKeys(ssaFn, cast.pos)
		assertAliasContains(t, aliases, "x")
		assertAliasNotContains(t, aliases, "y")
	})
}

// TestComputeMustAliasKeys_NestedCallPrefersCast verifies that nested helper
// calls inside the cast argument do not displace the actual conversion result.
func TestComputeMustAliasKeys_NestedCallPrefersCast(t *testing.T) {
	t.Parallel()

	runAliasProbeAnalysis(t, "nestedcallprobe", func(pass *analysis.Pass, res *ssaResult) {
		ssaFn, cast := probeAssignedCast(t, pass, res, "NestedCallAlias")
		aliases := computeMustAliasKeys(ssaFn, cast.pos)
		assertAliasContains(t, aliases, "x", "y")
	})
}

// TestComputeMustAliasKeys_OverflowReturnsNil verifies that very large alias
// fanout disables alias tracking conservatively.
func TestComputeMustAliasKeys_OverflowReturnsNil(t *testing.T) {
	t.Parallel()

	runAliasProbeAnalysis(t, "overflowprobe", func(pass *analysis.Pass, res *ssaResult) {
		ssaFn, cast := probeAssignedCast(t, pass, res, "OverflowAlias")
		if aliases := computeMustAliasKeys(ssaFn, cast.pos); aliases != nil {
			t.Fatalf("expected nil alias set once fanout exceeds %d, got %d entries", maxAliasSetSize, len(aliases))
		}
	})
}

// TestMatchesExprWithAliasSet verifies that alias-enriched cast targets match
// alias receivers while reassigned aliases stay excluded.
func TestMatchesExprWithAliasSet(t *testing.T) {
	t.Parallel()

	runAliasProbeAnalysis(t, "matchesexprprobe", func(pass *analysis.Pass, res *ssaResult) {
		copySSAFn, copyCast := probeAssignedCast(t, pass, res, "CopyAlias")
		copyTarget := enrichTargetWithSSAAlias(copySSAFn, copyCast)
		if len(copyTarget.aliasKeys) == 0 {
			t.Fatal("expected alias-enriched target for CopyAlias")
		}
		copyReceiver := findValidateReceiverInFunc(t, findProbeFuncDecl(t, pass, "CopyAlias"))
		if !copyTarget.matchesExpr(pass, copyReceiver) {
			t.Fatal("expected alias-enriched target to match y.Validate receiver")
		}

		reassignSSAFn, reassignCast := probeAssignedCast(t, pass, res, "ReassignedAlias")
		reassignTarget := enrichTargetWithSSAAlias(reassignSSAFn, reassignCast)
		reassignReceiver := findValidateReceiverInFunc(t, findProbeFuncDecl(t, pass, "ReassignedAlias"))
		if reassignTarget.matchesExpr(pass, reassignReceiver) {
			t.Fatal("expected reassigned alias receiver to stay excluded")
		}
	})
}

func runAliasProbeAnalysis(
	t *testing.T,
	name string,
	check func(pass *analysis.Pass, res *ssaResult),
) {
	t.Helper()

	testdata := analysistest.TestData()
	probeAnalyzer := &analysis.Analyzer{
		Name: name,
		Doc:  "probe phase d alias behavior",
		Run: func(pass *analysis.Pass) (any, error) {
			res := buildSSAForPass(pass)
			if res == nil || res.Pkg == nil {
				t.Fatal("buildSSAForPass() returned nil for probe fixture")
			}
			check(pass, res)
			return nil, nil //nolint:nilnil // probe analyzer has no meaningful result
		},
	}

	analysistestParallelLimiter <- struct{}{}
	defer func() { <-analysistestParallelLimiter }()

	analysistest.Run(t, testdata, probeAnalyzer, "cfa_ssa_alias_probe")
}

func findMemberFunc(pkg *ssa.Package, name string) *ssa.Function {
	for _, mem := range pkg.Members {
		fn, ok := mem.(*ssa.Function)
		if ok && fn.Name() == name {
			return fn
		}
	}
	return nil
}

func probeAssignedCast(
	t *testing.T,
	pass *analysis.Pass,
	res *ssaResult,
	funcName string,
) (*ssa.Function, cfaAssignedCast) {
	t.Helper()

	fn := findMemberFunc(res.Pkg, funcName)
	if fn == nil {
		t.Fatalf("missing SSA function %q", funcName)
	}
	fnDecl := findProbeFuncDecl(t, pass, funcName)
	parentMap := buildParentMap(fnDecl.Body)
	assignedCasts, _, _, _ := collectCFACasts(
		pass,
		fnDecl.Body,
		parentMap,
		func(_ *ast.FuncLit, _ int) {},
	)
	if len(assignedCasts) == 0 {
		t.Fatalf("expected at least 1 assigned cast in %s", funcName)
	}
	return fn, assignedCasts[0]
}

func findProbeFuncDecl(t *testing.T, pass *analysis.Pass, name string) *ast.FuncDecl {
	t.Helper()

	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Name != nil && fn.Name.Name == name {
				return fn
			}
		}
	}
	t.Fatalf("missing probe func decl %q", name)
	return nil
}

func findValidateReceiverInFunc(t *testing.T, fn *ast.FuncDecl) ast.Expr {
	t.Helper()

	var receiver ast.Expr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Validate" {
			return true
		}
		receiver = sel.X
		return false
	})
	if receiver == nil {
		t.Fatalf("missing Validate() call in %s", fn.Name.Name)
	}
	return receiver
}

func assertAliasContains(t *testing.T, aliases ssaAliasSet, names ...string) {
	t.Helper()

	if len(aliases) == 0 {
		t.Fatal("expected non-empty alias set")
	}
	for _, name := range names {
		found := false
		for key := range aliases {
			if aliasTestContains(key, ":"+name+":") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("alias set missing %q: %#v", name, aliases)
		}
	}
}

func assertAliasNotContains(t *testing.T, aliases ssaAliasSet, name string) {
	t.Helper()

	for key := range aliases {
		if aliasTestContains(key, ":"+name+":") {
			t.Fatalf("alias set unexpectedly contained %q: %#v", name, aliases)
		}
	}
}

func aliasTestContains(s, sub string) bool {
	for i := range len(s) - len(sub) + 1 {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
