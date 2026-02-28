// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	gocfg "golang.org/x/tools/go/cfg"
)

// parseFuncBody parses a Go source snippet and returns the first function's
// body and its CFG for test purposes.
func parseFuncBody(t *testing.T, src string) (*ast.BlockStmt, *gocfg.CFG) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		cfg := buildFuncCFG(fn.Body)
		return fn.Body, cfg
	}
	t.Fatal("no function found in source")
	return nil, nil
}

func TestBuildFuncCFG_NilBody(t *testing.T) {
	g := buildFuncCFG(nil)
	if g != nil {
		t.Error("expected nil CFG for nil body")
	}
}

func TestBuildFuncCFG_SimpleFunction(t *testing.T) {
	src := `package p
func f() {
	x := 1
	_ = x
}`
	_, cfg := parseFuncBody(t, src)
	if cfg == nil {
		t.Fatal("expected non-nil CFG")
	}
	if len(cfg.Blocks) == 0 {
		t.Error("expected at least one block")
	}
}

func TestFindDefiningBlock_Found(t *testing.T) {
	src := `package p
func f() {
	x := 1
	y := 2
	_ = x + y
}`
	body, cfg := parseFuncBody(t, src)
	if cfg == nil {
		t.Fatal("expected non-nil CFG")
	}

	// Find the first statement (x := 1) in the CFG.
	firstStmt := body.List[0]
	block, idx := findDefiningBlock(cfg, firstStmt)
	if block == nil {
		t.Fatal("expected to find block for first statement")
	}
	if idx < 0 {
		t.Error("expected non-negative index")
	}
}

func TestFindDefiningBlock_NotFound(t *testing.T) {
	src := `package p
func f() {
	x := 1
	_ = x
}`
	_, cfg := parseFuncBody(t, src)

	// Create a node that doesn't exist in the CFG.
	fakeNode := &ast.Ident{Name: "fake"}
	block, idx := findDefiningBlock(cfg, fakeNode)
	if block != nil {
		t.Error("expected nil block for non-existent node")
	}
	if idx != -1 {
		t.Errorf("expected index -1, got %d", idx)
	}
}

func TestContainsValidateCall_Found(t *testing.T) {
	src := `package p
type T string
func (t T) Validate() error { return nil }
func f() {
	var x T
	x.Validate()
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Find the expression statement x.Validate()
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "f" {
			continue
		}
		// The second statement should be x.Validate()
		if len(fn.Body.List) < 2 {
			t.Fatal("expected at least 2 statements")
		}
		stmt := fn.Body.List[1]
		if containsValidateCall(stmt, "x") {
			return // found, test passes
		}
		t.Error("expected to find Validate call on x")
		return
	}
	t.Fatal("function f not found")
}

func TestContainsValidateCall_WrongVar(t *testing.T) {
	src := `package p
type T string
func (t T) Validate() error { return nil }
func f() {
	var x T
	x.Validate()
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "f" {
			continue
		}
		stmt := fn.Body.List[1]
		if containsValidateCall(stmt, "y") {
			t.Error("should not find Validate call on y when only x.Validate() exists")
		}
		return
	}
	t.Fatal("function f not found")
}
