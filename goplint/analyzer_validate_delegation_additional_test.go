// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestCollectDelegationAliasBindingsRebind(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Name string
func (Name) Validate() error { return nil }

type Config struct{
	Primary Name
	Secondary Name
}

func (c *Config) Validate() error {
	alias := c.Primary
	alias = c.Secondary
	return alias.Validate()
}`

	pass, file := buildTypedPassFromSource(t, src)
	validateFn := findMethodDecl(t, file, "Config", "Validate")
	bindings := collectDelegationAliasBindings(pass, validateFn.Body, "c")
	call := findAliasValidateCall(t, validateFn, "alias")
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		t.Fatal("expected selector call for alias.Validate")
	}
	aliasKey := targetKeyForExpr(pass, sel.X)
	if aliasKey == "" {
		t.Fatal("expected alias key to resolve")
	}
	fieldName, ok := latestDelegationAliasFieldBefore(bindings[aliasKey], call.Pos())
	if !ok {
		t.Fatal("expected alias binding to resolve at Validate call")
	}
	if fieldName != "Secondary" {
		t.Fatalf("resolved alias field = %q, want %q", fieldName, "Secondary")
	}
}

func TestFindDelegatedFieldsSkipsConditionalHelperCall(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Name string
func (Name) Validate() error { return nil }
type Mode string
func (Mode) Validate() error { return nil }

type Config struct {
	FieldName Name
	FieldMode Mode
}

func (c *Config) Validate() error {
	if c.FieldName != "" {
		return c.validateMaybe()
	}
	return nil
}

func (c *Config) validateMaybe() error {
	if err := c.FieldName.Validate(); err != nil { return err }
	if err := c.FieldMode.Validate(); err != nil { return err }
	return nil
}`

	pass, _ := buildTypedPassFromSource(t, src)
	called := findDelegatedFields(pass, "Config")
	if called["FieldName"] || called["FieldMode"] {
		t.Fatalf("expected conditional helper delegation to be ignored, got %+v", called)
	}
}

func TestCollectDelegationAliasBindingsParallelAssignment(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Name string
func (Name) Validate() error { return nil }

type Config struct{
	Primary Name
	Secondary Name
}

func (c *Config) Validate() error {
	aliasA := c.Primary
	aliasB := c.Secondary
	aliasA, aliasB = aliasB, aliasA
	if err := aliasA.Validate(); err != nil { return err }
	if err := aliasB.Validate(); err != nil { return err }
	return nil
}`

	pass, file := buildTypedPassFromSource(t, src)
	validateFn := findMethodDecl(t, file, "Config", "Validate")
	bindings := collectDelegationAliasBindings(pass, validateFn.Body, "c")
	callA := findAliasValidateCall(t, validateFn, "aliasA")
	callB := findAliasValidateCall(t, validateFn, "aliasB")
	selA, ok := callA.Fun.(*ast.SelectorExpr)
	if !ok {
		t.Fatal("expected selector call for aliasA.Validate")
	}
	selB, ok := callB.Fun.(*ast.SelectorExpr)
	if !ok {
		t.Fatal("expected selector call for aliasB.Validate")
	}
	keyA := targetKeyForExpr(pass, selA.X)
	keyB := targetKeyForExpr(pass, selB.X)
	if keyA == "" || keyB == "" {
		t.Fatal("expected alias keys to resolve")
	}
	fieldA, ok := latestDelegationAliasFieldBefore(bindings[keyA], callA.Pos())
	if !ok {
		t.Fatal("expected aliasA binding to resolve")
	}
	fieldB, ok := latestDelegationAliasFieldBefore(bindings[keyB], callB.Pos())
	if !ok {
		t.Fatal("expected aliasB binding to resolve")
	}
	if fieldA != "Secondary" {
		t.Fatalf("aliasA resolved field = %q, want %q", fieldA, "Secondary")
	}
	if fieldB != "Primary" {
		t.Fatalf("aliasB resolved field = %q, want %q", fieldB, "Primary")
	}
}

func findAliasValidateCall(t *testing.T, fn *ast.FuncDecl, alias string) *ast.CallExpr {
	t.Helper()

	var found *ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Validate" {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != alias {
			return true
		}
		found = call
		return false
	})
	if found == nil {
		t.Fatalf("alias Validate call for %q not found", alias)
	}
	return found
}
