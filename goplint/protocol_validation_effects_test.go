// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestProtocolValidationEffectsRequireNilResultEdge(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Value string

func (v Value) Validate() error { return nil }
func consume(Value) {}

func Checked(value Value) {
	err := value.Validate()
	if err != nil {
		return
	}
	consume(value)
}

func Inverted(value Value) {
	err := value.Validate()
	if err == nil {
		consume(value)
	}
}

func Unchecked(value Value) {
	_ = value.Validate()
	consume(value)
}
`
	pass, _ := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	procedureIndex := buildProtocolProcedureIndex(pass, ssaRes)
	if len(procedureIndex.procedures()) == 0 {
		t.Fatal("package procedure inventory is empty")
	}
	if ssaRes == nil || ssaRes.Pkg == nil {
		t.Fatal("buildSSAForPass() returned no package")
	}

	for _, name := range []string{"Checked", "Inverted"} {
		fn := findMemberFunc(ssaRes.Pkg, name)
		if fn == nil {
			t.Fatalf("SSA function %s is missing", name)
		}
		interner := newProtocolIdentityInterner()
		invocations := collectProtocolValidationInvocations(fn, interner)
		if len(invocations) != 1 {
			t.Fatalf("%s validation invocations = %d, want 1", name, len(invocations))
		}
		invocation := invocations[0]
		if invocation.Relation.ReceiverIdentity == 0 || invocation.Relation.ErrorIdentity == 0 {
			t.Fatalf("%s has zero receiver/result relation: %+v", name, invocation.Relation)
		}
		if name == "Checked" {
			identityState := "distinct"
			if invocation.Relation.ReceiverIdentity == invocation.Relation.ErrorIdentity {
				identityState = "same"
			}
			requireMutationGuardObservation(
				t,
				"receiver-identity/checked-validation-relation",
				mutationGuardState("validation-receiver-and-error-identities", "distinct"),
				mutationGuardState("validation-receiver-and-error-identities", identityState),
			)
		}
		if invocation.Relation.ReceiverIdentity == invocation.Relation.ErrorIdentity {
			t.Fatalf("%s receiver and result share identity", name)
		}

		edges := collectProtocolValidationEdgeEffects(fn, invocations)
		if len(edges) != 2 {
			t.Fatalf("%s validation edge effects = %d, want 2", name, len(edges))
		}
		consumeBlock := protocolCallBlockByCalleeName(t, fn, "consume")
		seenNil := false
		seenNonNil := false
		for _, edge := range edges {
			initial := newProtocolRequiredState()
			got := initial.apply(edge.Effect, edge.Result, protocolUncertaintyUnresolvedCall)
			reachesConsume := protocolBlockReaches(edge.To, consumeBlock)
			switch edge.Result {
			case protocolErrorResultNil:
				seenNil = true
				if name == "Checked" {
					continuationState := "reaches-protected-continuation"
					if !reachesConsume {
						continuationState = "does-not-reach-protected-continuation"
					}
					requireMutationGuardObservation(
						t,
						"nil-branch-success/checked-nil-edge",
						mutationGuardState("nil-result-edge", "reaches-protected-continuation"),
						mutationGuardState("nil-result-edge", continuationState),
					)
				}
				if got.Validation != protocolValidationProven {
					t.Fatalf("%s nil edge did not validate: %+v", name, got)
				}
				if !reachesConsume {
					t.Fatalf("%s nil edge does not reach protected continuation", name)
				}
			case protocolErrorResultNonNil:
				seenNonNil = true
				if got.Validation != protocolValidationRequired {
					t.Fatalf("%s non-nil edge validated: %+v", name, got)
				}
				if reachesConsume {
					t.Fatalf("%s non-nil edge reaches protected continuation", name)
				}
			default:
				t.Fatalf("%s edge has unknown result", name)
			}
		}
		if !seenNil || !seenNonNil {
			t.Fatalf("%s edge results: nil=%t nonnil=%t", name, seenNil, seenNonNil)
		}
	}

	unchecked := findMemberFunc(ssaRes.Pkg, "Unchecked")
	if unchecked == nil {
		t.Fatal("SSA function Unchecked is missing")
	}
	interner := newProtocolIdentityInterner()
	invocations := collectProtocolValidationInvocations(unchecked, interner)
	if len(invocations) != 1 {
		t.Fatalf("Unchecked validation invocations = %d, want 1", len(invocations))
	}
	if effects := collectProtocolValidationEdgeEffects(unchecked, invocations); len(effects) != 0 {
		t.Fatalf("unchecked validation created %d proven edge effects", len(effects))
	}
}

func TestProtocolValidationIgnoresLowercaseLookalike(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Value string

func (v Value) validate() error { return nil }

func Check(value Value) error {
	return value.validate()
}
`
	pass, _ := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	if ssaRes == nil || ssaRes.Pkg == nil {
		t.Fatal("buildSSAForPass() returned no package")
	}
	fn := findMemberFunc(ssaRes.Pkg, "Check")
	if fn == nil {
		t.Fatal("SSA function Check is missing")
	}
	if got := collectProtocolValidationInvocations(fn, newProtocolIdentityInterner()); len(got) != 0 {
		t.Fatalf("lowercase lookalike produced %d validation invocations", len(got))
	}
}

func TestProtocolValidationFormsShareConditionalEffectAPI(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Validator interface { Validate() error }
type Value string

func (v Value) Validate() error { return nil }
func consume(any) {}

func InterfaceCall(value Validator) {
	err := value.Validate()
	if err != nil { return }
	consume(value)
}

func MethodValue(value Value) {
	validate := value.Validate
	err := validate()
	if err != nil { return }
	consume(value)
}

func MethodValueAlias(value Value) {
	validate := value.Validate
	alias := validate
	err := alias()
	if err != nil { return }
	consume(value)
}

func TerminatingFailure(value Value) {
	err := value.Validate()
	if err != nil { panic(err) }
	consume(value)
}
`
	pass, _ := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	if ssaRes == nil || ssaRes.Pkg == nil {
		t.Fatal("buildSSAForPass() returned no package")
	}

	for _, name := range []string{"InterfaceCall", "MethodValue", "MethodValueAlias", "TerminatingFailure"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fn := findMemberFunc(ssaRes.Pkg, name)
			if fn == nil {
				t.Fatalf("SSA function %s is missing", name)
			}
			interner := newProtocolIdentityInterner()
			invocations := collectProtocolValidationInvocations(fn, interner)
			if len(invocations) != 1 {
				t.Fatalf("%s validation invocations = %d, want 1", name, len(invocations))
			}
			wantReceiver := interner.internValue(fn.Params[0])
			if got := invocations[0].Relation.ReceiverIdentity; got != wantReceiver {
				t.Fatalf("%s receiver identity = %d, want parameter identity %d", name, got, wantReceiver)
			}

			edges := collectProtocolValidationEdgeEffects(fn, invocations)
			if len(edges) != 2 {
				t.Fatalf("%s validation edge effects = %d, want 2", name, len(edges))
			}
			consumeBlock := protocolCallBlockByCalleeName(t, fn, "consume")
			for _, edge := range edges {
				reachesConsume := protocolBlockReaches(edge.To, consumeBlock)
				if edge.Result == protocolErrorResultNil && !reachesConsume {
					t.Fatalf("%s nil edge does not reach protected continuation", name)
				}
				if edge.Result == protocolErrorResultNonNil && reachesConsume {
					t.Fatalf("%s non-nil terminating edge reaches protected continuation", name)
				}
			}
		})
	}
}

func TestValidationProgramRejectsWeakErrorHandling(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Value string
func (value Value) Validate() error { return nil }

func Checked(value Value) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return nil
}

func Continued(value Value) error {
	if err := value.Validate(); err != nil {
		_ = err
	}
	return nil
}

func SuccessTerminates(value Value) error {
	if err := value.Validate(); err == nil {
		return nil
	}
	return errUse(value)
}

func errUse(Value) error { return nil }

func Ignored(value Value) error {
	err := value.Validate()
	_ = err
	return nil
}

func Returned(value Value) error { return value.Validate() }
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	tests := []struct {
		functionName string
		wantChecked  bool
	}{
		{functionName: "Checked", wantChecked: true},
		{functionName: "Continued"},
		{functionName: "SuccessTerminates"},
		{functionName: "Ignored"},
		{functionName: "Returned", wantChecked: true},
	}
	for _, tt := range tests {
		declaration := findFuncDecl(t, file, tt.functionName)
		var validation *ast.CallExpr
		ast.Inspect(declaration.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := stripParens(call.Fun).(*ast.SelectorExpr)
			if ok && selector.Sel.Name == validateMethodName {
				validation = call
			}
			return true
		})
		program := buildProtocolValidationProgram(pass, ssaRes, nil)
		if validation == nil {
			t.Fatalf("%s has no validation call", tt.functionName)
		}
		if checked := program.callHasCheckedSuccess(validation); checked != tt.wantChecked {
			t.Fatalf("%s checked SSA validation = %t, want %t", tt.functionName, checked, tt.wantChecked)
		}
	}
}

func TestProtocolValidationProgramProjectsSSAResultOntoCFGEdges(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Value string
func (value Value) Validate() error { return nil }

func Checked(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	return nil
}
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	declaration := findFuncDecl(t, file, "Checked")
	program := buildProtocolValidationProgram(pass, ssaRes, nil)
	graph := buildInterprocSupergraphForFunc(pass, declaration, ssaRes)
	assigned, _, _, _ := collectCFACasts(pass, declaration.Body, buildParentMap(declaration.Body), func(*ast.FuncLit, int) {})
	if len(assigned) != 1 {
		t.Fatalf("assigned casts = %d, want 1", len(assigned))
	}
	if availability := enrichAssignedCastsWithSSA(pass, ssaRes, declaration, assigned); !availability.ready() {
		t.Fatalf("SSA alias enrichment unavailable: %+v", availability)
	}
	targetTransfer := program.targetEdgeTransfer(pass, assigned[0].target)

	var nilFacts, nonNilFacts int
	for _, edge := range graph.Edges {
		for _, fact := range program.edgeResultFacts(edge) {
			if fact.Invocation.CallSite == "" || fact.Invocation.SummaryProvenance != protocolValidationDirectProvenance {
				t.Fatalf("incomplete invocation provenance: %+v", fact.Invocation)
			}
			if fact.Invocation.ReceiverExpr == nil {
				t.Fatalf("validation invocation has no receiver expression: %+v", fact.Invocation)
			}
			switch fact.Result {
			case protocolErrorResultNil:
				nilFacts++
				tag, reason := targetTransfer(edge, ideStateNeedsValidate)
				if tag != ideEdgeFuncValidate || reason != pathOutcomeReasonNone {
					t.Fatalf("nil-edge target transfer = (%s, %s), want validation; target=%+v receiver=%T:%s", tag, reason, assigned[0].target, fact.Invocation.ReceiverExpr, exprStringKey(fact.Invocation.ReceiverExpr))
				}
			case protocolErrorResultNonNil:
				nonNilFacts++
				tag, reason := targetTransfer(edge, ideStateNeedsValidate)
				if tag != ideEdgeFuncValidationFailed || reason != pathOutcomeReasonNone {
					t.Fatalf("non-nil-edge target transfer = (%s, %s), want failed validation", tag, reason)
				}
			default:
				t.Fatalf("validation edge has unknown result: %+v", fact)
			}
		}
	}
	if nilFacts != 1 || nonNilFacts != 1 {
		t.Fatalf("projected validation facts: nil=%d nonnil=%d, want one each; program=%+v edges=%+v", nilFacts, nonNilFacts, program, graph.Edges)
	}

	functionCFG := buildProtocolCFG(pass, declaration.Body, ssaRes)
	defBlock, defIndex := findDefiningBlock(functionCFG, assigned[0].assign)
	result := newInterprocSolverWithSSA(pass, ssaRes).EvaluateCastPath(interprocCastPathInput{
		Decl:            declaration,
		CFG:             functionCFG,
		DefBlock:        defBlock,
		DefIdx:          defIndex,
		Target:          assigned[0].target,
		TypeName:        "Value",
		OriginKey:       "validation-program-test",
		MaxStates:       defaultCFGMaxStates,
		SSAAvailability: ssaRes.Availability,
	})
	if result.Class != interprocOutcomeSafe {
		t.Fatalf("edge-conditioned solver result = %+v, want safe", result)
	}
}

func TestProtocolConcurrentCaptureRemainsInconclusive(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Value string
func (value Value) Validate() error { return nil }
func use(Value) {}

func Probe(raw string) error {
	value := Value(raw)
	go func() { use(value) }()
	return value.Validate()
}
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	declaration := findFuncDecl(t, file, "Probe")
	assigned, _, _, _ := collectCFACasts(pass, declaration.Body, buildParentMap(declaration.Body), func(*ast.FuncLit, int) {})
	if len(assigned) != 1 {
		t.Fatalf("assigned casts = %d, want 1", len(assigned))
	}
	if availability := enrichAssignedCastsWithSSA(pass, ssaRes, declaration, assigned); !availability.ready() {
		t.Fatalf("SSA alias enrichment unavailable: %+v", availability)
	}
	goStatement, ok := declaration.Body.List[1].(*ast.GoStmt)
	if !ok {
		t.Fatalf("second statement = %T, want *ast.GoStmt", declaration.Body.List[1])
	}
	if !nodeHasConcurrentTargetUse(pass, declaration, goStatement, buildParentMap(declaration.Body), assigned[0].target) {
		t.Fatalf("goroutine capture was not recognized for target %+v", assigned[0].target)
	}
	functionCFG := buildProtocolCFG(pass, declaration.Body, ssaRes)
	graph := buildInterprocSupergraphForFunc(pass, declaration, ssaRes)
	concurrentNodes := 0
	goEvents := 0
	goEventsReferencingTarget := 0
	for nodeKey, graphNode := range graph.NodeAST {
		if nodeHasConcurrentTargetUse(pass, declaration, graphNode, buildParentMap(declaration.Body), assigned[0].target) {
			concurrentNodes++
		}
		nodeID, exists := graph.Nodes[nodeKey]
		if !exists {
			continue
		}
		event, exists := graph.callEvent(nodeID)
		if !exists || nodeID.Kind != interprocNodeKindCall || event.Phase != protocolCallEventGo {
			continue
		}
		goEvents++
		if graphCallReferencesTarget(graph, nodeID, pass, assigned[0].target) {
			goEventsReferencingTarget++
		}
	}
	if concurrentNodes == 0 {
		t.Fatalf("supergraph has no concurrent target node: %+v", graph.NodeAST)
	}
	if goEvents != 1 || goEventsReferencingTarget != 1 {
		t.Fatalf("go events = %d, target-referencing go events = %d, want 1 each", goEvents, goEventsReferencingTarget)
	}
	defBlock, defIndex := findDefiningBlock(functionCFG, assigned[0].assign)
	result := newInterprocSolverWithSSA(pass, ssaRes).EvaluateCastPath(interprocCastPathInput{
		Decl: declaration, CFG: functionCFG, DefBlock: defBlock, DefIdx: defIndex,
		Target: assigned[0].target, TypeName: "Value", OriginKey: "concurrent-capture-test",
		MaxStates:       defaultCFGMaxStates,
		SSAAvailability: ssaRes.Availability,
	})
	if result.Class != interprocOutcomeInconclusive || result.Reason != pathOutcomeReasonConcurrentMutation {
		t.Fatalf("concurrent capture result = %+v, want inconclusive concurrent-mutation", result)
	}
}

func TestProtocolValidationProgramResolvesReceiverIdentityAtInvocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   []protocolAliasResolution
	}{
		{
			name: "var declaration",
			source: `package probe
type Value string
func (value Value) Validate() error { return nil }
func Probe(raw string) error {
	var value Value = Value(raw)
	if err := value.Validate(); err != nil { return err }
	return nil
}`,
			want: []protocolAliasResolution{protocolAliasMust},
		},
		{
			name: "canonical selector",
			source: `package probe
type Value string
func (value Value) Validate() error { return nil }
type Holder struct { Value Value }
func Probe(raw string, holder *Holder) error {
	(*holder).Value = Value(raw)
	if err := holder.Value.Validate(); err != nil { return err }
	return nil
}`,
			want: []protocolAliasResolution{protocolAliasMust},
		},
		{
			name: "shadowed selector",
			source: `package probe
type Value string
func (value Value) Validate() error { return nil }
type Holder struct { Value Value }
func Probe(raw string, holder *Holder) error {
	{
		holder := &Holder{}
		holder.Value = Value(raw)
	}
	return holder.Value.Validate()
}`,
			want: []protocolAliasResolution{protocolAliasUnknown},
		},
		{
			name: "unrelated phi",
			source: `package probe
type Value string
func (value Value) Validate() error { return nil }
func Probe(raw string, choose bool) error {
	value := Value(raw)
	left := Value("left")
	right := Value("right")
	alias := left
	if choose { alias = left } else { alias = right }
	_ = alias.Validate()
	return value.Validate()
}`,
			want: []protocolAliasResolution{protocolAliasUnknown, protocolAliasMust},
		},
		{
			name: "dynamic index",
			source: `package probe
type Value string
func (value Value) Validate() error { return nil }
func Probe(raw string, index int) error {
	values := []Value{"zero", "one"}
	values[index] = Value(raw)
	index = 1
	return values[index].Validate()
}`,
			want: []protocolAliasResolution{protocolAliasAmbiguous},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pass, file := buildTypedPassFromSource(t, tt.source)
			ssaResult := buildSSAForPass(pass)
			declaration := findFuncDecl(t, file, "Probe")
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
			program := buildProtocolValidationProgram(pass, ssaResult, nil)
			var got []protocolAliasResolution
			for _, invocations := range program.invocationsByCall {
				for _, invocation := range invocations {
					if invocation.Call.Parent() == assigned[0].target.flowAliases.castValue.Parent() {
						got = append(got, protocolInvocationTargetResolution(pass, assigned[0].target, invocation))
					}
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("invocation resolutions = %v, want %v; program=%+v target=%+v stores=%v", got, tt.want, program, assigned[0].target, assigned[0].target.flowAliases.castStoreAddresses)
			}
			if len(tt.want) == 1 && tt.want[0] == protocolAliasMust {
				graph := buildInterprocSupergraphForFunc(pass, declaration, ssaResult)
				transfer := program.targetEdgeTransfer(pass, assigned[0].target)
				var nilTransfers, nonNilTransfers int
				for _, edge := range graph.Edges {
					for _, fact := range program.edgeResultFacts(edge) {
						tag, reason := transfer(edge, ideStateNeedsValidate)
						if reason != pathOutcomeReasonNone {
							t.Fatalf("edge transfer reason = %s, want none", reason)
						}
						switch fact.Result {
						case protocolErrorResultNil:
							if tag == ideEdgeFuncValidate {
								nilTransfers++
							}
						case protocolErrorResultNonNil:
							if tag == ideEdgeFuncValidationFailed {
								nonNilTransfers++
							}
						case protocolErrorResultUnknown:
						}
					}
				}
				if nilTransfers != 1 || nonNilTransfers != 1 {
					t.Fatalf("conditional transfers = (nil=%d, nonnil=%d), want (1, 1); graph=%+v program=%+v", nilTransfers, nonNilTransfers, graph.Edges, program)
				}
				functionCFG := buildProtocolCFG(pass, declaration.Body, ssaResult)
				defBlock, defIndex := findDefiningBlock(functionCFG, assigned[0].assign)
				result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateCastPath(interprocCastPathInput{
					Decl: declaration, CFG: functionCFG, DefBlock: defBlock, DefIdx: defIndex,
					Target: assigned[0].target, TypeName: "Value", OriginKey: "receiver-identity-test",
					MaxStates:       defaultCFGMaxStates,
					SSAAvailability: ssaResult.Availability,
				})
				if result.Class != interprocOutcomeSafe {
					t.Fatalf("edge-conditioned solver result = %+v, want safe", result)
				}
			}
		})
	}
}

func protocolCallBlockByCalleeName(t *testing.T, fn *ssa.Function, name string) *ssa.BasicBlock {
	t.Helper()
	for _, block := range fn.Blocks {
		for _, instruction := range block.Instrs {
			call, ok := instruction.(ssa.CallInstruction)
			if !ok {
				continue
			}
			callee := call.Common().StaticCallee()
			if callee != nil && callee.Name() == name {
				return block
			}
		}
	}
	t.Fatalf("SSA function %s has no call to %s", fn.Name(), name)
	return nil
}

func protocolBlockReaches(start, target *ssa.BasicBlock) bool {
	if start == nil || target == nil {
		return false
	}
	queue := []*ssa.BasicBlock{start}
	seen := make(map[*ssa.BasicBlock]struct{})
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if block == target {
			return true
		}
		if _, visited := seen[block]; visited {
			continue
		}
		seen[block] = struct{}{}
		queue = append(queue, block.Succs...)
	}
	return false
}
