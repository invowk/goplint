// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestCollectDelegationAliasBindingsGuardInputs(t *testing.T) {
	t.Parallel()

	if got := collectDelegationAliasBindings(nil, &ast.BlockStmt{}, "c"); got != nil {
		t.Fatalf("nil pass produced bindings: %+v", got)
	}
	if got := collectDelegationAliasBindings(&analysis.Pass{}, &ast.BlockStmt{}, "c"); got != nil {
		t.Fatalf("pass without TypesInfo produced bindings: %+v", got)
	}

	src := `package testpkg
type Name string
func (Name) Validate() error { return nil }

type Config struct{ Primary Name }

func (c *Config) Validate() error {
	return c.Primary.Validate()
}`

	pass, file := buildTypedPassFromSource(t, src)
	validateFn := findMethodDecl(t, file, "Config", "Validate")
	if got := collectDelegationAliasBindings(pass, nil, "c"); got != nil {
		t.Fatalf("nil body produced bindings: %+v", got)
	}
	if got := collectDelegationAliasBindings(pass, validateFn.Body, ""); got != nil {
		t.Fatalf("empty receiver produced bindings: %+v", got)
	}
}

func TestCollectDelegationAliasBindingsClearsAliasOnNonFieldRebind(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Name string
func (Name) Validate() error { return nil }

type Config struct{
	Primary Name
}

func (c *Config) Validate() error {
	alias := c.Primary
	alias = Name("manual")
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
	if fieldName, ok := latestDelegationAliasFieldBefore(bindings[aliasKey], call.Pos()); ok {
		t.Fatalf("resolved cleared alias to field %q, want no delegation", fieldName)
	}
}

func TestCollectDelegationAliasBindingsChainsAliasThroughValueSpec(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Name string
func (Name) Validate() error { return nil }

type Config struct{
	Primary Name
}

func (c *Config) Validate() error {
	primary := c.Primary
	var alias = primary
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
		t.Fatal("expected chained alias binding to resolve")
	}
	if fieldName != "Primary" {
		t.Fatalf("resolved alias field = %q, want %q", fieldName, "Primary")
	}
}

func TestCollectDelegationAliasBindingsRangeValueAlias(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Name string
func (Name) Validate() error { return nil }

type Config struct{
	Items []Name
}

func (c *Config) Validate() error {
	for _, item := range c.Items {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}`

	pass, file := buildTypedPassFromSource(t, src)
	validateFn := findMethodDecl(t, file, "Config", "Validate")
	bindings := collectDelegationAliasBindings(pass, validateFn.Body, "c")
	call := findAliasValidateCall(t, validateFn, "item")
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		t.Fatal("expected selector call for item.Validate")
	}
	aliasKey := targetKeyForExpr(pass, sel.X)
	if aliasKey == "" {
		t.Fatal("expected range value alias key to resolve")
	}
	fieldName, ok := latestDelegationAliasFieldBefore(bindings[aliasKey], call.Pos())
	if !ok {
		t.Fatal("expected range value binding to resolve")
	}
	if fieldName != "Items" {
		t.Fatalf("resolved range value field = %q, want %q", fieldName, "Items")
	}
}

func TestDelegatedFieldNameForArgDirectAndAlias(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Name string
func (Name) Validate() error { return nil }

type Config struct{
	Primary Name
	Secondary Name
}

func delegate(Name, Name, Name) {}

func (c *Config) Validate() error {
	alias := c.Primary
	manual := Name("manual")
	delegate(c.Secondary, alias, manual)
	return nil
}`

	pass, file := buildTypedPassFromSource(t, src)
	validateFn := findMethodDecl(t, file, "Config", "Validate")
	bindings := collectDelegationAliasBindings(pass, validateFn.Body, "c")
	call := findCallExpr(t, validateFn, "delegate")

	fieldName, ok := delegatedFieldNameForArg(pass, call.Args[0], "c", bindings, call.Pos())
	if !ok || fieldName != "Secondary" {
		t.Fatalf("direct receiver arg resolved to (%q, %v), want (Secondary, true)", fieldName, ok)
	}
	fieldName, ok = delegatedFieldNameForArg(pass, call.Args[1], "c", bindings, call.Pos())
	if !ok || fieldName != "Primary" {
		t.Fatalf("alias arg resolved to (%q, %v), want (Primary, true)", fieldName, ok)
	}
	if fieldName, ok := delegatedFieldNameForArg(pass, call.Args[2], "c", bindings, call.Pos()); ok {
		t.Fatalf("manual arg resolved to field %q, want no delegation", fieldName)
	}
	if fieldName, ok := delegatedFieldNameForArg(pass, call.Args[0], "", bindings, call.Pos()); ok {
		t.Fatalf("empty receiver resolved to field %q, want no delegation", fieldName)
	}
}

func TestCollectDelegatedHelperParamsPatterns(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Name string
func (Name) Validate() error { return nil }

func helper(direct Name, indexed []Name, ranged []Name, rangeIndexed []Name, ignored []Name) {
	_ = direct.Validate()
	_ = indexed[0].Validate()
	for _, item := range ranged {
		_ = item.Validate()
	}
	for i := range rangeIndexed {
		_ = rangeIndexed[i].Validate()
	}
	for _, item := range ignored {
		_ = item
	}
}`

	_, file := buildTypedPassFromSource(t, src)
	helperFn := findFuncDecl(t, file, "helper")
	if got := collectDelegatedHelperParams(nil, []string{"direct"}); got != nil {
		t.Fatalf("nil body produced delegated params: %+v", got)
	}
	if got := collectDelegatedHelperParams(helperFn.Body, nil); got != nil {
		t.Fatalf("nil param list produced delegated params: %+v", got)
	}

	got := collectDelegatedHelperParams(helperFn.Body, functionParamNames(helperFn.Type.Params))
	want := []int{0, 1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("delegated params = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delegated params = %+v, want %+v", got, want)
		}
	}
}

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

func findCallExpr(t *testing.T, fn *ast.FuncDecl, name string) *ast.CallExpr {
	t.Helper()

	var found *ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != name {
			return true
		}
		found = call
		return false
	})
	if found == nil {
		t.Fatalf("call to %q not found", name)
	}
	return found
}
