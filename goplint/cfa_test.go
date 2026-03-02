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
	t.Parallel()

	g := buildFuncCFG(nil)
	if g != nil {
		t.Error("expected nil CFG for nil body")
	}
}

func TestBuildFuncCFG_SimpleFunction(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
		if containsValidateCall(stmt, "x", nil) {
			return // found, test passes
		}
		t.Error("expected to find Validate call on x")
		return
	}
	t.Fatal("function f not found")
}

func TestContainsValidateCall_WrongVar(t *testing.T) {
	t.Parallel()

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
		if containsValidateCall(stmt, "y", nil) {
			t.Error("should not find Validate call on y when only x.Validate() exists")
		}
		return
	}
	t.Fatal("function f not found")
}

func TestCollectImmediateClosureLits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		wantLen int
	}{
		{
			name: "iife only",
			src: `package p
func f() {
	func() {}()
}`,
			wantLen: 1,
		},
		{
			name: "goroutine excluded",
			src: `package p
func f() {
	go func() {}()
}`,
			wantLen: 0,
		},
		{
			name: "defer excluded",
			src: `package p
func f() {
	defer func() {}()
}`,
			wantLen: 0,
		},
		{
			name: "mixed immediate and wrappers",
			src: `package p
func f() {
	func() {}()
	go func() {}()
	defer func() {}()
	func() { func() {}() }()
}`,
			wantLen: 3,
		},
		{
			name: "parenthesized iife",
			src: `package p
func f() {
	(func() {})()
}`,
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, _ := parseFuncBody(t, tt.src)
			got := collectImmediateClosureLits(body)
			if len(got) != tt.wantLen {
				t.Fatalf("len(collectImmediateClosureLits) = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestCollectDeferredClosureLits_Parenthesized(t *testing.T) {
	t.Parallel()

	src := `package p
func f() {
	defer (func() {})()
}`

	body, _ := parseFuncBody(t, src)
	got := collectDeferredClosureLits(body)
	if len(got) != 1 {
		t.Fatalf("len(collectDeferredClosureLits) = %d, want 1", len(got))
	}
}

func TestCollectImmediateClosureLits_NilBody(t *testing.T) {
	t.Parallel()

	got := collectImmediateClosureLits(nil)
	if got != nil {
		t.Fatalf("collectImmediateClosureLits(nil) = %v, want nil", got)
	}
}

func TestCollectSynchronousClosureLits_ClassifiesExactClosures(t *testing.T) {
	t.Parallel()

	src := `package p
func f() {
	defer func() {}()
	go func() {}()
	func() {}()
}`
	body, _ := parseFuncBody(t, src)
	if len(body.List) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(body.List))
	}

	deferStmt, ok := body.List[0].(*ast.DeferStmt)
	if !ok {
		t.Fatalf("expected defer stmt at index 0, got %T", body.List[0])
	}
	deferLit, ok := deferStmt.Call.Fun.(*ast.FuncLit)
	if !ok {
		t.Fatal("expected defer function literal")
	}

	goStmt, ok := body.List[1].(*ast.GoStmt)
	if !ok {
		t.Fatalf("expected go stmt at index 1, got %T", body.List[1])
	}
	goLit, ok := goStmt.Call.Fun.(*ast.FuncLit)
	if !ok {
		t.Fatal("expected go function literal")
	}

	exprStmt, ok := body.List[2].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("expected expression stmt at index 2, got %T", body.List[2])
	}
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected call expr at index 2, got %T", exprStmt.X)
	}
	immediateLit, ok := call.Fun.(*ast.FuncLit)
	if !ok {
		t.Fatal("expected immediate function literal")
	}

	deferred := collectDeferredClosureLits(body)
	immediate := collectImmediateClosureLits(body)
	sync := collectSynchronousClosureLits(body)
	ubv := collectUBVClosureLits(body)

	if !deferred[deferLit] {
		t.Fatal("expected defer closure to be classified as deferred")
	}
	if deferred[goLit] || deferred[immediateLit] {
		t.Fatal("did not expect go/IIFE closures in deferred set")
	}

	if !immediate[immediateLit] {
		t.Fatal("expected IIFE closure to be classified as immediate")
	}
	if immediate[deferLit] || immediate[goLit] {
		t.Fatal("did not expect defer/go closures in immediate set")
	}

	if !sync[deferLit] || !sync[immediateLit] {
		t.Fatal("expected sync set to include deferred + immediate closures")
	}
	if sync[goLit] {
		t.Fatal("did not expect goroutine closure in sync set")
	}
	if !ubv[immediateLit] {
		t.Fatal("expected UBV set to include immediate closure")
	}
	if ubv[deferLit] || ubv[goLit] {
		t.Fatal("did not expect defer/go closures in UBV set")
	}
}

func TestCastTargetMatchesExpr_IndexParensCanonicalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		lhs    string
		target string
	}{
		{
			name:   "index expr",
			lhs:    "ports[0]",
			target: "ports[(0)]",
		},
		{
			name:   "index list expr",
			lhs:    "matrix[i,j]",
			target: "matrix[(i),(j)]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lhsExpr, err := parser.ParseExpr(tt.lhs)
			if err != nil {
				t.Fatalf("parse lhs: %v", err)
			}
			target, ok := castTargetFromExpr(nil, lhsExpr)
			if !ok {
				t.Fatalf("castTargetFromExpr returned ok=false for %q", tt.lhs)
			}

			matchExpr, err := parser.ParseExpr(tt.target)
			if err != nil {
				t.Fatalf("parse target: %v", err)
			}
			if !target.matchesExpr(nil, matchExpr) {
				t.Fatalf("expected %q to match target from %q", tt.target, tt.lhs)
			}
		})
	}
}

func TestCastTargetMatchesExpr_AddressOfReceiverCanonicalization(t *testing.T) {
	t.Parallel()

	lhsExpr, err := parser.ParseExpr("x")
	if err != nil {
		t.Fatalf("parse lhs: %v", err)
	}
	target, ok := castTargetFromExpr(nil, lhsExpr)
	if !ok {
		t.Fatal("castTargetFromExpr returned ok=false for lhs x")
	}

	receiverExpr, err := parser.ParseExpr("(&x)")
	if err != nil {
		t.Fatalf("parse receiver: %v", err)
	}
	if !target.matchesExpr(nil, receiverExpr) {
		t.Fatal("expected (&x) receiver to match target derived from x")
	}
}

func TestHasUseBeforeValidateInBlock_DeferredValidateDoesNotSuppress(t *testing.T) {
	t.Parallel()

	src := `package p
type T string
func f() {
	var x T
	defer func() { _ = x.Validate() }()
	use(x)
}
func (t T) Validate() error { return nil }
func use(_ T) {}`

	body, _ := parseFuncBody(t, src)
	if len(body.List) < 3 {
		t.Fatalf("expected at least 3 statements, got %d", len(body.List))
	}
	target := newCastTargetFromName("x")
	nodes := make([]ast.Node, 0, len(body.List))
	for _, stmt := range body.List {
		nodes = append(nodes, stmt)
	}

	if hasUseBeforeValidateInBlock(nil, nodes, 1, target, collectSynchronousClosureLits(body), nil, nil) {
		t.Fatal("expected sync closure set to treat deferred Validate as ordering-safe")
	}
	if !hasUseBeforeValidateInBlock(nil, nodes, 1, target, collectUBVClosureLits(body), nil, nil) {
		t.Fatal("expected UBV closure set to flag use before deferred Validate")
	}
}

func TestFirstUseValidateOrderInNode_AsyncValidateIgnored(t *testing.T) {
	t.Parallel()

	src := `package p
type T string
func (t T) Validate() error { return nil }
func f() {
	var x T
	go x.Validate()
	defer x.Validate()
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		candidate, ok := decl.(*ast.FuncDecl)
		if !ok || candidate.Name.Name != "f" {
			continue
		}
		fn = candidate
		break
	}
	if fn == nil {
		t.Fatal("function f not found")
	}
	if len(fn.Body.List) < 3 {
		t.Fatalf("expected at least 3 statements, got %d", len(fn.Body.List))
	}

	target := newCastTargetFromName("x")
	if got := firstUseValidateOrderInNode(nil, fn.Body.List[1], target, nil, nil, nil); got != ubvOrderNone {
		t.Fatalf("go x.Validate() order = %v, want %v", got, ubvOrderNone)
	}
	if got := firstUseValidateOrderInNode(nil, fn.Body.List[2], target, nil, nil, nil); got != ubvOrderNone {
		t.Fatalf("defer x.Validate() order = %v, want %v", got, ubvOrderNone)
	}
}
