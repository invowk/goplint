// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/protocoloracle"
)

// TestProtocolOracleSolverCoreComponent is supporting component evidence only.
// The generated-Go analyzer test is the required end-to-end comparison.
func TestProtocolOracleSolverCoreComponent(t *testing.T) {
	t.Parallel()

	manifest, err := protocoloracle.LoadBoundsManifest(filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		t.Fatalf("LoadBoundsManifest() error: %v", err)
	}
	outcomes := map[protocoloracle.Outcome]bool{}
	count := 0
	err = protocoloracle.Enumerate(manifest, func(program protocoloracle.Program) error {
		if !solverCoreComponentSupports(program) {
			return nil
		}
		count++
		reference := protocoloracle.Interpret(program, manifest.Blocking.MaxStates)
		for _, identity := range program.Identities {
			got := runGeneratedProgram(program, identity, manifest.Blocking.MaxStates)
			want := reference.ByIdentity[identity]
			outcomes[want] = true
			if got != want {
				return fmt.Errorf("program %s identity %d: canonical=%s reference=%s; program=%+v",
					program.Fingerprint(), identity, got, want, program)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("solver-core component corpus is empty")
	}
	for _, outcome := range []protocoloracle.Outcome{
		protocoloracle.OutcomeNone,
		protocoloracle.OutcomeViolation,
	} {
		if !outcomes[outcome] {
			t.Fatalf("generated corpus did not exercise outcome %q", outcome)
		}
	}
}

func TestProtocolOracleIndependence(t *testing.T) {
	t.Parallel()

	oracleDir := filepath.Join("..", "internal", "protocoloracle")
	files, err := filepath.Glob(filepath.Join(oracleDir, "*.go"))
	if err != nil {
		t.Fatalf("Glob() error: %v", err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("ParseFile(%s) error: %v", path, parseErr)
		}
		for _, imported := range file.Imports {
			pathValue, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("invalid import in %s: %v", path, unquoteErr)
			}
			if strings.Contains(pathValue, ".") {
				t.Fatalf("independent oracle imports non-standard package %q", pathValue)
			}
		}
	}
	productionFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("Glob production files error: %v", err)
	}
	for _, path := range productionFiles {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("ReadFile(%s) error: %v", path, readErr)
		}
		if strings.Contains(string(content), "internal/protocoloracle") {
			t.Fatalf("production file %s imports the test-only oracle", path)
		}
	}
}

func runGeneratedProgram(
	program protocoloracle.Program,
	identity protocoloracle.Identity,
	maxStates int,
) protocoloracle.Outcome {
	graph := newInterprocSupergraph()
	operations := make(map[string]protocoloracle.Node)
	for _, node := range program.Nodes {
		id := generatedInterprocNodeID(node.Ref)
		graph.addNode(id, nil)
		operations[id.Key()] = node
		if node.Terminal {
			graph.terminalCFGNodes[id.Key()] = true
		}
	}
	for _, edge := range program.Edges {
		graph.addEdge(interprocEdge{
			From:     generatedInterprocNodeID(edge.From),
			To:       generatedInterprocNodeID(edge.To),
			Kind:     generatedInterprocEdgeKind(edge.Kind),
			CallSite: generatedCallSite(edge.CallSite),
		})
	}
	transfer := func(nodeID interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
		node := operations[nodeID.Key()]
		if node.Identity != identity {
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		}
		switch node.UnknownEffect {
		case protocoloracle.UnknownEffectNone:
			// No uncertainty contribution.
		case protocoloracle.UnknownEffectUnresolved:
			return ideEdgeFuncIdentity, pathOutcomeReasonUnresolvedTarget
		case protocoloracle.UnknownEffectConcurrentMutation:
			return ideEdgeFuncIdentity, pathOutcomeReasonConcurrentMutation
		case protocoloracle.UnknownEffectEscapedHeap:
			return ideEdgeFuncIdentity, pathOutcomeReasonEscapedHeapMutation
		}
		switch node.Operation {
		case protocoloracle.OperationValidate:
			switch node.Condition {
			case protocoloracle.ConditionalResultNil:
				return ideEdgeFuncValidate, pathOutcomeReasonNone
			case protocoloracle.ConditionalResultUnknown:
				return ideEdgeFuncIdentity, pathOutcomeReasonUnresolvedTarget
			default:
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			}
		case protocoloracle.OperationMutate, protocoloracle.OperationReplace:
			return ideEdgeFuncInvalidate, pathOutcomeReasonNone
		case protocoloracle.OperationEscape:
			return ideEdgeFuncIdentity, pathOutcomeReasonEscapedHeapMutation
		case protocoloracle.OperationUnresolved:
			return ideEdgeFuncIdentity, pathOutcomeReasonUnresolvedTarget
		default:
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		}
	}
	terminalUnsafe := func(nodeID interprocNodeID, _ ast.Node, state ideValidationState) bool {
		node := operations[nodeID.Key()]
		if node.Identity == identity &&
			(node.Operation == protocoloracle.OperationConsume || node.Operation == protocoloracle.OperationEscape) {
			return state != ideStateValidated
		}
		return node.Terminal && state != ideStateValidated
	}
	initialState := newProtocolRequiredState()
	if program.InitialFactFor(identity) == protocoloracle.InitialFactValidated {
		initialState = ideStateValidated
	}
	result, _ := runIFDSPropagationWithStatsOptions(
		graph,
		generatedInterprocNodeID(program.Entry),
		maxStates,
		nil,
		nil,
		transfer,
		terminalUnsafe,
		nil,
		nil,
		interprocSinkPolicy{TerminalCanObserve: true},
		nil,
		nil,
		interprocTabulationOptions{InitialState: &initialState},
	)
	switch result.Class {
	case interprocOutcomeUnsafe:
		return protocoloracle.OutcomeViolation
	case interprocOutcomeInconclusive:
		return protocoloracle.OutcomeInconclusive
	default:
		return protocoloracle.OutcomeNone
	}
}

func generatedInterprocNodeID(ref protocoloracle.NodeRef) interprocNodeID {
	return interprocNodeID{
		FuncKey:    fmt.Sprintf("p%d", ref.Procedure),
		BlockIndex: int32(ref.Node),
		Kind:       interprocNodeKindCFG,
	}
}

func generatedInterprocEdgeKind(kind protocoloracle.EdgeKind) interprocEdgeKind {
	switch kind {
	case protocoloracle.EdgeCall:
		return interprocEdgeCall
	case protocoloracle.EdgeReturn:
		return interprocEdgeReturn
	default:
		return interprocEdgeIntra
	}
}

func generatedCallSite(callSite uint8) string {
	if callSite == 0 {
		return ""
	}
	return fmt.Sprintf("site-%d", callSite)
}

func solverCoreComponentSupports(program protocoloracle.Program) bool {
	for _, node := range program.Nodes {
		if node.AliasAction != protocoloracle.AliasActionNone || node.Constraint != protocoloracle.ConstraintNone {
			return false
		}
		if node.UnknownEffect != protocoloracle.UnknownEffectNone ||
			node.Operation == protocoloracle.OperationUnresolved ||
			node.Condition == protocoloracle.ConditionalResultUnknown {
			return false
		}
	}
	return true
}
