// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"
	"reflect"
	"sync"
	"testing"
)

func TestLocalCalleeSummariesUseExplicitEffects(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (v *Value) Validate() error { return nil }
func (v *Value) String() string { return string(*v) }

func Pure(value *Value) error { return nil }
func Preserve(value *Value) error { _ = value; return nil }
func Consume(value *Value) error { _ = value.String(); return nil }
func Mutate(value *Value) error { *value = "changed"; return nil }
func Replace(value, source *Value) error { *value = *source; return nil }
func Terminal(value *Value) error { panic("stop") }
func MutateThenValidate(value *Value) error { *value = "changed"; return value.Validate() }
func ValidateThenMutate(value *Value) error {
	err := value.Validate()
	*value = "changed"
	return err
}
func ValidateValue(value *Value) error { return value.Validate() }
func MustValidate(value *Value) {
	if err := value.Validate(); err != nil { panic(err) }
}

var escaped *Value
func Escape(value *Value) error { escaped = value; return nil }
`
	pass, file := buildTypedPassFromSource(t, source)
	want := map[string]string{
		"Pure":          protocolSummaryEffectPure,
		"Preserve":      protocolSummaryEffectPreserve,
		"Consume":       protocolSummaryEffectConsume,
		"Mutate":        protocolSummaryEffectMutate,
		"Replace":       protocolSummaryEffectReplace,
		"Terminal":      protocolSummaryEffectTerminal,
		"ValidateValue": protocolSummaryEffectValidate,
		"MustValidate":  protocolSummaryEffectValidate,
		"Escape":        protocolSummaryEffectEscape,
	}
	for functionName, effectKind := range want {
		declaration := findFuncDecl(t, file, functionName)
		function, ok := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
		if !ok {
			t.Fatalf("%s type object is not a function", functionName)
		}
		summary, summaryOK, reason := calleeSummaryForFuncSlotWithStack(
			pass,
			function,
			calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0},
			summaryStackScope{seen: make(map[string]bool)},
			&sync.Map{},
		)
		if !summaryOK || reason != pathOutcomeReasonNone {
			t.Errorf("%s summary = (%+v, %v, %q), want complete summary", functionName, summary, summaryOK, reason)
			continue
		}
		if !summary.Complete || !summary.hasEffect(effectKind) {
			t.Errorf("%s summary effects = %+v, want %q", functionName, summary.Effects, effectKind)
		}
		if functionName == "Replace" {
			var replacement *ProtocolSummaryEffectFact
			for idx := range summary.Effects {
				if summary.Effects[idx].Kind == protocolSummaryEffectReplace {
					replacement = &summary.Effects[idx]
					break
				}
			}
			if replacement == nil {
				t.Error("Replace summary is missing its replacement effect")
			} else if replacement.SourceKind != protocolSummaryTargetParameter || replacement.SourceSlot != 1 {
				t.Errorf("Replace source = (%q, %d), want parameter slot 1", replacement.SourceKind, replacement.SourceSlot)
			}
		}
	}

	mustValidate := findFuncDecl(t, file, "MustValidate")
	mustValidateObject, ok := pass.TypesInfo.Defs[mustValidate.Name].(*types.Func)
	if !ok {
		t.Fatal("MustValidate type object is not a function")
	}
	mustValidateSummary, summaryOK, reason := calleeSummaryForFuncSlotWithStack(
		pass,
		mustValidateObject,
		calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0},
		summaryStackScope{seen: make(map[string]bool)},
		&sync.Map{},
	)
	if !summaryOK || reason != pathOutcomeReasonNone || !mustValidateSummary.hasSuccessfulReturnValidation() {
		t.Fatalf("MustValidate summary = (%+v, %t, %q), want successful-return validation", mustValidateSummary, summaryOK, reason)
	}
	for _, effect := range mustValidateSummary.Effects {
		if effect.Kind == protocolSummaryEffectValidate && effect.ConditionResultSlot >= 0 {
			t.Fatalf("MustValidate fabricated condition result slot %d", effect.ConditionResultSlot)
		}
	}

	for functionName, wantKinds := range map[string][]string{
		"MutateThenValidate": {protocolSummaryEffectMutate, protocolSummaryEffectValidate},
		"ValidateThenMutate": {protocolSummaryEffectValidate, protocolSummaryEffectMutate},
	} {
		declaration := findFuncDecl(t, file, functionName)
		function, ok := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
		if !ok {
			t.Fatalf("%s type object is not a function", functionName)
		}
		summary, summaryOK, reason := calleeSummaryForFuncSlotWithStack(
			pass,
			function,
			calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0},
			summaryStackScope{seen: make(map[string]bool)},
			&sync.Map{},
		)
		if !summaryOK || reason != pathOutcomeReasonNone {
			t.Fatalf("%s summary = (%+v, %v, %q), want complete summary", functionName, summary, summaryOK, reason)
		}
		gotKinds := make([]string, 0, len(summary.Effects))
		for _, effect := range summary.Effects {
			gotKinds = append(gotKinds, effect.Kind)
		}
		if !reflect.DeepEqual(gotKinds, wantKinds) {
			t.Errorf("%s ordered effects = %v, want %v", functionName, gotKinds, wantKinds)
		}
	}
}

func TestRecursiveCalleeSummaryConvergesConditionalValidation(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value Value) Validate() error { return nil }
func recursiveValidate(value Value, depth int) error {
	if depth <= 0 { return value.Validate() }
	return recursiveValidate(value, depth-1)
}
func mutualValidateA(value Value, depth int) error {
	if depth <= 0 { return value.Validate() }
	return mutualValidateB(value, depth-1)
}
func mutualValidateB(value Value, depth int) error {
	return mutualValidateA(value, depth)
}
func recursiveWithoutBase(value Value) error {
	return recursiveWithoutBase(value)
}`
	pass, file := buildTypedPassFromSource(t, source)
	for _, functionName := range []string{"recursiveValidate", "mutualValidateA", "mutualValidateB"} {
		declaration := findFuncDecl(t, file, functionName)
		function, ok := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
		if !ok {
			t.Fatalf("%s type object is not a function", functionName)
		}
		summary, summaryOK, reason := calleeSummaryForFuncSlotWithStack(
			pass,
			function,
			calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0},
			summaryStackScope{seen: make(map[string]bool)},
			&sync.Map{},
		)
		if !summaryOK || reason != pathOutcomeReasonNone || !summary.conditionallyValidatesWithoutHazard() {
			t.Fatalf("%s summary = (%+v, %t, %s), want complete conditional validation", functionName, summary, summaryOK, reason)
		}
	}

	declaration := findFuncDecl(t, file, "recursiveWithoutBase")
	function, ok := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
	if !ok {
		t.Fatal("recursiveWithoutBase type object is not a function")
	}
	ssaResult := buildSSAForPass(pass)
	resolution := resolveSSAFunction(ssaResult, function)
	if !resolution.Availability.ready() {
		t.Fatalf("recursiveWithoutBase SSA unavailable: %+v", resolution.Availability)
	}
	if _, ok := buildProtocolRecursiveSummary(
		resolution.Function,
		calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0},
	); ok {
		t.Fatal("base-free recursive cycle was classified as conditional validation")
	}
}

func TestRecursiveCalleeSummaryPreservesOrderedEffects(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value *Value) Validate() error { return nil }
func recursiveMutateThenValidate(value *Value, depth int) error {
	if depth <= 0 { return value.Validate() }
	*value = "changed"
	return recursiveMutateThenValidate(value, depth-1)
}
func recursiveValidateThenMutate(value *Value, depth int) error {
	if depth <= 0 { return value.Validate() }
	err := recursiveValidateThenMutate(value, depth-1)
	*value = "changed"
	return err
}
func mutualMutateThenValidateA(value *Value, depth int) error {
	if depth <= 0 { return value.Validate() }
	return mutualMutateThenValidateB(value, depth-1)
}
func mutualMutateThenValidateB(value *Value, depth int) error {
	*value = "changed"
	return mutualMutateThenValidateA(value, depth)
}
`
	pass, file := buildTypedPassFromSource(t, source)
	tests := []struct {
		functionName  string
		wantKinds     []string
		wantValidates bool
	}{
		{
			functionName:  "recursiveMutateThenValidate",
			wantKinds:     []string{protocolSummaryEffectMutate, protocolSummaryEffectValidate},
			wantValidates: true,
		},
		{
			functionName: "recursiveValidateThenMutate",
			wantKinds:    []string{protocolSummaryEffectValidate, protocolSummaryEffectMutate},
		},
		{
			functionName:  "mutualMutateThenValidateA",
			wantKinds:     []string{protocolSummaryEffectMutate, protocolSummaryEffectValidate},
			wantValidates: true,
		},
		{
			functionName:  "mutualMutateThenValidateB",
			wantKinds:     []string{protocolSummaryEffectMutate, protocolSummaryEffectValidate},
			wantValidates: true,
		},
	}
	for _, test := range tests {
		t.Run(test.functionName, func(t *testing.T) {
			t.Parallel()

			declaration := findFuncDecl(t, file, test.functionName)
			function, ok := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
			if !ok {
				t.Fatalf("%s type object is not a function", test.functionName)
			}
			summary, summaryOK, reason := calleeSummaryForFuncSlotWithStack(
				pass,
				function,
				calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0},
				summaryStackScope{seen: make(map[string]bool)},
				&sync.Map{},
			)
			if !summaryOK || reason != pathOutcomeReasonNone || !summary.Complete {
				t.Fatalf("summary = (%+v, %t, %s), want complete recursive summary", summary, summaryOK, reason)
			}
			gotKinds := make([]string, 0, len(summary.Effects))
			for _, effect := range summary.Effects {
				gotKinds = append(gotKinds, effect.Kind)
			}
			if !reflect.DeepEqual(gotKinds, test.wantKinds) {
				t.Fatalf("ordered effects = %v, want %v", gotKinds, test.wantKinds)
			}
			if got := summary.conditionallyValidatesWithoutHazard(); got != test.wantValidates {
				t.Errorf("conditionallyValidatesWithoutHazard() = %t, want %t", got, test.wantValidates)
			}
			for _, effect := range summary.Effects {
				if effect.Kind != protocolSummaryEffectValidate {
					continue
				}
				if effect.Condition != protocolSummaryConditionResultNil || effect.ConditionResultSlot != 0 {
					t.Errorf("recursive validation relation = %+v, want exact result slot 0", effect)
				}
			}
		})
	}
}

func TestCalleeSummaryInProgressEntryUsesSSARecursiveConvergence(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value Value) Validate() error { return nil }
func recursiveValidate(value Value, depth int) error {
	if depth <= 0 { return value.Validate() }
	return recursiveValidate(value, depth-1)
}`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "recursiveValidate")
	function, ok := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
	if !ok {
		t.Fatal("recursiveValidate type object is not a function")
	}
	ssaResult := buildSSAForPass(pass)
	slot := calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0}
	cacheKey := objectKey(function) + "|" + slot.cacheKey()
	cache := &sync.Map{}
	cache.Store(cacheKey, calleeSummaryEntry{
		reason:     pathOutcomeReasonRecursionCycle,
		inProgress: true,
		ssa:        ssaResult,
	})

	summary, summaryOK, reason := calleeSummaryForFuncSlotWithStack(
		pass,
		function,
		slot,
		stackScopeFromMap(nil, ssaResult),
		cache,
	)
	if !summaryOK || reason != pathOutcomeReasonNone || !summary.conditionallyValidatesWithoutHazard() {
		t.Fatalf("in-progress summary = (%+v, %t, %s), want converged conditional validation", summary, summaryOK, reason)
	}
}

func TestCalleeSummaryEffectsAreAppliedInOrder(t *testing.T) {
	t.Parallel()

	target := newProtocolTargetSummaryEffect(protocolSummaryEffectMutate, protocolSummaryTargetParameter, 0)
	validation := newProtocolSummaryEffect(protocolSummaryTargetParameter, 0, 0)
	successfulReturnValidation := newProtocolSuccessfulReturnSummaryEffect(protocolSummaryTargetParameter, 0)
	tests := []struct {
		name          string
		effects       []ProtocolSummaryEffectFact
		wantValidated bool
		wantHazard    bool
		wantComplete  bool
	}{
		{
			name:         "mutation then validation",
			effects:      []ProtocolSummaryEffectFact{target, validation},
			wantComplete: true,
		},
		{
			name:          "successful return validation",
			effects:       []ProtocolSummaryEffectFact{target, successfulReturnValidation},
			wantValidated: true,
			wantComplete:  true,
		},
		{
			name:         "validation then mutation",
			effects:      []ProtocolSummaryEffectFact{validation, target},
			wantComplete: true,
		},
		{
			name: "escape before validation",
			effects: []ProtocolSummaryEffectFact{
				newProtocolTargetSummaryEffect(protocolSummaryEffectEscape, protocolSummaryTargetParameter, 0),
				validation,
			},
			wantHazard:   true,
			wantComplete: true,
		},
		{
			name:         "incomplete summary",
			effects:      []ProtocolSummaryEffectFact{validation},
			wantComplete: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			summary := calleeTargetSummary{Effects: tt.effects, Complete: tt.wantComplete}
			state, complete := summary.protocolContinuationState()
			if complete != tt.wantComplete {
				t.Fatalf("protocolContinuationState() complete = %t, want %t", complete, tt.wantComplete)
			}
			if !complete {
				return
			}
			if state.validationProven() != tt.wantValidated || (state.Hazards != 0) != tt.wantHazard {
				t.Fatalf("protocolContinuationState() = %+v, want validated=%t hazard=%t", state, tt.wantValidated, tt.wantHazard)
			}
		})
	}
}

func TestPostValidationTransferAppliesOrderedLocalSummaries(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (v *Value) Validate() error { return nil }
func (v *Value) String() string { return string(*v) }
func Pure(value *Value) error { return nil }
func Preserve(value *Value) error { _ = value; return nil }
func Consume(value *Value) error { _ = value.String(); return nil }
func Mutate(value *Value) error { *value = "changed"; return nil }
func MutateThenValidate(value *Value) error { *value = "changed"; return value.Validate() }
func ValidateThenMutate(value *Value) error {
	err := value.Validate()
	*value = "changed"
	return err
}
var escaped *Value
func Escape(value *Value) error { escaped = value; return nil }
func Caller(value *Value) {
	Pure(value)
	Preserve(value)
	Consume(value)
	Mutate(value)
	MutateThenValidate(value)
	ValidateThenMutate(value)
	Escape(value)
}
`
	pass, file := buildTypedPassFromSource(t, source)
	caller := findFuncDecl(t, file, "Caller")
	target, ok := functionTargetForSlot(pass, caller, calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0})
	if !ok {
		t.Fatal("Caller parameter target is missing")
	}
	calls := make(map[string]*ast.CallExpr)
	ast.Inspect(caller.Body, func(node ast.Node) bool {
		call, callOK := node.(*ast.CallExpr)
		if !callOK {
			return true
		}
		if identifier, identOK := stripParens(call.Fun).(*ast.Ident); identOK {
			calls[identifier.Name] = call
		}
		return true
	})
	tests := []struct {
		name       string
		wantTag    ideEdgeFuncTag
		wantReason pathOutcomeReason
	}{
		{name: "Pure", wantTag: ideEdgeFuncIdentity},
		{name: "Preserve", wantTag: ideEdgeFuncIdentity},
		{name: "Consume", wantTag: ideEdgeFuncIdentity},
		{name: "Mutate", wantTag: ideEdgeFuncInvalidate},
		{name: "MutateThenValidate", wantTag: ideEdgeFuncInvalidate},
		{name: "ValidateThenMutate", wantTag: ideEdgeFuncInvalidate},
		{name: "Escape", wantTag: ideEdgeFuncIdentity, wantReason: pathOutcomeReasonEscapedHeapMutation},
	}
	cache := &sync.Map{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			call := calls[tt.name]
			if call == nil {
				t.Fatalf("call to %s is missing", tt.name)
			}
			gotTag, gotReason := postValidationTargetEffect(pass, call, target, cache)
			if gotTag != tt.wantTag || gotReason != tt.wantReason {
				t.Fatalf("postValidationTargetEffect(%s) = (%q, %q), want (%q, %q)",
					tt.name, gotTag, gotReason, tt.wantTag, tt.wantReason)
			}
		})
	}
}

func TestPostValidationTransferDoesNotEscapeValueCopy(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (v Value) Validate() error { return nil }
var escaped Value
func EscapeCopy(value Value) { escaped = value }
func Caller(value Value) { EscapeCopy(value) }
`
	pass, file := buildTypedPassFromSource(t, source)
	caller := findFuncDecl(t, file, "Caller")
	target, ok := functionTargetForSlot(pass, caller, calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0})
	if !ok {
		t.Fatal("Caller parameter target is missing")
	}
	call := caller.Body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr)
	tag, reason := postValidationTargetEffect(pass, call, target, &sync.Map{})
	if tag != ideEdgeFuncIdentity || reason != pathOutcomeReasonNone {
		t.Fatalf("postValidationTargetEffect(value copy) = (%q, %q), want identity/none", tag, reason)
	}
}

func TestPostValidationNonCallTransferDistinguishesMutableEscapesFromValueCopies(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func Probe(value Value, pointers []*Value, sink **Value, pointerCh chan *Value, valueCh chan Value) {
	pointerCh <- &value
	pointers[0] = &value
	*sink = &value
	callback := func() { value = "changed" }
	_ = callback
	copied := value
	valueCh <- value
	_ = copied
}`
	pass, file := buildTypedPassFromSource(t, source)
	probe := findFuncDecl(t, file, "Probe")
	target, ok := functionTargetForSlot(pass, probe, calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0})
	if !ok {
		t.Fatal("Probe value target is missing")
	}
	tests := []struct {
		name       string
		statement  int
		wantReason pathOutcomeReason
	}{
		{name: "pointer channel", statement: 0, wantReason: pathOutcomeReasonEscapedHeapMutation},
		{name: "aggregate pointer store", statement: 1, wantReason: pathOutcomeReasonEscapedHeapMutation},
		{name: "indirect pointer store", statement: 2, wantReason: pathOutcomeReasonEscapedHeapMutation},
		{name: "closure capture", statement: 3, wantReason: pathOutcomeReasonEscapedHeapMutation},
		{name: "immutable local copy", statement: 5},
		{name: "immutable channel copy", statement: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tag, reason := postValidationNonCallTargetEffect(pass, probe.Body.List[tt.statement], target)
			if tag != ideEdgeFuncIdentity || reason != tt.wantReason {
				t.Fatalf("postValidationNonCallTargetEffect() = (%q, %q), want (%q, %q)",
					tag, reason, ideEdgeFuncIdentity, tt.wantReason)
			}
		})
	}
}

func TestPostValidationNonCallTransferKeepsSynchronousReadsAndIIFEsValidated(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Record struct{ Name string }
func Probe(record *Record) {
	_ = record.Name
	func() { _ = record.Name }()
}`
	pass, file := buildTypedPassFromSource(t, source)
	probe := findFuncDecl(t, file, "Probe")
	target, ok := functionTargetForSlot(pass, probe, calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: 0})
	if !ok {
		t.Fatal("Probe pointer target is missing")
	}
	for index, statement := range probe.Body.List {
		tag, reason := postValidationNonCallTargetEffect(pass, statement, target)
		if tag != ideEdgeFuncIdentity || reason != pathOutcomeReasonNone {
			t.Fatalf("statement %d post-validation transfer = (%q, %q), want identity/none", index, tag, reason)
		}
	}
}
