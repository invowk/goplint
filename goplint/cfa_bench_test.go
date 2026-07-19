// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"errors"
	"fmt"
	"go/ast"
	"path/filepath"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/protocoloracle"
)

var benchmarkProtocolResult any

func BenchmarkProtocolCanonicalSolver(b *testing.B) {
	b.ReportAllocs()
	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "caller", Kind: interprocNodeKindCFG}
	call := interprocNodeID{FuncKey: "caller", NodeIndex: 1, Kind: interprocNodeKindCall}
	callee := interprocNodeID{FuncKey: "callee", Kind: interprocNodeKindCFG}
	exit := interprocNodeID{FuncKey: "callee", NodeIndex: 1, Kind: interprocNodeKindCFG}
	ret := interprocNodeID{FuncKey: "caller", NodeIndex: 2, Kind: interprocNodeKindReturn}
	graph.addEdge(interprocEdge{From: start, To: call, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: call, To: callee, Kind: interprocEdgeCall, CallSite: "bench-call"})
	graph.addEdge(interprocEdge{From: callee, To: exit, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: exit, To: ret, Kind: interprocEdgeReturn, CallSite: "bench-call"})
	graph.functionExitNodes[ret.Key()] = true

	b.ResetTimer()
	for range b.N {
		benchmarkProtocolResult = runIFDSPropagation(
			graph,
			start,
			defaultCFGMaxStates,
			nil,
			nil,
			nil,
			func(interprocNodeID, ast.Node, ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			},
			func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
				return graph.isFunctionExitNode(node) && state == ideStateNeedsValidate
			},
			nil,
		)
	}
}

func BenchmarkProtocolRecursiveTabulation(b *testing.B) {
	b.ReportAllocs()
	graph := newInterprocSupergraph()
	entry := interprocNodeID{FuncKey: "recursive", BlockIndex: 0, Kind: interprocNodeKindCFG}
	call := interprocNodeID{FuncKey: "recursive", BlockIndex: 1, Kind: interprocNodeKindCall}
	baseExit := interprocNodeID{FuncKey: "recursive", BlockIndex: 2, Kind: interprocNodeKindCFG}
	returnSite := interprocNodeID{FuncKey: "recursive", BlockIndex: 1, Kind: interprocNodeKindReturn}
	recursiveExit := interprocNodeID{FuncKey: "recursive", BlockIndex: 3, Kind: interprocNodeKindCFG}
	graph.addEdge(interprocEdge{From: entry, To: call, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: entry, To: baseExit, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: call, To: entry, Kind: interprocEdgeCall, CallSite: "self"})
	graph.addEdge(interprocEdge{From: baseExit, To: returnSite, Kind: interprocEdgeReturn, CallSite: "self"})
	graph.addEdge(interprocEdge{From: returnSite, To: recursiveExit, Kind: interprocEdgeIntra})
	graph.functionExitNodes[baseExit.Key()] = true
	graph.functionExitNodes[recursiveExit.Key()] = true

	var result interprocPathResult
	b.ResetTimer()
	for range b.N {
		result = runIFDSPropagation(
			graph,
			entry,
			defaultCFGMaxStates,
			nil,
			nil,
			nil,
			func(node interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
				if node.Key() == baseExit.Key() {
					return ideEdgeFuncValidate, pathOutcomeReasonNone
				}
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			},
			func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
				return graph.isFunctionExitNode(node) && state != ideStateValidated
			},
			nil,
		)
	}
	b.StopTimer()
	if result.Class != interprocOutcomeSafe || result.Tabulation.SummaryReuses == 0 {
		b.Fatalf("recursive tabulation result = %+v, want safe with summary reuse", result)
	}
	b.ReportMetric(float64(result.Tabulation.PathEdges), "path-edges/op")
	b.ReportMetric(float64(result.Tabulation.Summaries), "summaries/op")
	b.ReportMetric(float64(result.Tabulation.SummaryReuses), "summary-reuses/op")
	benchmarkProtocolResult = result
}

func BenchmarkProtocolAliasJoin(b *testing.B) {
	b.ReportAllocs()
	left := newProtocolAliasSnapshot()
	right := newProtocolAliasSnapshot()
	for identity := protocolIdentity(1); identity <= 64; identity++ {
		left.bind(identity, identity)
		right.bind(identity, identity)
	}
	for identity := protocolIdentity(2); identity <= 64; identity += 2 {
		right.bind(identity, identity+1000)
	}

	b.ResetTimer()
	for range b.N {
		benchmarkProtocolResult = left.join(right)
	}
}

func BenchmarkConstructorSuccessfulReturnClassification(b *testing.B) {
	b.ReportAllocs()
	const source = `package benchmark
type Value struct{}
func (*Value) Validate() error { return nil }
func NewValue(err error) (*Value, error) {
	value := &Value{}
	if validateErr := value.Validate(); validateErr != nil {
		return nil, validateErr
	}
	if err != nil {
		return nil, err
	} else {
		return value, err
	}
}
`
	pass, file := buildTypedPassFromSource(b, source)
	declaration := findFuncDecl(b, file, "NewValue")
	ssaResult := buildSSAForPass(pass)
	returnInfo := resolveReturnTypeValidateInfo(pass, declaration)
	solver := newInterprocSolverWithSSA(pass, ssaResult)
	input := interprocConstructorPathInput{
		Decl:            declaration,
		ReturnTypeKey:   returnInfo.TypeKey,
		ResultSlot:      returnInfo.ResultSlot,
		Constructor:     "benchmark.NewValue",
		MaxStates:       defaultCFGMaxStates,
		SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaResult, declaration),
	}

	var result interprocPathResult
	b.ResetTimer()
	for b.Loop() {
		result = solver.EvaluateConstructorPath(input)
	}
	b.StopTimer()
	if result.Class != interprocOutcomeSafe {
		b.Fatalf("constructor path result = %+v, want safe", result)
	}
	benchmarkProtocolResult = result
}

func BenchmarkProtocolPackageProcedureInventory(b *testing.B) {
	b.ReportAllocs()
	const source = `package benchmark
var callback = func(raw string) string { return raw }
func Run(raw string) string {
	nested := func(value string) string { return callback(value) }
	return nested(raw)
}
`
	pass, _ := buildTypedPassFromSource(b, source)
	ssaResult := buildSSAForPass(pass)

	b.ResetTimer()
	for b.Loop() {
		benchmarkProtocolResult = buildProtocolProcedureIndex(pass, ssaResult)
	}
}

func BenchmarkProtocolRefinementEvidence(b *testing.B) {
	b.ReportAllocs()
	formula := cfgSSAConstraintFormula{}
	for alternative := range 32 {
		subject := "v" + string(rune('a'+alternative%26))
		formula.alternatives = append(formula.alternatives, []cfgPredicateConstraint{
			{subject: subject, op: "neq", value: "ready"},
			{subject: subject, op: "eq", value: "ready"},
		})
	}
	formula.normalize()
	witness := []int32{0, 1, 2, 3}

	b.ResetTimer()
	for range b.N {
		evidence, ok := buildSSAConstraintEvidence(formula)
		if !ok {
			b.Fatal("benchmark formula unexpectedly satisfiable")
		}
		evidence.WitnessPath = witness
		evidence.Subjects = ssaConstraintSubjects(formula)
		benchmarkProtocolResult = checkSSAConstraintFormulaEvidence(formula, witness, evidence)
	}
}

func BenchmarkProtocolReferenceInterpreter(b *testing.B) {
	b.ReportAllocs()
	manifest, err := protocoloracle.LoadBoundsManifest(filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		b.Fatal(err)
	}

	programs := make([]protocoloracle.Program, 0, manifest.Blocking.ExpectedProgramCount)
	if err = protocoloracle.Enumerate(manifest, func(program protocoloracle.Program) error {
		programs = append(programs, program)
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(len(programs)), "programs/op")
	b.ResetTimer()
	for b.Loop() {
		for _, program := range programs {
			benchmarkProtocolResult = protocoloracle.Interpret(program, manifest.Blocking.MaxStates)
		}
	}
}

func BenchmarkProtocolGeneratedAnalyzer(b *testing.B) {
	b.ReportAllocs()
	manifest, err := protocoloracle.LoadBoundsManifest(filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		b.Fatal(err)
	}
	type benchmarkSource struct {
		caseID   string
		identity protocoloracle.Identity
		source   string
	}
	sources := make([]benchmarkSource, 0, manifest.Blocking.ExpectedProgramCount)
	programCount := 0
	if err = protocoloracle.Enumerate(manifest, func(program protocoloracle.Program) error {
		programCount++
		for _, identity := range program.Identities {
			source, sourceErr := protocoloracle.GoSourceForIdentity(program, identity)
			if sourceErr != nil {
				return sourceErr
			}
			sources = append(sources, benchmarkSource{caseID: program.CaseID, identity: identity, source: source})
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(programCount), "generated-programs/op")
	b.ReportMetric(float64(len(sources)), "identities/op")
	observedPipeline := generatedAnalyzerPipelineTrace{}
	b.ResetTimer()
	for b.Loop() {
		diagnostics := 0
		pipeline := generatedAnalyzerPipelineTrace{}
		for _, input := range sources {
			result, analyzeErr := runGeneratedGoAnalyzer(b, input.source, manifest.Blocking.MaxStates)
			if analyzeErr != nil {
				b.Fatalf("analyze %s identity %d: %v", input.caseID, input.identity, analyzeErr)
			}
			diagnostics += result.DiagnosticCount
			pipeline.merge(result.Pipeline)
			benchmarkProtocolResult = result
		}
		if evidenceErr := validateGeneratedAnalyzerBenchmarkEvidence(diagnostics, pipeline); evidenceErr != nil {
			b.Fatal(evidenceErr)
		}
		observedPipeline = pipeline
	}
	b.ReportMetric(float64(len(observedPipeline)), "pipeline-stages/op")
}

func validateGeneratedAnalyzerBenchmarkEvidence(
	diagnostics int,
	pipeline generatedAnalyzerPipelineTrace,
) error {
	if diagnostics == 0 {
		return errors.New("generated analyzer benchmark emitted no diagnostics")
	}
	for _, stage := range requiredGeneratedAnalyzerPipeline() {
		if !pipeline[stage] {
			return fmt.Errorf("generated analyzer benchmark omitted production stage %q", stage)
		}
	}
	return nil
}
