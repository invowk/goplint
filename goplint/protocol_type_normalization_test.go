// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestNormalizeProtocolCallUsesInstantiatedGenericSignatures(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Box[T any] struct{ Value T }

func NewBox[T any](value T) (Box[T], error) { return Box[T]{Value: value}, nil }
func NewPair[T, U any](first T, second U) (T, U, error) { return first, second, nil }

func Use() {
	_, _ = NewBox[string]("")
	_, _, _ = NewPair[string, int]("", 0)
}
`
	pass, file := buildTypedPassFromSource(t, source)
	calls := make(map[string]protocolNormalizedCall)
	ast.Inspect(findFuncDecl(t, file, "Use").Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		normalized, ok := normalizeProtocolCall(pass, call)
		if ok {
			calls[normalized.Function.Name()] = normalized
		}
		return true
	})

	normalizationState := "resolved-index-and-index-list"
	if calls["NewBox"].Signature == nil || calls["NewPair"].Signature == nil {
		normalizationState = "missing-indexed-callee"
	}
	requireMutationGuardObservation(
		t,
		"generic-normalization/indexed-callees",
		mutationGuardState("generic-indexed-call-resolution", "resolved-index-and-index-list"),
		mutationGuardState("generic-indexed-call-resolution", normalizationState),
	)

	box := calls["NewBox"]
	if box.Signature == nil || len(box.Results) != 2 || !box.TrailingError {
		t.Fatalf("NewBox normalization = %+v, want two results and trailing error", box)
	}
	if got := types.TypeString(box.Results[0].Type, nil); got != "testpkg.Box[string]" {
		t.Fatalf("NewBox result 0 = %q, want instantiated Box[string]", got)
	}

	pair := calls["NewPair"]
	if pair.Signature == nil || len(pair.Results) != 3 || !pair.TrailingError {
		t.Fatalf("NewPair normalization = %+v, want three results and trailing error", pair)
	}
	wantPair := []string{"string", "int", "error"}
	for idx, want := range wantPair {
		if pair.Results[idx].Slot != idx {
			t.Fatalf("NewPair result slot = %d, want %d", pair.Results[idx].Slot, idx)
		}
		if got := types.TypeString(pair.Results[idx].Type, nil); got != want {
			t.Fatalf("NewPair result %d = %q, want %q", idx, got, want)
		}
	}
}

func TestRawPrimitiveTypeParameterConstraintsAreConservative(t *testing.T) {
	t.Parallel()

	const source = `package probe

func Primitive[T ~string | ~int](value T) {}
func Mixed[T ~string | []byte](value T) {}
func Unsupported[T interface{ ~string; String() string }](value T) {}
func Unconstrained[T any](value T) {}
`
	pass, file := buildTypedPassFromSource(t, source)
	tests := []struct {
		functionName string
		want         bool
	}{
		{functionName: "Primitive", want: true},
		{functionName: "Mixed"},
		{functionName: "Unsupported", want: true},
		{functionName: "Unconstrained"},
	}
	for _, tt := range tests {
		fn := findFuncDecl(t, file, tt.functionName)
		parameterType := pass.TypesInfo.TypeOf(fn.Type.Params.List[0].Type)
		if got := isRawPrimitive(parameterType); got != tt.want {
			t.Fatalf("isRawPrimitive(%s parameter) = %v, want %v", tt.functionName, got, tt.want)
		}
	}
	mixed := findFuncDecl(t, file, "Mixed")
	mixedType := pass.TypesInfo.TypeOf(mixed.Type.Params.List[0].Type)
	if got := classifyRawPrimitive(mixedType); got != primitiveConstraintUncertain {
		t.Fatalf("mixed generic constraint classification = %d, want uncertain", got)
	}
}

func TestResolveProtocolValidateMethodUsesGoMethodSets(t *testing.T) {
	t.Parallel()

	const source = `package probe

type Value string
func (Value) Validate() error { return nil }

type PointerValue string
func (*PointerValue) Validate() error { return nil }

type Lowercase string
func (Lowercase) validate() error { return nil }

type Wrong string
func (Wrong) Validate() bool { return true }

type Validatable interface { Validate() error }
`
	pass, file := buildTypedPassFromSource(t, source)
	tests := []struct {
		typeName string
		pointer  bool
		want     bool
	}{
		{typeName: "Value", want: true},
		{typeName: "Value", pointer: true, want: true},
		{typeName: "PointerValue", want: true},
		{typeName: "PointerValue", pointer: true, want: true},
		{typeName: "Lowercase"},
		{typeName: "Wrong"},
		{typeName: "Validatable", want: true},
	}
	for _, tt := range tests {
		object := findTypeObject(t, pass, file, tt.typeName)
		typeToCheck := object.Type()
		if tt.pointer {
			typeToCheck = types.NewPointer(typeToCheck)
		}
		method, got := resolveProtocolValidateMethod(typeToCheck)
		if got != tt.want {
			t.Fatalf("resolveProtocolValidateMethod(%s, pointer=%v) = %v, want %v", tt.typeName, tt.pointer, got, tt.want)
		}
		if got && method.Name() != validateMethodName {
			t.Fatalf("resolved method = %q, want %q", method.Name(), validateMethodName)
		}
	}
}

func findTypeObject(t *testing.T, pass *analysis.Pass, file *ast.File, name string) *types.TypeName {
	t.Helper()
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, specification := range gen.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != name {
				continue
			}
			object, ok := pass.TypesInfo.Defs[typeSpec.Name].(*types.TypeName)
			if !ok {
				t.Fatalf("type object for %s has unexpected type", name)
			}
			return object
		}
	}
	t.Fatalf("type %s not found", name)
	return nil
}
