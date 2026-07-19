// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestBuildProtocolSummaryFactIsConditionalAndSlotSensitive(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Value string
func (v Value) Validate() error { return nil }

func ValidateValue(value Value) error { return value.Validate() }
func (v Value) Check() error { return v.Validate() }
func Ignore(value Value) error {
	_ = value.Validate()
	return nil
}
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	if ssaRes == nil || ssaRes.Pkg == nil {
		t.Fatal("buildSSAForPass() returned no package")
	}

	helper := findMemberFunc(ssaRes.Pkg, "ValidateValue")
	if helper == nil {
		t.Fatal("SSA function ValidateValue is missing")
	}
	helperFact := buildProtocolSummaryFact(pass.Pkg.Path(), helper.Name(), helper)
	assertProtocolSummaryEffects(t, helperFact, []ProtocolSummaryEffectFact{
		newProtocolSummaryEffect(protocolSummaryTargetParameter, 0, 0),
	})

	methodDecl := findFuncDecl(t, file, "Check")
	methodObject, ok := pass.TypesInfo.Defs[methodDecl.Name].(*types.Func)
	if !ok {
		t.Fatal("type object for Check is not a function")
	}
	method := ssaFuncForTypesFunc(ssaRes, methodObject)
	if method == nil {
		t.Fatal("SSA method Check is missing")
	}
	methodFact := buildProtocolSummaryFact(pass.Pkg.Path(), method.Name(), method)
	assertProtocolSummaryEffects(t, methodFact, []ProtocolSummaryEffectFact{
		newProtocolSummaryEffect(protocolSummaryTargetReceiver, 0, 0),
	})

	ignored := findMemberFunc(ssaRes.Pkg, "Ignore")
	if ignored == nil {
		t.Fatal("SSA function Ignore is missing")
	}
	ignoredFact := buildProtocolSummaryFact(pass.Pkg.Path(), ignored.Name(), ignored)
	if len(ignoredFact.Effects) != 0 {
		t.Fatalf("unreturned validation result exported %d unconditional effects", len(ignoredFact.Effects))
	}
}

func TestBuildProtocolSummaryFactTracksValidatedConstructorResultIdentity(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Value string
func (v Value) Validate() error { return nil }

func NewValue(raw string) (Value, error) {
	value := Value(raw)
	return value, value.Validate()
}

func NewWrongValue(raw string) (Value, error) {
	validated := Value(raw)
	other := Value("other")
	return other, validated.Validate()
}

func NewPartiallyValidatedValue(raw string, skip bool) (Value, error) {
	value := Value(raw)
	if skip {
		return value, nil
	}
	return value, value.Validate()
}
`
	pass, _ := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	if ssaRes == nil || ssaRes.Pkg == nil {
		t.Fatal("buildSSAForPass() returned no package")
	}

	constructor := findMemberFunc(ssaRes.Pkg, "NewValue")
	if constructor == nil {
		t.Fatal("SSA function NewValue is missing")
	}
	assertProtocolSummaryEffects(t, buildProtocolSummaryFact(pass.Pkg.Path(), constructor.Name(), constructor), []ProtocolSummaryEffectFact{
		newProtocolSummaryEffect(protocolSummaryTargetResult, 0, 1),
	})

	for _, functionName := range []string{"NewWrongValue", "NewPartiallyValidatedValue"} {
		fn := findMemberFunc(ssaRes.Pkg, functionName)
		if fn == nil {
			t.Fatalf("SSA function %s is missing", functionName)
		}
		fact := buildProtocolSummaryFact(pass.Pkg.Path(), fn.Name(), fn)
		if len(fact.Effects) != 0 {
			t.Fatalf("%s exported unsound effects for a different or conditionally validated object: %+v", functionName, fact.Effects)
		}
	}
}

func TestValidateProtocolSummaryFactFailsClosed(t *testing.T) {
	t.Parallel()

	valid := ProtocolSummaryFact{
		FormatVersion:    protocolSummaryFactVersion,
		PackagePath:      "example.com/probe",
		FunctionName:     "ValidateValue",
		FunctionIdentity: "example.com/probe.ValidateValue",
		Complete:         true,
		Effects: []ProtocolSummaryEffectFact{
			newProtocolSummaryEffect(protocolSummaryTargetParameter, 0, 0),
		},
	}
	legacyVersion := valid
	legacyVersion.FormatVersion = 1
	versionMismatch := valid
	versionMismatch.FormatVersion = protocolSummaryFactVersion + 1
	tests := []struct {
		name        string
		fact        *ProtocolSummaryFact
		packagePath string
		want        protocolSummaryFactStatus
	}{
		{name: "valid", fact: &valid, packagePath: valid.PackagePath},
		{name: "missing", packagePath: valid.PackagePath, want: protocolSummaryFactMissing},
		{
			name:        "legacy version",
			fact:        &legacyVersion,
			packagePath: valid.PackagePath,
			want:        protocolSummaryFactIncompatible,
		},
		{
			name:        "version mismatch",
			fact:        &versionMismatch,
			packagePath: valid.PackagePath,
			want:        protocolSummaryFactIncompatible,
		},
		{
			name:        "package mismatch",
			fact:        &valid,
			packagePath: "example.com/other",
			want:        protocolSummaryFactIncompatible,
		},
		{
			name: "incomplete",
			fact: &ProtocolSummaryFact{
				FormatVersion: protocolSummaryFactVersion,
				PackagePath:   valid.PackagePath,
			},
			packagePath: valid.PackagePath,
			want:        protocolSummaryFactIncompatible,
		},
		{
			name: "invalid slot",
			fact: &ProtocolSummaryFact{
				FormatVersion: protocolSummaryFactVersion,
				PackagePath:   valid.PackagePath,
				Complete:      true,
				Effects: []ProtocolSummaryEffectFact{
					newProtocolSummaryEffect(protocolSummaryTargetParameter, -1, 0),
				},
			},
			packagePath: valid.PackagePath,
			want:        protocolSummaryFactIncompatible,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := validateProtocolSummaryFactShape(tt.fact, tt.packagePath)
			assertionID := ""
			switch tt.name {
			case "legacy version":
				assertionID = "fact-versioning/legacy-version"
			case "version mismatch":
				assertionID = "fact-versioning/version-mismatch"
			}
			if assertionID != "" {
				requireMutationGuardObservation(
					t,
					assertionID,
					mutationGuardState("protocol-summary-fact-status", "incompatible"),
					mutationGuardState("protocol-summary-fact-status", mutationGuardSummaryFactStatus(got)),
				)
			}
			if got != tt.want {
				t.Fatalf("validateProtocolSummaryFact() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestProtocolSummaryFactFailuresHaveDeterministicStatuses(t *testing.T) {
	t.Parallel()

	incompatibleVersion := ProtocolSummaryFact{
		FormatVersion:    protocolSummaryFactVersion + 1,
		PackagePath:      "example.com/probe",
		FunctionName:     "ValidateValue",
		FunctionIdentity: "example.com/probe.ValidateValue",
		Complete:         true,
	}
	tests := []struct {
		name string
		fact *ProtocolSummaryFact
		want protocolSummaryFactStatus
	}{
		{name: "missing", want: protocolSummaryFactMissing},
		{
			name: "incompatible version",
			fact: &incompatibleVersion,
			want: protocolSummaryFactIncompatible,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := validateProtocolSummaryFactShape(tt.fact, "example.com/probe")
			if tt.name == "incompatible version" {
				requireMutationGuardObservation(
					t,
					"fact-versioning/deterministic-incompatible-version",
					mutationGuardState("protocol-summary-fact-status", "incompatible"),
					mutationGuardState("protocol-summary-fact-status", mutationGuardSummaryFactStatus(got)),
				)
			}
			if got != tt.want {
				t.Fatalf("summary fact status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestProtocolSummaryFactValidationBindsAttachedFunctionAndSlots(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (*Value) Validate() error { return nil }
func Helper(value, source *Value) (*Value, error) { return value, nil }
`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "Helper")
	function, ok := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
	if !ok {
		t.Fatal("Helper type object is not a function")
	}
	valid := ProtocolSummaryFact{
		FormatVersion:    protocolSummaryFactVersion,
		PackagePath:      function.Pkg().Path(),
		FunctionName:     function.Name(),
		FunctionIdentity: protocolFunctionIdentity(function),
		Complete:         true,
		Effects: []ProtocolSummaryEffectFact{
			newProtocolSummaryEffect(protocolSummaryTargetParameter, 0, 1),
			{
				Kind:                protocolSummaryEffectReplace,
				TargetKind:          protocolSummaryTargetParameter,
				TargetSlot:          0,
				SourceKind:          protocolSummaryTargetParameter,
				SourceSlot:          1,
				ConditionResultSlot: -1,
			},
		},
	}
	if got := validateProtocolSummaryFact(&valid, function); got != protocolSummaryFactValid {
		t.Fatalf("valid attached fact status = %d, want valid", got)
	}
	tests := []struct {
		name   string
		mutate func(*ProtocolSummaryFact)
	}{
		{name: "wrong function", mutate: func(fact *ProtocolSummaryFact) { fact.FunctionName = "Other" }},
		{name: "wrong function identity", mutate: func(fact *ProtocolSummaryFact) {
			fact.FunctionIdentity = fact.PackagePath + ".Other"
		}},
		{name: "target slot", mutate: func(fact *ProtocolSummaryFact) { fact.Effects[0].TargetSlot = 2 }},
		{name: "target role", mutate: func(fact *ProtocolSummaryFact) { fact.Effects[0].TargetKind = protocolSummaryTargetReceiver }},
		{name: "condition slot range", mutate: func(fact *ProtocolSummaryFact) { fact.Effects[0].ConditionResultSlot = 2 }},
		{name: "condition slot type", mutate: func(fact *ProtocolSummaryFact) { fact.Effects[0].ConditionResultSlot = 0 }},
		{name: "source slot", mutate: func(fact *ProtocolSummaryFact) { fact.Effects[1].SourceSlot = 2 }},
		{name: "source type", mutate: func(fact *ProtocolSummaryFact) {
			fact.Effects[1].SourceKind = protocolSummaryTargetResult
			fact.Effects[1].SourceSlot = 1
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			invalid := valid
			invalid.Effects = append([]ProtocolSummaryEffectFact(nil), valid.Effects...)
			tt.mutate(&invalid)
			if got := validateProtocolSummaryFact(&invalid, function); got != protocolSummaryFactIncompatible {
				t.Fatalf("malformed attached fact status = %d, want incompatible; fact=%+v", got, invalid)
			}
		})
	}
}

func TestProtocolSummaryFactStringIncludesVersionAndOwner(t *testing.T) {
	t.Parallel()

	fact := &ProtocolSummaryFact{
		FormatVersion:    protocolSummaryFactVersion,
		PackagePath:      "example.com/probe",
		FunctionName:     "ValidateValue",
		FunctionIdentity: "example.com/probe.ValidateValue",
		Complete:         true,
		Effects:          []ProtocolSummaryEffectFact{{}},
	}
	want := "protocol-summary:v5:example.com/probe:example.com/probe.ValidateValue:1"
	if got := fact.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestProtocolSummaryFactRejectsSameNamedMethodOnDifferentReceiver(t *testing.T) {
	t.Parallel()

	const source = `package probe
type First struct{}
type Second struct{}
func (*First) Apply() {}
func (*Second) Apply() {}
`
	pass, file := buildTypedPassFromSource(t, source)
	methods := make([]*types.Func, 0, 2)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "Apply" {
			continue
		}
		object, ok := pass.TypesInfo.Defs[function.Name].(*types.Func)
		if ok {
			methods = append(methods, object)
		}
	}
	if len(methods) != 2 {
		t.Fatalf("same-named method count = %d, want 2", len(methods))
	}
	fact := ProtocolSummaryFact{
		FormatVersion:    protocolSummaryFactVersion,
		PackagePath:      methods[0].Pkg().Path(),
		FunctionName:     methods[0].Name(),
		FunctionIdentity: protocolFunctionIdentity(methods[0]),
		Complete:         true,
		Effects:          []ProtocolSummaryEffectFact{newProtocolCallSummaryEffect(protocolSummaryEffectPure)},
	}
	if got := validateProtocolSummaryFact(&fact, methods[0]); got != protocolSummaryFactValid {
		t.Fatalf("owning method fact status = %d, want valid", got)
	}
	if got := validateProtocolSummaryFact(&fact, methods[1]); got != protocolSummaryFactIncompatible {
		t.Fatalf("different receiver method fact status = %d, want incompatible", got)
	}
}

func TestProtocolSummaryFactAcceptsExplicitEffectVocabulary(t *testing.T) {
	t.Parallel()

	replacement := newProtocolTargetSummaryEffect(
		protocolSummaryEffectReplace,
		protocolSummaryTargetParameter,
		0,
	)
	replacement.SourceKind = protocolSummaryTargetParameter
	replacement.SourceSlot = 1
	fact := &ProtocolSummaryFact{
		FormatVersion:    protocolSummaryFactVersion,
		PackagePath:      "example.com/probe",
		FunctionName:     "Effects",
		FunctionIdentity: "example.com/probe.Effects",
		Complete:         true,
		Effects: []ProtocolSummaryEffectFact{
			newProtocolCallSummaryEffect(protocolSummaryEffectPure),
			newProtocolTargetSummaryEffect(protocolSummaryEffectPreserve, protocolSummaryTargetParameter, 0),
			newProtocolSummaryEffect(protocolSummaryTargetParameter, 0, 0),
			newProtocolTargetSummaryEffect(protocolSummaryEffectMutate, protocolSummaryTargetParameter, 0),
			replacement,
			newProtocolTargetSummaryEffect(protocolSummaryEffectEscape, protocolSummaryTargetParameter, 0),
			newProtocolTargetSummaryEffect(protocolSummaryEffectConsume, protocolSummaryTargetParameter, 0),
			newProtocolCallSummaryEffect(protocolSummaryEffectTerminal),
		},
	}
	if uncertainty := validateProtocolSummaryFactShape(fact, fact.PackagePath); uncertainty != 0 {
		t.Fatalf("explicit effect vocabulary validation = %d, want 0", uncertainty)
	}

	for _, effect := range []ProtocolSummaryEffectFact{
		{Kind: "unknown", TargetSlot: -1, SourceSlot: -1, ConditionResultSlot: -1},
		newProtocolTargetSummaryEffect(protocolSummaryEffectPure, protocolSummaryTargetParameter, 0),
		newProtocolTargetSummaryEffect(protocolSummaryEffectReplace, protocolSummaryTargetParameter, 0),
	} {
		invalid := *fact
		invalid.Effects = []ProtocolSummaryEffectFact{effect}
		if status := validateProtocolSummaryFactShape(&invalid, fact.PackagePath); status != protocolSummaryFactIncompatible {
			t.Errorf("invalid effect %+v validation = %d, want incompatible", effect, status)
		}
	}
}

func TestFilteredPackageExportsProtocolSummaryFact(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")
	setFlag(t, h.Analyzer, "include-packages", "example.com/not-the-fixture")

	analysistestParallelLimiter <- struct{}{}
	defer func() { <-analysistestParallelLimiter }()
	results := analysistest.Run(t, analysistest.TestData(), h.Analyzer, "protocol_summary_fact")
	found := false
	for _, result := range results {
		for object, facts := range result.Facts {
			if object == nil || object.Name() != "ValidateValue" {
				continue
			}
			function, ok := object.(*types.Func)
			if !ok {
				t.Fatalf("ValidateValue fact owner is %T, want *types.Func", object)
			}
			for _, rawFact := range facts {
				fact, factOK := rawFact.(*ProtocolSummaryFact)
				if !factOK {
					continue
				}
				if got := validateProtocolSummaryFact(fact, function); got != 0 {
					t.Fatalf("exported summary fact validation = %d: %+v", got, fact)
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("filtered package did not export ProtocolSummaryFact for ValidateValue")
	}
}

func TestProtocolSummaryFactsExportAllExplicitEffects(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")

	analysistestParallelLimiter <- struct{}{}
	defer func() { <-analysistestParallelLimiter }()
	results := analysistest.Run(t, analysistest.TestData(), h.Analyzer, "protocol_summary_effects")
	want := map[string][]string{
		"Pure":                {protocolSummaryEffectPure},
		"Preserve":            {protocolSummaryEffectPreserve},
		"ConditionalValidate": {protocolSummaryEffectValidate},
		"Mutate":              {protocolSummaryEffectMutate},
		"Replace":             {protocolSummaryEffectReplace},
		"Escape":              {protocolSummaryEffectEscape},
		"Consume":             {protocolSummaryEffectConsume},
		"Terminal":            {protocolSummaryEffectTerminal},
	}
	for _, result := range results {
		for object, facts := range result.Facts {
			wantKinds, tracked := want[object.Name()]
			if !tracked {
				continue
			}
			for _, rawFact := range facts {
				fact, ok := rawFact.(*ProtocolSummaryFact)
				if !ok {
					continue
				}
				if !fact.Complete {
					t.Errorf("%s exported an incomplete summary", object.Name())
				}
				for _, effect := range fact.Effects {
					if slices.Contains(wantKinds, effect.Kind) {
						delete(want, object.Name())
						break
					}
				}
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing exported explicit summary effects: %v", want)
	}
}

func TestProtocolSummaryFactFailsClosedForComplexInputEffects(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value *Value) Validate() error { return nil }
func Conditional(value *Value) error {
	if value == nil {
		return nil
	}
	return value.Validate()
}
func ChannelEscape(ch chan *Value, value *Value) { ch <- value }
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	for _, functionName := range []string{"Conditional", "ChannelEscape"} {
		declaration := findFuncDecl(t, file, functionName)
		function, ok := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
		if !ok {
			t.Fatalf("%s type object is not a function", functionName)
		}
		ssaFunction := ssaFuncForTypesFunc(ssaResult, function)
		fact := buildCompleteProtocolSummaryFact(
			pass,
			function,
			ssaFunction,
			map[string]bool{objectKey(function): true},
		)
		if fact.Complete || len(fact.Effects) != 0 {
			t.Errorf("%s input summary = %+v, want incomplete fact with no effects", functionName, fact)
		}
	}
}

func TestProtocolSummaryEffectsApplyAcrossPackages(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")

	diagnostics, analysisErrors, _ := collectDiagnosticsForPackages(
		t,
		h.Analyzer,
		"protocol_summary_effects_cross/...",
	)
	violations := 0
	inconclusives := 0
	for _, diagnostic := range diagnostics {
		switch diagnostic.Category {
		case CategoryMissingConstructorValidate:
			violations++
		case CategoryMissingConstructorValidateInc:
			inconclusives++
		}
	}
	requireMutationGuardObservation(
		t,
		"post-validation-summary/cross-package-effects",
		mutationGuardState("post-validation-constructor-diagnostics", "violations=5,inconclusives=1"),
		mutationGuardState(
			"post-validation-constructor-diagnostics",
			fmt.Sprintf("violations=%d,inconclusives=%d", violations, inconclusives),
		),
	)
	if len(analysisErrors) != 0 {
		t.Fatalf("cross-package summary analysistest errors: %v", analysisErrors)
	}
}

func TestFilteredCrossPackageGenericProtocolFacts(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-constructor-validates", "true")
	setFlag(t, h.Analyzer, "include-packages", "protocol_generic_cross/app")

	analysistestParallelLimiter <- struct{}{}
	defer func() { <-analysistestParallelLimiter }()
	results := analysistest.Run(
		t,
		analysistest.TestData(),
		h.Analyzer,
		"protocol_generic_cross/util",
		"protocol_generic_cross/app",
	)
	wantKinds := map[string]string{
		"ValidateValue": protocolSummaryTargetParameter,
		"NewValue":      protocolSummaryTargetResult,
	}
	found := make(map[string]bool)
	for _, result := range results {
		for object, facts := range result.Facts {
			wantKind, wanted := wantKinds[object.Name()]
			if !wanted {
				continue
			}
			for _, rawFact := range facts {
				fact, ok := rawFact.(*ProtocolSummaryFact)
				if !ok || len(fact.Effects) != 1 {
					continue
				}
				if fact.Effects[0].TargetKind != wantKind {
					t.Fatalf("%s target kind = %q, want %q", object.Name(), fact.Effects[0].TargetKind, wantKind)
				}
				if fact.Effects[0].Kind != protocolSummaryEffectValidate {
					t.Fatalf("%s effect kind = %q, want %q", object.Name(), fact.Effects[0].Kind, protocolSummaryEffectValidate)
				}
				found[object.Name()] = true
			}
		}
	}
	for name := range wantKinds {
		if !found[name] {
			t.Errorf("filtered generic package did not export conditional fact for %s", name)
		}
	}
}

func assertProtocolSummaryEffects(t *testing.T, fact ProtocolSummaryFact, want []ProtocolSummaryEffectFact) {
	t.Helper()
	if fact.FormatVersion != protocolSummaryFactVersion {
		t.Fatalf("summary version = %d, want %d", fact.FormatVersion, protocolSummaryFactVersion)
	}
	if !reflect.DeepEqual(fact.Effects, want) {
		t.Fatalf("summary effects = %+v, want %+v", fact.Effects, want)
	}
}

func mutationGuardSummaryFactStatus(status protocolSummaryFactStatus) string {
	switch status {
	case protocolSummaryFactValid:
		return "valid"
	case protocolSummaryFactMissing:
		return "missing"
	case protocolSummaryFactIncompatible:
		return "incompatible"
	default:
		return "unknown"
	}
}
