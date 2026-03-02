// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestFieldTypeForMeta(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Config struct {
	Name  string
	Count int
}

type Scalar int

var ConfigVar int
`

	pass, _ := buildTypedPassFromSource(t, src)

	if got := fieldTypeForMeta(pass, "Config", "Name"); got == nil || got.String() != "string" {
		t.Fatalf("fieldTypeForMeta(Config, Name) = %v, want string", got)
	}
	if got := fieldTypeForMeta(pass, "Config", "Count"); got == nil || got.String() != "int" {
		t.Fatalf("fieldTypeForMeta(Config, Count) = %v, want int", got)
	}
	if got := fieldTypeForMeta(pass, "MissingStruct", "Name"); got != nil {
		t.Fatalf("fieldTypeForMeta(missing struct) = %v, want nil", got)
	}
	if got := fieldTypeForMeta(pass, "ConfigVar", "Name"); got != nil {
		t.Fatalf("fieldTypeForMeta(non-named object) = %v, want nil", got)
	}
	if got := fieldTypeForMeta(pass, "Scalar", "Name"); got != nil {
		t.Fatalf("fieldTypeForMeta(named non-struct) = %v, want nil", got)
	}
	if got := fieldTypeForMeta(pass, "Config", "Unknown"); got != nil {
		t.Fatalf("fieldTypeForMeta(missing field) = %v, want nil", got)
	}
}

func TestFieldTypeForMeta_NilPassPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("fieldTypeForMeta(nil, ...) should panic when pass is nil")
		}
	}()
	_ = fieldTypeForMeta(nil, "Config", "Name")
}

func TestFieldTypeForMeta_ReturnsNamedFieldType(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Name string
type Config struct {
	Display Name
}
`
	pass, _ := buildTypedPassFromSource(t, src)
	got := fieldTypeForMeta(pass, "Config", "Display")
	named, ok := types.Unalias(got).(*types.Named)
	if !ok || named.Obj().Name() != "Name" {
		t.Fatalf("fieldTypeForMeta(Config, Display) = %v, want named Name", got)
	}
}

func TestConstructorReturnsError(t *testing.T) {
	t.Parallel()

	src := `package testpkg
func Good() (int, error) { return 0, nil }
func Bad() int { return 0 }
`
	pass, file := buildTypedPassFromSource(t, src)
	goodFn := findFuncDecl(t, file, "Good")
	badFn := findFuncDecl(t, file, "Bad")

	if !constructorReturnsError(pass, goodFn.Type.Results) {
		t.Fatal("constructorReturnsError(Good results) = false, want true")
	}
	if constructorReturnsError(pass, badFn.Type.Results) {
		t.Fatal("constructorReturnsError(Bad results) = true, want false")
	}
	if constructorReturnsError(pass, nil) {
		t.Fatal("constructorReturnsError(nil results) = true, want false")
	}

	unknownResults := &ast.FieldList{
		List: []*ast.Field{{Type: ast.NewIdent("UnknownType")}},
	}
	passNoType := &analysis.Pass{
		TypesInfo: &types.Info{
			Types: make(map[ast.Expr]types.TypeAndValue),
		},
	}
	if constructorReturnsError(passNoType, unknownResults) {
		t.Fatal("constructorReturnsError(pass without type info) = true, want false")
	}
}
