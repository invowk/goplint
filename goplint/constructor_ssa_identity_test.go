// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestConstructorSSAIdentityUsesReachingReturnObject(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value struct{}
func (value *Value) Validate() error { return nil }
func NewValue() *Value {
	first := &Value{}
	second := &Value{}
	result := first
	result = second
	return result
}`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	declaration := findFuncDecl(t, file, "NewValue")
	model, availability := buildConstructorSSAIdentityModel(
		pass,
		ssaResult,
		declaration,
		0,
	)
	if !availability.ready() {
		t.Fatalf("SSA availability = %+v, want ready", availability)
	}
	keys := model.returnObjectKeys()
	if len(keys) != 1 {
		t.Fatalf("return object keys = %v, want one reaching object", keys)
	}
	if len(model.uncertainReturns) != 0 {
		t.Fatalf("uncertain returns = %v, want none", model.uncertainReturns)
	}
}

func TestConstructorSSAIdentityTracksValidatedZeroValue(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value struct{}
func (Value) Validate() error { return nil }
func NewValue() (Value, error) {
	value := Value{}
	if err := value.Validate(); err != nil { return Value{}, err }
	return value, nil
}`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	declaration := findFuncDecl(t, file, "NewValue")
	model, availability := buildConstructorSSAIdentityModel(pass, ssaResult, declaration, 0)
	if !availability.ready() {
		t.Fatalf("SSA availability = %+v, want ready", availability)
	}
	if keys := model.returnObjectKeys(); len(keys) != 1 {
		var returns []string
		for _, block := range model.function.Blocks {
			for _, instruction := range block.Instrs {
				if returned, ok := instruction.(*ssa.Return); ok {
					returns = append(returns, fmt.Sprintf("pos=%d value=%T:%v", returned.Pos(), returned.Results[0], returned.Results[0]))
				}
			}
		}
		t.Fatalf("return object keys = %v, want one; uncertain=%v returns=%v", keys, model.uncertainReturns, returns)
	}
}

func TestConstructorSSAIdentityTracksMutatedValueAcrossReturns(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Plan struct { Base string; Patterns []string }
func (Plan) Validate() error { return nil }
func NewPlan(configured bool) (Plan, error) {
	plan := Plan{Patterns: []string{"*"}}
	if !configured {
		if err := plan.Validate(); err != nil { return Plan{}, err }
		return plan, nil
	}
	plan.Base = "work"
	if err := plan.Validate(); err != nil { return Plan{}, err }
	return plan, nil
}`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	declaration := findFuncDecl(t, file, "NewPlan")
	model, availability := buildConstructorSSAIdentityModel(
		pass,
		ssaResult,
		declaration,
		0,
	)
	if !availability.ready() {
		t.Fatalf("SSA availability = %+v, want ready", availability)
	}
	if keys := model.returnObjectKeys(); len(keys) != 1 {
		t.Fatalf("return object keys = %v, want one; uncertain=%v", keys, model.uncertainReturns)
	}
	result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateConstructorPath(interprocConstructorPathInput{
		Decl:            declaration,
		ReturnTypeKey:   typeIdentityKey(pass.TypesInfo.TypeOf(declaration.Type.Results.List[0].Type)),
		ResultSlot:      0,
		Constructor:     "probe.NewPlan",
		MaxStates:       defaultCFGMaxStates,
		SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaResult, declaration),
	})
	if result.Class != interprocOutcomeSafe {
		t.Fatalf("constructor result = %s (%s), want safe; witness=%+v", result.Class, result.Reason, result.WitnessEdges)
	}
}

func TestConstructorSSAIdentityHonorsResultSlotAndAmbiguousPhi(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value struct{}
func (value *Value) Validate() error { return nil }
func NewValue(flag bool) (error, *Value) {
	first := &Value{}
	second := &Value{}
	if flag {
		return nil, first
	}
	return nil, second
}`
	pass, file := buildTypedPassFromSource(t, source)
	model, availability := buildConstructorSSAIdentityModel(
		pass,
		buildSSAForPass(pass),
		findFuncDecl(t, file, "NewValue"),
		1,
	)
	if !availability.ready() {
		t.Fatalf("SSA availability = %+v, want ready", availability)
	}
	if keys := model.returnObjectKeys(); len(keys) != 2 {
		t.Fatalf("return object keys = %v, want two precise branch objects", keys)
	}
	if len(model.uncertainReturns) != 0 {
		t.Fatalf("uncertain returns = %v, want precise per-return identities", model.uncertainReturns)
	}
}

func TestConstructorSSAIdentityProjectsImmediateClosureBinding(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value struct{}
func (value *Value) Validate() error { return nil }
func NewValue() (*Value, error) {
	value := &Value{}
	validate := func() error { return value.Validate() }
	if err := validate(); err != nil { return nil, err }
	return value, nil
}`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	declaration := findFuncDecl(t, file, "NewValue")
	model, availability := buildConstructorSSAIdentityModel(
		pass,
		ssaResult,
		declaration,
		0,
	)
	if !availability.ready() {
		t.Fatalf("SSA availability = %+v, want ready", availability)
	}
	keys := model.returnObjectKeys()
	if len(keys) != 1 {
		t.Fatalf("return object keys = %v, want one", keys)
	}
	target, ok := model.targetForObject(keys[0])
	if !ok || target.flowAliases == nil || len(target.flowAliases.closureValues) == 0 {
		t.Fatalf("closure bindings = %+v, want exact projected free variable", target.flowAliases)
	}
	_, _, closureCalls, _ := collectCFACasts(
		pass,
		declaration.Body,
		buildParentMap(declaration.Body),
		func(*ast.FuncLit, int) {},
	)
	methodCalls := collectSynchronousClosureValidationCalls(collectSynchronousClosureVarCalls(closureCalls))
	program := buildProtocolValidationProgram(pass, ssaResult, methodCalls)
	found := false
	var invocationDetails []string
	for _, invocations := range program.invocationsByCall {
		for _, invocation := range invocations {
			resolution := protocolInvocationTargetResolution(pass, target, invocation)
			invocationDetails = append(invocationDetails, fmt.Sprintf("receiver=%T:%v parent=%v resolution=%d", invocation.Receiver, invocation.Receiver, invocation.Call.Parent(), resolution))
			if resolution == protocolAliasMust {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf(
			"closure validation invocation did not retain the captured return identity: method_calls=%d invocation_sites=%d captured=%v closure_values=%v invocations=%v",
			len(methodCalls),
			len(program.invocationsByCall),
			target.flowAliases.capturedBindings,
			target.flowAliases.closureValues,
			invocationDetails,
		)
	}
}

func TestConstructorSSAIdentitySchedulesDeferredValidationAtReturn(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value struct{}
func (value *Value) Validate() error { return nil }
func NewValue() (value *Value, err error) {
	value = &Value{}
	defer func() {
		validationError := value.Validate()
		if err == nil { err = validationError }
	}()
	return value, nil
}`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	declaration := findFuncDecl(t, file, "NewValue")
	model, _ := buildConstructorSSAIdentityModel(pass, ssaResult, declaration, 0)
	result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateConstructorPath(interprocConstructorPathInput{
		Decl:            declaration,
		ReturnTypeKey:   typeIdentityKey(pass.TypesInfo.TypeOf(declaration.Type.Results.List[0].Type)),
		ResultSlot:      0,
		Constructor:     "probe.NewValue",
		MaxStates:       defaultCFGMaxStates,
		SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaResult, declaration),
	})
	if result.Class != interprocOutcomeSafe {
		t.Fatalf("constructor result = %s (%s), want safe; keys=%v uncertain=%v witness=%+v terminal=%+v", result.Class, result.Reason, model.returnObjectKeys(), model.uncertainReturns, result.WitnessEdges, result.WitnessTerminal)
	}
}

func TestConstructorDeferredValidationUsesExitTimeLIFOPaths(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value struct{}
func (value *Value) Validate() error { return nil }
type DeferredEffect interface { Apply(*Value, *error) }

func Conditional(validate bool) (value *Value, err error) {
	value = &Value{}
	defer func() {
		if validate {
			validationError := value.Validate()
			if err == nil { err = validationError }
		}
	}()
	return value, nil
}

func ValidationThenOverwrite() (value *Value, err error) {
	value = &Value{}
	defer func() { err = nil }()
	defer func() {
		validationError := value.Validate()
		if err == nil { err = validationError }
	}()
	return value, nil
}

func ValidationThenPreserve() (value *Value, err error) {
	value = &Value{}
	defer func() { _ = err }()
	defer func() {
		validationError := value.Validate()
		if err == nil { err = validationError }
	}()
	return value, nil
}

func Unresolved(effect DeferredEffect) (value *Value, err error) {
	value = &Value{}
	defer effect.Apply(value, &err)
	return value, nil
}

func CaptureRebound() (result *Value, err error) {
	value := &Value{}
	result = value
	defer func() {
		validationError := value.Validate()
		if err == nil { err = validationError }
	}()
	value = &Value{}
	return result, nil
}

func Repeated(count int) (value *Value, err error) {
	value = &Value{}
	for index := 0; index < count; index++ {
		defer func() {
			validationError := value.Validate()
			if err == nil { err = validationError }
		}()
	}
	return value, nil
}
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	tests := []struct {
		name       string
		wantClass  interprocOutcomeClass
		wantReason pathOutcomeReason
	}{
		{name: "Conditional", wantClass: interprocOutcomeInconclusive, wantReason: pathOutcomeReasonUnresolvedTarget},
		{name: "ValidationThenOverwrite", wantClass: interprocOutcomeInconclusive, wantReason: pathOutcomeReasonUnresolvedTarget},
		{name: "ValidationThenPreserve", wantClass: interprocOutcomeSafe},
		{name: "Unresolved", wantClass: interprocOutcomeInconclusive, wantReason: pathOutcomeReasonUnresolvedTarget},
		{name: "CaptureRebound", wantClass: interprocOutcomeInconclusive, wantReason: pathOutcomeReasonAmbiguousIdentity},
		{name: "Repeated", wantClass: interprocOutcomeInconclusive, wantReason: pathOutcomeReasonFeasibilityUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			declaration := findFuncDecl(t, file, tt.name)
			result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateConstructorPath(interprocConstructorPathInput{
				Decl: declaration, ReturnTypeKey: typeIdentityKey(pass.TypesInfo.TypeOf(declaration.Type.Results.List[0].Type)),
				ResultSlot: 0, Constructor: "probe." + tt.name, MaxStates: defaultCFGMaxStates,
				SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaResult, declaration),
			})
			if result.Class != tt.wantClass || result.Reason != tt.wantReason {
				t.Fatalf("constructor result = %s (%s), want %s (%s); witness=%+v", result.Class, result.Reason, tt.wantClass, tt.wantReason, result.WitnessEdges)
			}
		})
	}
}

func TestConstructorSSAIdentityKeepsGenericInstantiationsDistinct(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Box[T any] struct{ Value T }
func (box Box[T]) Validate() error { return nil }
func validateStringBox(box Box[string]) error { return box.Validate() }
func NewStringBox(raw string) (Box[string], error) {
	box := Box[string]{Value: raw}
	if err := validateStringBox(box); err != nil { return Box[string]{}, err }
	return box, nil
}
func NewIntBox(raw int) (Box[int], error) {
	box := Box[int]{Value: raw}
	if err := validateStringBox(Box[string]{Value: "ok"}); err != nil { return Box[int]{}, err }
	return box, nil
}
type Partial struct{}
func (partial *Partial) Validate() error { return nil }
func maybePartial(validate bool) (*Partial, error) {
	partial := &Partial{}
	if !validate { return partial, nil }
	if err := partial.Validate(); err != nil { return nil, err }
	return partial, nil
}
func NewPartial(validate bool) (*Partial, error) {
	return maybePartial(validate)
}`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	tests := []struct {
		name string
		want interprocOutcomeClass
	}{
		{name: "NewStringBox", want: interprocOutcomeSafe},
		{name: "NewIntBox", want: interprocOutcomeUnsafe},
		{name: "NewPartial", want: interprocOutcomeUnsafe},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			declaration := findFuncDecl(t, file, tt.name)
			model, _ := buildConstructorSSAIdentityModel(pass, ssaResult, declaration, 0)
			result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateConstructorPath(interprocConstructorPathInput{
				Decl:            declaration,
				ReturnTypeKey:   typeIdentityKey(pass.TypesInfo.TypeOf(declaration.Type.Results.List[0].Type)),
				ResultSlot:      0,
				Constructor:     "probe." + tt.name,
				MaxStates:       defaultCFGMaxStates,
				SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaResult, declaration),
			})
			if result.Class != tt.want {
				functionObject, _ := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
				resolution := resolveSSAFunction(ssaResult, functionObject)
				var returns []string
				for _, block := range resolution.Function.Blocks {
					for _, instruction := range block.Instrs {
						if returned, ok := instruction.(*ssa.Return); ok {
							returns = append(returns, fmt.Sprintf("pos=%d value=%T:%v", returned.Pos(), returned.Results[0], returned.Results[0]))
						}
					}
				}
				t.Fatalf("result = %s (%s), want %s; keys=%v uncertain=%v returns=%v witness=%+v", result.Class, result.Reason, tt.want, model.returnObjectKeys(), model.uncertainReturns, returns, result.WitnessEdges)
			}
		})
	}
}
