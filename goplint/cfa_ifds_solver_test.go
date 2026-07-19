// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestInterprocSolverCastPathNilDefinitionIsInconclusive(t *testing.T) {
	t.Parallel()

	solver := newInterprocSolver(nil)
	input := interprocCastPathInput{
		TypeName:        "pkg.Type",
		OriginKey:       "cast-pos",
		SSAAvailability: ssaAvailability{Status: ssaAvailabilityReady},
	}

	result := solver.EvaluateCastPath(input)
	if result.Class != interprocOutcomeInconclusive {
		t.Fatalf("class = %q, want %q", result.Class, interprocOutcomeInconclusive)
	}
	if result.Reason != pathOutcomeReasonUnresolvedTarget {
		t.Fatalf("reason = %q, want %q", result.Reason, pathOutcomeReasonUnresolvedTarget)
	}
	if result.FactFamily != ifdsFactFamilyCastNeedsValidate {
		t.Fatalf("fact family = %q, want %q", result.FactFamily, ifdsFactFamilyCastNeedsValidate)
	}
}

func TestInterprocSolverUBVCrossBlockNilDefinitionIsInconclusive(t *testing.T) {
	t.Parallel()

	solver := newInterprocSolver(nil)
	result := solver.EvaluateUBVCrossBlock(interprocUBVCrossBlockInput{
		OriginKey:       "cast-pos",
		TypeName:        "pkg.Type",
		SSAAvailability: ssaAvailability{Status: ssaAvailabilityReady},
	})
	if result.Class != interprocOutcomeInconclusive {
		t.Fatalf("class = %q, want %q", result.Class, interprocOutcomeInconclusive)
	}
	if result.Reason != pathOutcomeReasonUnresolvedTarget {
		t.Fatalf("reason = %q, want %q", result.Reason, pathOutcomeReasonUnresolvedTarget)
	}
}

func TestInterprocSolverCheckedVoidFailureTerminatesCastAndExposesCrossBlockUBV(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value Value) Validate() error { return nil }
func use(Value) {}

func CheckedVoid(raw string) {
	value := Value(raw)
	if err := value.Validate(); err != nil { return }
	use(value)
}

func CrossBlockUseBeforeValidate(raw string, branch bool) {
	value := Value(raw)
	if branch { use(value) }
	if err := value.Validate(); err != nil { return }
}
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	tests := []struct {
		function string
		wantUBV  bool
	}{
		{function: "CheckedVoid"},
		{function: "CrossBlockUseBeforeValidate", wantUBV: true},
	}

	for _, tt := range tests {
		t.Run(tt.function, func(t *testing.T) {
			t.Parallel()

			declaration := findFuncDecl(t, file, tt.function)
			assigned, _, closureCalls, methodValueCalls := collectCFACasts(
				pass,
				declaration.Body,
				buildParentMap(declaration.Body),
				func(*ast.FuncLit, int) {},
			)
			if availability := enrichAssignedCastsWithSSA(pass, ssaResult, declaration, assigned); !availability.ready() {
				t.Fatalf("SSA enrichment = %+v, want ready", availability)
			}
			if len(assigned) != 1 {
				t.Fatalf("assigned casts = %d, want 1", len(assigned))
			}
			cast := assigned[0]
			cfg := buildProtocolCFG(pass, declaration.Body, ssaResult)
			definitionBlock, definitionIndex := findDefiningBlock(cfg, cast.assign)
			availability := protocolSSAAvailabilityForDecl(pass, ssaResult, declaration)
			methodCalls := collectMethodValueValidateCallSet(methodValueCalls)
			solver := newInterprocSolverWithSSA(pass, ssaResult)
			castResult := solver.EvaluateCastPath(interprocCastPathInput{
				Decl:            declaration,
				CFG:             cfg,
				DefBlock:        definitionBlock,
				DefIdx:          definitionIndex,
				Target:          cast.target,
				TypeName:        cast.typeName,
				OriginKey:       "checked-void-" + tt.function,
				SyncCalls:       collectSynchronousClosureVarCalls(closureCalls),
				MethodCalls:     methodCalls,
				MaxStates:       defaultCFGMaxStates,
				SSAAvailability: availability,
			})
			if castResult.Class != interprocOutcomeSafe {
				t.Fatalf("cast path = %+v, want safe", castResult)
			}
			if !tt.wantUBV {
				return
			}

			ubvInput := interprocUBVCrossBlockInput{
				Target:          cast.target,
				DefBlock:        definitionBlock,
				DefIdx:          definitionIndex,
				OriginKey:       "checked-void-ubv-" + tt.function,
				TypeName:        cast.typeName,
				SyncCalls:       collectUBVClosureVarCalls(closureCalls),
				MethodCalls:     methodCalls,
				MaxStates:       defaultCFGMaxStates,
				SSAAvailability: availability,
			}
			ubvResult := solver.EvaluateUBVCrossBlock(ubvInput)
			if ubvResult.Class != interprocOutcomeUnsafe {
				t.Fatalf("UBV path = %+v, want unsafe", ubvResult)
			}
			if got := classifyUBVFindingScope(ubvResult, ubvInput); got != ubvFindingScopeCrossBlock {
				t.Fatalf("UBV scope = %q, want %q", got, ubvFindingScopeCrossBlock)
			}
		})
	}
}

func TestInterprocSolverUBVCanonicalTraversalEdgeTransitions(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value Value) Validate() error { return nil }
func mutate(*Value) {}
var sink Value

func EscapeBeforeValidate(raw string) error {
	value := Value(raw)
	sink = value
	return value.Validate()
}

func PostValidationConcurrentNoSink(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	go mutate(&value)
	return nil
}

func PostValidationConcurrentSink(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	go mutate(&value)
	_ = value
	return nil
}

func UnrelatedConcurrentSink(raw string) error {
	value := Value(raw)
	other := Value(raw)
	if err := value.Validate(); err != nil { return err }
	go mutate(&other)
	_ = value
	return nil
}

func CrossBlockUse(raw string, branch bool) error {
	value := Value(raw)
	if branch { _ = value }
	return value.Validate()
}
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	tests := []struct {
		function   string
		wantClass  interprocOutcomeClass
		wantReason pathOutcomeReason
		wantEdgeFn ideEdgeFuncTag
		wantScope  ubvFindingScope
	}{
		{
			function:   "EscapeBeforeValidate",
			wantClass:  interprocOutcomeUnsafe,
			wantEdgeFn: ideEdgeFuncEscape,
			wantScope:  ubvFindingScopeSameBlock,
		},
		{
			function:   "PostValidationConcurrentNoSink",
			wantClass:  interprocOutcomeSafe,
			wantEdgeFn: ideEdgeFuncValidate,
		},
		{
			function:   "PostValidationConcurrentSink",
			wantClass:  interprocOutcomeInconclusive,
			wantReason: pathOutcomeReasonConcurrentMutation,
			wantEdgeFn: ideEdgeFuncIdentity,
		},
		{
			function:   "UnrelatedConcurrentSink",
			wantClass:  interprocOutcomeSafe,
			wantEdgeFn: ideEdgeFuncValidate,
		},
		{
			function:   "CrossBlockUse",
			wantClass:  interprocOutcomeUnsafe,
			wantEdgeFn: ideEdgeFuncEscape,
			wantScope:  ubvFindingScopeCrossBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.function, func(t *testing.T) {
			t.Parallel()

			declaration := findFuncDecl(t, file, tt.function)
			assigned, _, closureCalls, methodValueCalls := collectCFACasts(
				pass,
				declaration.Body,
				buildParentMap(declaration.Body),
				func(*ast.FuncLit, int) {},
			)
			if availability := enrichAssignedCastsWithSSA(pass, ssaResult, declaration, assigned); !availability.ready() {
				t.Fatalf("SSA enrichment = %+v, want ready", availability)
			}
			if len(assigned) == 0 {
				t.Fatal("expected an assigned Value cast")
			}
			target := assigned[0]
			cfg := buildProtocolCFG(pass, declaration.Body, ssaResult)
			definitionBlock, definitionIndex := findDefiningBlock(cfg, target.assign)
			availability := protocolSSAAvailabilityForDecl(pass, ssaResult, declaration)
			input := interprocUBVCrossBlockInput{
				Target:          target.target,
				DefBlock:        definitionBlock,
				DefIdx:          definitionIndex,
				OriginKey:       "ubv-transition-" + tt.function,
				TypeName:        target.typeName,
				SyncCalls:       collectSynchronousClosureVarCalls(closureCalls),
				MethodCalls:     collectMethodValueValidateCallSet(methodValueCalls),
				MaxStates:       defaultCFGMaxStates,
				SSAAvailability: availability,
			}
			result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateUBVCrossBlock(input)
			if result.Class != tt.wantClass {
				t.Fatalf("class = %q, want %q", result.Class, tt.wantClass)
			}
			if result.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", result.Reason, tt.wantReason)
			}
			if result.EdgeFunctionTag != tt.wantEdgeFn {
				t.Fatalf("edge function tag = %q, want %q", result.EdgeFunctionTag, tt.wantEdgeFn)
			}
			if tt.wantScope != "" {
				if got := classifyUBVFindingScope(result, input); got != tt.wantScope {
					t.Fatalf(
						"scope = %q, want %q; def=(%d,%d) path=%v terminal=%+v nodes=%T witness=%+v",
						got,
						tt.wantScope,
						definitionBlock.Index,
						definitionIndex,
						result.Witness,
						result.WitnessTerminal,
						definitionBlock.Nodes,
						result.WitnessEdges,
					)
				}
			}
		})
	}
}

func TestInterprocSolverUBVCrossBlockCarriesUncertaintyToTrackedSink(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value Value) Validate() error { return nil }
func mutate(value *Value) { *value = "changed" }
type Consumer interface { Consume(Value) }

func NoSink(raw string, branch bool) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	if branch { go mutate(&value) }
	return nil
}

func LaterSink(raw string, branch bool) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	if branch { go mutate(&value) }
	_ = value
	return nil
}

func UnrelatedEffect(raw string, branch bool) error {
	value := Value(raw)
	other := Value(raw)
	if err := value.Validate(); err != nil { return err }
	if branch { go mutate(&other) }
	_ = value
	return nil
}

func UnresolvedSink(raw string, consumer Consumer) error {
	value := Value(raw)
	consumer.Consume(value)
	return value.Validate()
}

func PanicAfterUncertainEffect(raw string, branch bool) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	if branch {
		go mutate(&value)
		panic("stop")
	}
	_ = value
	return nil
}

func ConsumeBeforePanic(raw string, branch bool) error {
	value := Value(raw)
	if branch {
		_ = value
		panic("stop")
	}
	return value.Validate()
}

func UnresolvedThenDefiniteSink(raw string, consumer Consumer) error {
	value := Value(raw)
	go consumer.Consume(value)
	_ = value
	return value.Validate()
}

func ReboundOutsideUncertaintySlice(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	alias := value
	go mutate(&alias)
	value = Value("replacement")
	_ = value
	return nil
}
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	tests := []struct {
		functionName string
		wantClass    interprocOutcomeClass
		wantReason   pathOutcomeReason
	}{
		{functionName: "NoSink", wantClass: interprocOutcomeSafe},
		{
			functionName: "LaterSink",
			wantClass:    interprocOutcomeInconclusive,
			wantReason:   pathOutcomeReasonConcurrentMutation,
		},
		{functionName: "UnrelatedEffect", wantClass: interprocOutcomeSafe},
		{
			functionName: "UnresolvedSink",
			wantClass:    interprocOutcomeInconclusive,
			wantReason:   pathOutcomeReasonUnresolvedTarget,
		},
		{functionName: "PanicAfterUncertainEffect", wantClass: interprocOutcomeSafe},
		{functionName: "ConsumeBeforePanic", wantClass: interprocOutcomeUnsafe},
		{functionName: "UnresolvedThenDefiniteSink", wantClass: interprocOutcomeUnsafe},
		{functionName: "ReboundOutsideUncertaintySlice", wantClass: interprocOutcomeSafe},
	}
	for _, tt := range tests {
		t.Run(tt.functionName, func(t *testing.T) {
			t.Parallel()

			declaration := findFuncDecl(t, file, tt.functionName)
			assigned, _, closureCalls, methodValueCalls := collectCFACasts(
				pass,
				declaration.Body,
				buildParentMap(declaration.Body),
				func(*ast.FuncLit, int) {},
			)
			if availability := enrichAssignedCastsWithSSA(pass, ssaResult, declaration, assigned); !availability.ready() {
				t.Fatalf("%s SSA enrichment = %+v, want ready", tt.functionName, availability)
			}
			var target cfaAssignedCast
			found := false
			for _, candidate := range assigned {
				if candidate.target.displayName == "value" {
					target = candidate
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s has no tracked value cast: %+v", tt.functionName, assigned)
			}
			cfg := buildProtocolCFG(pass, declaration.Body, ssaResult)
			definitionBlock, definitionIndex := findDefiningBlock(cfg, target.assign)
			availability := protocolSSAAvailabilityForDecl(pass, ssaResult, declaration)
			result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateUBVCrossBlock(interprocUBVCrossBlockInput{
				Target:          target.target,
				DefBlock:        definitionBlock,
				DefIdx:          definitionIndex,
				OriginKey:       "ubv-sink-" + tt.functionName,
				TypeName:        target.typeName,
				SyncCalls:       collectSynchronousClosureVarCalls(closureCalls),
				MethodCalls:     collectMethodValueValidateCallSet(methodValueCalls),
				MaxStates:       defaultCFGMaxStates,
				SSAAvailability: availability,
			})
			if result.Class != tt.wantClass || result.Reason != tt.wantReason {
				t.Fatalf("%s result = %s (%s), want %s (%s)", tt.functionName, result.Class, result.Reason, tt.wantClass, tt.wantReason)
			}
		})
	}
}
