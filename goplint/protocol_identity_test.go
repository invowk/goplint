// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestProtocolProcedureLiteralKeyIgnoresPrecedingLayoutDrift(t *testing.T) {
	t.Parallel()

	base := protocolLiteralKeysForSource(t, `package probe
func Probe() { callback := func() { _ = 1 }; callback() }
`)
	shifted := protocolLiteralKeysForSource(t, `package probe

const unrelatedDeclarationWithDifferentLength = "layout drift"

func Probe() {
	callback := func() {
		_ = 1
	}
	callback()
}
`)
	if !slices.Equal(base, shifted) {
		t.Fatalf("literal procedure keys changed under layout drift:\nbase=%q\nshifted=%q", base, shifted)
	}
}

func TestProtocolProcedureFallbackLiteralKeyIgnoresPrecedingLayoutDrift(t *testing.T) {
	t.Parallel()

	base := protocolFallbackLiteralKeysForSource(t, `package probe
func Probe() { callback := func() { _ = 1 }; callback() }
`)
	shifted := protocolFallbackLiteralKeysForSource(t, `package probe

const unrelatedDeclarationWithDifferentLength = "layout drift"

func Probe() {
	callback := func() {
		_ = 1
	}
	callback()
}
`)
	if !slices.Equal(base, shifted) {
		t.Fatalf("fallback literal procedure keys changed under layout drift:\nbase=%q\nshifted=%q", base, shifted)
	}
}

func protocolFallbackLiteralKeysForSource(t testing.TB, source string) []string {
	t.Helper()
	pass, _ := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	var keys []string
	for _, function := range protocolPackageFunctions(ssaResult) {
		if function.Object() == nil && function.Syntax() != nil {
			keys = append(keys, protocolProcedureKey(function))
		}
	}
	slices.Sort(keys)
	return keys
}

func protocolLiteralKeysForSource(t testing.TB, source string) []string {
	t.Helper()
	pass, _ := buildTypedPassFromSource(t, source)
	index := buildProtocolProcedureIndex(pass, buildSSAForPass(pass))
	var keys []string
	for _, procedure := range index.procedures() {
		if procedure.Literal != nil {
			keys = append(keys, procedure.Key)
		}
	}
	slices.Sort(keys)
	return keys
}

func TestProtocolIdentityInternerUsesSSAIdentity(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Box struct{ value int }

func (b *Box) Merge(other *Box, choose bool) (*Box, error) {
	left := new(Box)
	right := new(Box)
	copyOfOther := any(other)
	_ = copyOfOther
	var selected *Box
	if choose {
		selected = left
	} else {
		selected = right
	}
	return selected, nil
}

func CallMerge(receiver, other *Box) (*Box, error) {
	value, err := receiver.Merge(other, true)
	return value, err
}
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	if ssaRes == nil || ssaRes.Pkg == nil {
		t.Fatal("buildSSAForPass() returned no package")
	}

	mergeDecl := findFuncDecl(t, file, "Merge")
	mergeObject, ok := pass.TypesInfo.Defs[mergeDecl.Name].(*types.Func)
	if !ok {
		t.Fatal("type object for Merge is not a function")
	}
	merge := ssaFuncForTypesFunc(ssaRes, mergeObject)
	caller := findMemberFunc(ssaRes.Pkg, "CallMerge")
	if merge == nil || caller == nil {
		t.Fatalf("SSA functions missing: Merge=%v CallMerge=%v", merge, caller)
	}
	interner := newProtocolIdentityInterner()

	receiverID := interner.internValue(merge.Params[0])
	otherID := interner.internValue(merge.Params[1])
	if receiverID == otherID {
		t.Fatal("same-typed receiver and parameter share an identity")
	}
	assertProtocolIdentityKind(t, interner, receiverID, protocolIdentityReceiver)
	assertProtocolIdentityKind(t, interner, otherID, protocolIdentityParameter)
	if got := interner.internValue(merge.Params[1]); got != otherID {
		t.Fatalf("re-interned parameter identity = %d, want %d", got, otherID)
	}

	allocations := collectProtocolIdentityKinds(t, interner, merge, protocolIdentityAllocation)
	if len(allocations) < 2 {
		t.Fatalf("allocation identities = %v, want at least two", allocations)
	}
	if allocations[0] == allocations[1] {
		t.Fatal("same-typed allocations share an identity")
	}
	phis := collectProtocolIdentityKinds(t, interner, merge, protocolIdentityPhi)
	if len(phis) == 0 {
		t.Fatal("expected a phi identity for selected")
	}
	results := collectProtocolIdentityKinds(t, interner, caller, protocolIdentityResult)
	if len(results) < 2 {
		t.Fatalf("result identities = %v, want tuple extracts", results)
	}
	if results[0] == results[1] {
		t.Fatal("distinct return slots share an identity")
	}

	resultObject := interner.internResult(merge, 0)
	if got := interner.internResult(merge, 0); got != resultObject {
		t.Fatalf("re-interned result object = %d, want %d", got, resultObject)
	}
	assertProtocolIdentityKind(t, interner, resultObject, protocolIdentityResult)
}

func TestProtocolIdentityInternerRecordsCopySources(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Source interface{ marker() }
type Value struct{}
func (Value) marker() {}

func Box(value Value) Source {
	return value
}
`
	pass, _ := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	if ssaRes == nil || ssaRes.Pkg == nil {
		t.Fatal("buildSSAForPass() returned no package")
	}
	fn := findMemberFunc(ssaRes.Pkg, "Box")
	if fn == nil {
		t.Fatal("SSA function Box is missing")
	}
	interner := newProtocolIdentityInterner()
	foundCopy := false
	for _, block := range fn.Blocks {
		for _, instruction := range block.Instrs {
			value, ok := instruction.(ssa.Value)
			if !ok {
				continue
			}
			identity := interner.internValue(value)
			descriptor, ok := interner.descriptor(identity)
			if !ok || descriptor.Kind != protocolIdentityCopy {
				continue
			}
			if sourceID, ok := interner.copySource(identity); !ok || sourceID == 0 || sourceID == identity {
				t.Fatalf("copy identity %d has invalid source %d (present=%t)", identity, sourceID, ok)
			}
			foundCopy = true
		}
	}
	if !foundCopy {
		t.Fatal("expected an SSA copy-like interface conversion")
	}
}

func TestProtocolIdentityInternerUsesQualifiedStaticAddresses(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Box struct {
	Value int
	Other int
}
type Alias Box

var shared Box

func Local(box *Box) (*int, *int, *int) {
	first := &box.Value
	second := &box.Value
	other := &box.Other
	return first, second, other
}

func OtherProcedure(box *Box) *int { return &box.Value }
func GlobalField() *int { return &shared.Value }
func GlobalFieldAgain() *int { return &shared.Value }
func CopiedBase(box *Box) *int {
	alias := (*Alias)(box)
	return &alias.Value
}
`
	pass, _ := buildTypedPassFromSource(t, source)
	ssaResult := buildSSAForPass(pass)
	if ssaResult == nil || ssaResult.Pkg == nil {
		t.Fatal("buildSSAForPass() returned no package")
	}
	interner := newProtocolIdentityInterner()

	local := collectProtocolFieldAddresses(t, ssaResult.Pkg, "Local")
	if len(local[0]) != 2 || len(local[1]) != 1 {
		t.Fatalf("Local field-address counts = value:%d other:%d, want 2 and 1", len(local[0]), len(local[1]))
	}
	firstValue := interner.internValue(local[0][0])
	secondValue := interner.internValue(local[0][1])
	otherValue := interner.internValue(local[1][0])
	if firstValue != secondValue {
		t.Fatalf("repeated static field address identities = %d and %d, want reuse", firstValue, secondValue)
	}
	if firstValue == otherValue {
		t.Fatal("distinct static fields share an identity")
	}
	assertProtocolIdentityKind(t, interner, firstValue, protocolIdentityFieldAddr)

	otherProcedure := collectProtocolFieldAddresses(t, ssaResult.Pkg, "OtherProcedure")
	otherProcedureValue := interner.internValue(otherProcedure[0][0])
	if otherProcedureValue == firstValue {
		t.Fatal("same-named local fields in different procedures share an identity")
	}
	localDescriptor, _ := interner.descriptor(firstValue)
	otherDescriptor, _ := interner.descriptor(otherProcedureValue)
	if localDescriptor.Procedure == otherDescriptor.Procedure ||
		!strings.Contains(localDescriptor.Procedure, "Local") ||
		!strings.Contains(otherDescriptor.Procedure, "OtherProcedure") {
		t.Fatalf("field procedures = %q and %q, want distinct qualified procedures", localDescriptor.Procedure, otherDescriptor.Procedure)
	}

	global := collectProtocolFieldAddresses(t, ssaResult.Pkg, "GlobalField")
	globalAgain := collectProtocolFieldAddresses(t, ssaResult.Pkg, "GlobalFieldAgain")
	globalValue := interner.internValue(global[0][0])
	if got := interner.internValue(globalAgain[0][0]); got != globalValue {
		t.Fatalf("global static field identities = %d and %d, want package-level reuse", globalValue, got)
	}
	globalBase := interner.internValue(global[0][0].X)
	assertProtocolIdentityKind(t, interner, globalBase, protocolIdentityGlobalAddr)
	globalDescriptor, _ := interner.descriptor(globalBase)
	if globalDescriptor.Procedure != "testpkg:<global>" || globalDescriptor.ObjectKey == "" {
		t.Fatalf("global descriptor = %+v, want package-qualified stable key", globalDescriptor)
	}

	copied := collectProtocolFieldAddresses(t, ssaResult.Pkg, "CopiedBase")
	copiedValue := interner.internValue(copied[0][0])
	sourceValue, ok := interner.copySource(copiedValue)
	if !ok || sourceValue == 0 || sourceValue == copiedValue {
		t.Fatalf("copied field address %d source = %d (present=%t), want distinct source field", copiedValue, sourceValue, ok)
	}
	assertProtocolIdentityKind(t, interner, sourceValue, protocolIdentityFieldAddr)
}

func assertProtocolIdentityKind(
	t *testing.T,
	interner *protocolIdentityInterner,
	identity protocolIdentity,
	want protocolIdentityKind,
) {
	t.Helper()
	descriptor, ok := interner.descriptor(identity)
	if !ok {
		t.Fatalf("identity %d has no descriptor", identity)
	}
	if descriptor.Kind != want {
		t.Fatalf("identity %d kind = %q, want %q", identity, descriptor.Kind, want)
	}
}

func collectProtocolIdentityKinds(
	t *testing.T,
	interner *protocolIdentityInterner,
	fn *ssa.Function,
	want protocolIdentityKind,
) []protocolIdentity {
	t.Helper()
	identities := make([]protocolIdentity, 0)
	for _, block := range fn.Blocks {
		for _, instruction := range block.Instrs {
			value, ok := instruction.(ssa.Value)
			if !ok {
				continue
			}
			identity := interner.internValue(value)
			descriptor, ok := interner.descriptor(identity)
			if ok && descriptor.Kind == want {
				identities = append(identities, identity)
			}
		}
	}
	return identities
}

func collectProtocolFieldAddresses(
	t *testing.T,
	pkg *ssa.Package,
	functionName string,
) map[int][]*ssa.FieldAddr {
	t.Helper()
	fn := findMemberFunc(pkg, functionName)
	if fn == nil {
		t.Fatalf("SSA function %s is missing", functionName)
	}
	addresses := make(map[int][]*ssa.FieldAddr)
	for _, block := range fn.Blocks {
		for _, instruction := range block.Instrs {
			address, ok := instruction.(*ssa.FieldAddr)
			if ok {
				addresses[address.Field] = append(addresses[address.Field], address)
			}
		}
	}
	return addresses
}
