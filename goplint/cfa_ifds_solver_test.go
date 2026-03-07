// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestNormalizeInterprocEngine(t *testing.T) {
	t.Parallel()

	if got := normalizeInterprocEngine(cfgInterprocEngineIFDS); got != cfgInterprocEngineIFDS {
		t.Fatalf("normalizeInterprocEngine(ifds) = %q, want %q", got, cfgInterprocEngineIFDS)
	}
	if got := normalizeInterprocEngine("unknown"); got != cfgInterprocEngineLegacy {
		t.Fatalf("normalizeInterprocEngine(unknown) = %q, want %q", got, cfgInterprocEngineLegacy)
	}
}

func TestInterprocSolverCastPathNilDefinitionIsInconclusive(t *testing.T) {
	t.Parallel()

	legacySolver := newInterprocSolver(nil, cfgBackendSSA, cfgInterprocEngineLegacy)
	ifdsSolver := newInterprocSolver(nil, cfgBackendSSA, cfgInterprocEngineIFDS)
	input := interprocCastPathInput{
		TypeName:  "pkg.Type",
		OriginKey: "cast-pos",
	}

	legacy := legacySolver.EvaluateCastPathLegacy(input)
	if legacy.Class != interprocOutcomeInconclusive {
		t.Fatalf("legacy class = %q, want %q", legacy.Class, interprocOutcomeInconclusive)
	}
	if legacy.Reason != pathOutcomeReasonUnresolvedTarget {
		t.Fatalf("legacy reason = %q, want %q", legacy.Reason, pathOutcomeReasonUnresolvedTarget)
	}

	ifds := ifdsSolver.EvaluateCastPathIFDS(input)
	if ifds.Class != legacy.Class || ifds.Reason != legacy.Reason {
		t.Fatalf("ifds result = (%q,%q), want (%q,%q)", ifds.Class, ifds.Reason, legacy.Class, legacy.Reason)
	}
	if ifds.FactFamily != ifdsFactFamilyCastNeedsValidate {
		t.Fatalf("ifds fact family = %q, want %q", ifds.FactFamily, ifdsFactFamilyCastNeedsValidate)
	}
}

func TestInterprocSolverUBVCrossBlockNilDefinitionIsInconclusive(t *testing.T) {
	t.Parallel()

	solver := newInterprocSolver(nil, cfgBackendSSA, cfgInterprocEngineIFDS)
	result := solver.EvaluateUBVCrossBlock(interprocUBVCrossBlockInput{
		Mode:      ubvModeEscape,
		OriginKey: "cast-pos",
		TypeName:  "pkg.Type",
	})
	if result.Class != interprocOutcomeInconclusive {
		t.Fatalf("class = %q, want %q", result.Class, interprocOutcomeInconclusive)
	}
	if result.Reason != pathOutcomeReasonUnresolvedTarget {
		t.Fatalf("reason = %q, want %q", result.Reason, pathOutcomeReasonUnresolvedTarget)
	}
}

func TestInterprocSolverUBVInBlockIFDSEdgeTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		src        string
		wantClass  interprocOutcomeClass
		wantEdgeFn ideEdgeFuncTag
	}{
		{
			name: "use before validate produces consume transition",
			src: `package p
func f() {
	x := 1
	use(x)
	x.Validate()
}`,
			wantClass:  interprocOutcomeUnsafe,
			wantEdgeFn: ideEdgeFuncConsume,
		},
		{
			name: "validate before use produces validated state",
			src: `package p
func f() {
	x := 1
	x.Validate()
	use(x)
}`,
			wantClass:  interprocOutcomeSafe,
			wantEdgeFn: ideEdgeFuncValidate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			nodes := parseFunctionNodes(t, tt.src, "f")
			solver := newInterprocSolver(nil, cfgBackendSSA, cfgInterprocEngineIFDS)
			result := solver.EvaluateUBVInBlockIFDS(interprocUBVInBlockInput{
				Target:      newCastTargetFromName("x"),
				Nodes:       nodes,
				StartIndex:  1,
				Mode:        ubvModeOrder,
				OriginKey:   "cast-pos",
				TypeName:    "p.T",
				SyncLits:    nil,
				SyncCalls:   nil,
				MethodCalls: nil,
			})
			if result.Class != tt.wantClass {
				t.Fatalf("class = %q, want %q", result.Class, tt.wantClass)
			}
			if result.EdgeFunctionTag != tt.wantEdgeFn {
				t.Fatalf("edge function tag = %q, want %q", result.EdgeFunctionTag, tt.wantEdgeFn)
			}
		})
	}
}

func parseFunctionNodes(t *testing.T, src, fnName string) []ast.Node {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != fnName || fn.Body == nil {
			continue
		}
		nodes := make([]ast.Node, 0, len(fn.Body.List))
		for _, stmt := range fn.Body.List {
			nodes = append(nodes, stmt)
		}
		return nodes
	}
	t.Fatalf("function %q not found", fnName)
	return nil
}
