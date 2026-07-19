// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"
	"testing"
)

func TestObjectKeyDeterministic(t *testing.T) {
	t.Parallel()

	pkg := types.NewPackage("example.com/project/pkg", "pkg")
	v1 := types.NewVar(42, pkg, "value", types.Typ[types.String])
	v2 := types.NewVar(84, pkg, "value", types.Typ[types.String])

	key1 := objectKey(v1)
	key2 := objectKey(v2)
	if key1 != key2 {
		t.Fatalf("raw token-position drift changed semantic object key: %q != %q", key1, key2)
	}

	v3 := types.NewVar(42, pkg, "other", types.Typ[types.String])
	if objectKey(v1) == objectKey(v3) {
		t.Fatal("different semantic object names produced the same key")
	}
}

func TestObjectKeyAtIgnoresUnrelatedSourceLayoutDrift(t *testing.T) {
	t.Parallel()

	const base = `package probe
type Value string
func Probe(raw string) { value := Value(raw); _ = value }
`
	const shifted = `package probe
const unrelatedDeclarationWithDifferentLength = "layout drift"
type Value string
func Probe(raw string) { value := Value(raw); _ = value }
`

	baseKey := localObjectKeyFromSource(t, base)
	shiftedKey := localObjectKeyFromSource(t, shifted)
	if baseKey != shiftedKey {
		t.Fatalf("local object key changed under unrelated layout drift:\nbase=%q\nshifted=%q", baseKey, shiftedKey)
	}
}

func TestObjectKeyAtSeparatesShadowedLocalDeclarations(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func Probe(raw string) {
	value := Value(raw)
	_ = value
	{
		value := Value(raw)
		_ = value
	}
}
`
	pass, file := semanticProbePass(t, []semanticProbeFile{{name: "probe.go", source: source}})
	keys := make([]string, 0, 2)
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || ident.Name != "value" {
			return true
		}
		object := pass.TypesInfo.Defs[ident]
		if object != nil {
			keys = append(keys, objectKeyAt(pass, object))
		}
		return true
	})
	if len(keys) != 2 {
		t.Fatalf("shadowed declaration key count = %d, want 2", len(keys))
	}
	if keys[0] == keys[1] {
		t.Fatalf("shadowed local declarations collided: %q", keys[0])
	}
}

func TestDynamicIndexTargetKeyIgnoresUnrelatedSourceLayoutDrift(t *testing.T) {
	t.Parallel()

	const base = `package probe
type Value string
func Probe(values []Value, index int) Value { return values[index] }
`
	const shifted = `package probe
const unrelatedDeclarationWithDifferentLength = "layout drift"
type Value string
func Probe(values []Value, index int) Value { return values[index] }
`
	baseKey := dynamicIndexTargetKeyFromSource(t, base)
	shiftedKey := dynamicIndexTargetKeyFromSource(t, shifted)
	if baseKey != shiftedKey {
		t.Fatalf("dynamic index target key changed under unrelated layout drift:\nbase=%q\nshifted=%q", baseKey, shiftedKey)
	}
}

func dynamicIndexTargetKeyFromSource(t *testing.T, source string) string {
	t.Helper()

	pass, file := semanticProbePass(t, []semanticProbeFile{{name: "probe.go", source: source}})
	var indexed *ast.IndexExpr
	ast.Inspect(file, func(node ast.Node) bool {
		if candidate, ok := node.(*ast.IndexExpr); ok {
			indexed = candidate
			return false
		}
		return true
	})
	if indexed == nil {
		t.Fatal("dynamic index expression not found")
	}
	return targetKeyForExpr(pass, indexed)
}

func localObjectKeyFromSource(t *testing.T, source string) string {
	t.Helper()

	pass, file := semanticProbePass(t, []semanticProbeFile{{name: "probe.go", source: source}})
	var declaration *ast.Ident
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || ident.Name != "value" || pass.TypesInfo.Defs[ident] == nil {
			return true
		}
		declaration = ident
		return false
	})
	if declaration == nil {
		t.Fatal("local value declaration not found")
	}
	return objectKeyAt(pass, pass.TypesInfo.Defs[declaration])
}

func TestExprStringKey(t *testing.T) {
	t.Parallel()

	if got := exprStringKey(nil); got != "" {
		t.Fatalf("exprStringKey(nil) = %q, want empty", got)
	}

	expr := &ast.SelectorExpr{X: ast.NewIdent("cfg"), Sel: ast.NewIdent("Name")}
	if got := exprStringKey(expr); got != "cfg.Name" {
		t.Fatalf("exprStringKey(selector) = %q, want %q", got, "cfg.Name")
	}
}
