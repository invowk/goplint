// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestIFDSRelevantUnknownRemainsInconclusiveAfterValidation(t *testing.T) {
	t.Parallel()

	run := func(validateAfterUnknown bool) interprocPathResult {
		graph := newInterprocSupergraph()
		start := interprocNodeID{FuncKey: "entry", Kind: interprocNodeKindCFG}
		call := interprocNodeID{FuncKey: "entry", NodeIndex: 1, Kind: interprocNodeKindCall}
		ret := interprocNodeID{FuncKey: "entry", NodeIndex: 2, Kind: interprocNodeKindReturn}
		exit := interprocNodeID{FuncKey: "entry", NodeIndex: 3, Kind: interprocNodeKindCFG}
		unknownTarget := exit
		if validateAfterUnknown {
			unknownTarget = ret
		}
		graph.addEdge(interprocEdge{From: start, To: call, Kind: interprocEdgeIntra})
		graph.addEdge(interprocEdge{
			From: call, To: unknownTarget, Kind: interprocEdgeCallToReturn, Reason: pathOutcomeReasonUnresolvedTarget,
		})
		if validateAfterUnknown {
			graph.addEdge(interprocEdge{From: ret, To: exit, Kind: interprocEdgeIntra})
		}
		graph.functionExitNodes[exit.Key()] = true

		return runIFDSPropagation(
			graph,
			start,
			100,
			nil,
			nil,
			nil,
			func(node interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
				if validateAfterUnknown && node.Key() == ret.Key() {
					return ideEdgeFuncValidate, pathOutcomeReasonNone
				}
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			},
			func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
				return graph.isFunctionExitNode(node) && state != ideStateValidated
			},
			func(interprocNodeID, ast.Node, ideValidationState) bool { return true },
		)
	}

	if got := run(false); got.Class != interprocOutcomeUnsafe {
		t.Fatalf("definite unvalidated terminal outcome = (%s, %s), want unsafe", got.Class, got.Reason)
	}
	if got := run(true); got.Class != interprocOutcomeInconclusive || got.Reason != pathOutcomeReasonUnresolvedTarget {
		t.Fatalf("checked validation after unresolved mutation outcome = (%s, %s), want inconclusive/unresolved", got.Class, got.Reason)
	}
}

func TestIFDSRelevantUnknownAfterValidationIsInconclusive(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "entry", Kind: interprocNodeKindCFG}
	validated := interprocNodeID{FuncKey: "entry", NodeIndex: 1, Kind: interprocNodeKindCFG}
	call := interprocNodeID{FuncKey: "entry", NodeIndex: 2, Kind: interprocNodeKindCall}
	exit := interprocNodeID{FuncKey: "entry", NodeIndex: 3, Kind: interprocNodeKindCFG}
	graph.addEdge(interprocEdge{From: start, To: validated, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: validated, To: call, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{
		From: call, To: exit, Kind: interprocEdgeCallToReturn, Reason: pathOutcomeReasonUnresolvedTarget,
	})
	graph.functionExitNodes[exit.Key()] = true

	got := runIFDSPropagation(
		graph,
		start,
		100,
		nil,
		nil,
		nil,
		func(node interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			if node.Key() == validated.Key() {
				return ideEdgeFuncValidate, pathOutcomeReasonNone
			}
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
			return graph.isFunctionExitNode(node) && state != ideStateValidated
		},
		func(interprocNodeID, ast.Node, ideValidationState) bool { return true },
	)
	if got.Class != interprocOutcomeInconclusive || got.Reason != pathOutcomeReasonUnresolvedTarget {
		t.Fatalf("unresolved mutation after validation outcome = (%s, %s), want inconclusive/unresolved", got.Class, got.Reason)
	}
}

func TestUBVPostValidationUnknownEffectRequiresMutableIdentity(t *testing.T) {
	t.Parallel()

	source := `package testpkg
type Name string
type Mutator interface {
	Apply(*Name)
	Use(Name)
}
func probe(mutator Mutator, raw string) {
	name := Name(raw)
	mutator.Apply(&name)
	mutator.Use(name)
}
`
	pass, file := buildTypedPassFromSource(t, source)
	probe := findFuncDecl(t, file, "probe")
	assignment := probe.Body.List[0].(*ast.AssignStmt)
	target, ok := castTargetFromExpr(pass, assignment.Lhs[0])
	if !ok {
		t.Fatal("cast target was not resolved")
	}

	_, mutableReason := ubvNodeEdgeTag(
		pass,
		probe.Body.List[1],
		target,
		nil,
		nil,
		nil,
		ideStateValidated,
		nil,
		nil,
	)
	if mutableReason != pathOutcomeReasonUnresolvedTarget {
		t.Fatalf("mutable post-validation call reason = %q, want unresolved-target", mutableReason)
	}

	_, valueCopyReason := ubvNodeEdgeTag(
		pass,
		probe.Body.List[2],
		target,
		nil,
		nil,
		nil,
		ideStateValidated,
		nil,
		nil,
	)
	if valueCopyReason != pathOutcomeReasonNone {
		t.Fatalf("value-copy post-validation call reason = %q, want none", valueCopyReason)
	}
}

func TestUBVPreValidationPackageEscapeIsDefiniteByDefault(t *testing.T) {
	t.Parallel()

	source := `package testpkg
type Name string
var escaped *Name
func probe(raw string) {
	name := Name(raw)
	escaped = &name
}`
	pass, file := buildTypedPassFromSource(t, source)
	probe := findFuncDecl(t, file, "probe")
	definition := probe.Body.List[0].(*ast.AssignStmt)
	target, ok := castTargetFromExpr(pass, definition.Lhs[0])
	if !ok {
		t.Fatal("cast target was not resolved")
	}

	tag, reason := ubvNodeEdgeTag(
		pass,
		probe.Body.List[1],
		target,
		nil,
		nil,
		nil,
		ideStateNeedsValidate,
		nil,
		nil,
	)
	if tag != ideEdgeFuncEscape || reason != pathOutcomeReasonNone {
		t.Fatalf("pre-validation package escape = (%q, %q), want (escape, no reason)", tag, reason)
	}
}

func TestIFDSViolationOutranksRelevantUnknownAtTerminal(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "entry", Kind: interprocNodeKindCFG}
	call := interprocNodeID{FuncKey: "entry", NodeIndex: 1, Kind: interprocNodeKindCall}
	exit := interprocNodeID{FuncKey: "entry", NodeIndex: 2, Kind: interprocNodeKindCFG}
	graph.addEdge(interprocEdge{From: start, To: call, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{
		From: call, To: exit, Kind: interprocEdgeCallToReturn, Reason: pathOutcomeReasonUnresolvedTarget,
	})
	graph.functionExitNodes[exit.Key()] = true

	got := runIFDSPropagation(
		graph,
		start,
		100,
		nil,
		nil,
		nil,
		func(node interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			if node.Key() == start.Key() {
				return ideEdgeFuncConsume, pathOutcomeReasonNone
			}
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
			return graph.isFunctionExitNode(node) && state.consumedBeforeValidation()
		},
		func(interprocNodeID, ast.Node, ideValidationState) bool { return true },
	)
	if got.Class != interprocOutcomeUnsafe {
		t.Fatalf("joined violation and uncertainty outcome = %q, want unsafe", got.Class)
	}
}
