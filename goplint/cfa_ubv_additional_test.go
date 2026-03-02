// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestIsVarUseWrapper(t *testing.T) {
	t.Parallel()

	src := `package p
func f() {
	x := 1
	use(x)
}`
	body := parseNamedFuncBody(t, src, "f")

	var call *ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if ident, ok := c.Fun.(*ast.Ident); ok && ident.Name == "use" {
				call = c
				return false
			}
		}
		return true
	})
	if call == nil {
		t.Fatal("expected use(x) call")
	}
	if !isVarUse(call, "x") {
		t.Fatal("expected call to use(x) to be treated as variable use")
	}
}

func TestIsVarUseTargetSeen_ExpressionKinds(t *testing.T) {
	t.Parallel()

	target := newCastTargetFromName("x")
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "key value key",
			src: `package p
func f() {
	x := 1
	_ = map[int]int{x: 1}
}`,
			want: true,
		},
		{
			name: "key value value",
			src: `package p
func f() {
	x := 1
	_ = map[int]int{1: x}
}`,
			want: true,
		},
		{
			name: "index expression",
			src: `package p
func f() {
	x := 1
	m := map[int]int{}
	_ = m[x]
}`,
			want: true,
		},
		{
			name: "send statement",
			src: `package p
func f() {
	x := 1
	ch := make(chan int, 1)
	ch <- x
}`,
			want: true,
		},
		{
			name: "validate call not use",
			src: `package p
type T struct{}
func (T) Validate() error { return nil }
func f() {
	var x T
	x.Validate()
}`,
			want: false,
		},
		{
			name: "display call not use",
			src: `package p
type T struct{}
func (T) String() string { return "" }
func f() {
	var x T
	x.String()
}`,
			want: false,
		},
		{
			name: "non display method call is use",
			src: `package p
type T struct{}
func (T) Setup() {}
func f() {
	var x T
	x.Setup()
}`,
			want: true,
		},
		{
			name: "function argument is use",
			src: `package p
func use(v int) {}
func f() {
	x := 1
	use(x)
}`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := parseNamedFuncBody(t, tt.src, "f")
			got := isVarUseTargetSeen(nil, body, target, nil, nil, nil, map[*ast.FuncLit]bool{})
			if got != tt.want {
				t.Fatalf("isVarUseTargetSeen() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsVarUseTargetSeen_SyncClosureControls(t *testing.T) {
	t.Parallel()

	target := newCastTargetFromName("x")
	src := `package p
func use(v int) {}
func f() {
	x := 1
	func() { use(x) }()
}`
	body := parseNamedFuncBody(t, src, "f")
	lit := firstFuncLitInNode(t, body)

	if got := isVarUseTargetSeen(nil, body, target, nil, nil, nil, map[*ast.FuncLit]bool{}); got {
		t.Fatal("closure use should be ignored when syncLits is nil")
	}

	syncLits := map[*ast.FuncLit]bool{lit: true}
	if got := isVarUseTargetSeen(nil, body, target, syncLits, nil, nil, map[*ast.FuncLit]bool{}); !got {
		t.Fatal("closure use should be detected when syncLits marks the literal executable")
	}
}

func TestIsVarUseTargetSeen_SyncCalls(t *testing.T) {
	t.Parallel()

	target := newCastTargetFromName("x")

	t.Run("use before validate", func(t *testing.T) {
		t.Parallel()

		src := `package p
type T struct{}
func (T) Validate() error { return nil }
func use(T) {}
func f() {
	var x T
	g := func() { use(x) }
	g()
}`
		body := parseNamedFuncBody(t, src, "f")
		call, lit := closureVarAndCall(t, body, "g")
		syncCalls := closureVarCallSet{call: lit}

		if got := isVarUseTargetSeen(nil, body, target, nil, syncCalls, nil, map[*ast.FuncLit]bool{}); !got {
			t.Fatal("expected use-before-validate in sync closure call to be detected")
		}
	})

	t.Run("validate before use", func(t *testing.T) {
		t.Parallel()

		src := `package p
type T struct{}
func (T) Validate() error { return nil }
func use(T) {}
func f() {
	var x T
	g := func() {
		x.Validate()
		use(x)
	}
	g()
}`
		body := parseNamedFuncBody(t, src, "f")
		call, lit := closureVarAndCall(t, body, "g")
		syncCalls := closureVarCallSet{call: lit}

		if got := isVarUseTargetSeen(nil, body, target, nil, syncCalls, nil, map[*ast.FuncLit]bool{}); got {
			t.Fatal("validate-before-use in sync closure should not report use-before-validate")
		}
	})
}

func firstFuncLitInNode(t *testing.T, node ast.Node) *ast.FuncLit {
	t.Helper()

	var lit *ast.FuncLit
	ast.Inspect(node, func(n ast.Node) bool {
		found, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		lit = found
		return false
	})
	if lit == nil {
		t.Fatal("function literal not found")
	}
	return lit
}

func closureVarAndCall(t *testing.T, body *ast.BlockStmt, closureVar string) (*ast.CallExpr, *ast.FuncLit) {
	t.Helper()

	var lit *ast.FuncLit
	var call *ast.CallExpr

	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) == 0 || len(assign.Rhs) == 0 {
			return true
		}
		ident, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || ident.Name != closureVar {
			return true
		}
		matched, ok := assign.Rhs[0].(*ast.FuncLit)
		if ok {
			lit = matched
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := ce.Fun.(*ast.Ident)
		if !ok || ident.Name != closureVar {
			return true
		}
		call = ce
		return false
	})

	if lit == nil || call == nil {
		t.Fatalf("failed to locate closure binding/call for variable %q", closureVar)
	}
	return call, lit
}

func parseNamedFuncBody(t *testing.T, src, funcName string) *ast.BlockStmt {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != funcName || fn.Body == nil {
			continue
		}
		return fn.Body
	}
	t.Fatalf("function %q not found", funcName)
	return nil
}

func TestFirstUseValidateOrderInNode_MethodValueDeferredIgnored(t *testing.T) {
	t.Parallel()

	src := `package p
type T struct{}
func (T) Validate() error { return nil }
func use(T) {}
func f() {
	var x T
	validateFn := x.Validate
	defer validateFn()
	use(x)
}`
	body := parseNamedFuncBody(t, src, "f")

	assign := body.List[1].(*ast.AssignStmt)
	deferStmt := body.List[2].(*ast.DeferStmt)
	useStmt := body.List[3]

	call := deferStmt.Call

	methodCalls := methodValueValidateCallSet{
		call: assign.Rhs[0].(*ast.SelectorExpr).X,
	}
	target := newCastTargetFromName("x")

	if got := firstUseValidateOrderInNode(nil, deferStmt, target, nil, nil, methodCalls); got != ubvOrderNone {
		t.Fatalf("deferred method-value validate should not count, got %v", got)
	}
	if got := firstUseValidateOrderInNode(nil, useStmt, target, nil, nil, methodCalls); got != ubvOrderUseBeforeValidate {
		t.Fatalf("use statement should be use-before-validate, got %v", got)
	}
}

func TestFirstUseValidateOrderInNode_AssignmentAliasIsUse(t *testing.T) {
	t.Parallel()

	src := `package p
type T struct{}
func (T) Validate() error { return nil }
func f() {
	var x T
	y := x
	_ = y
}`
	body := parseNamedFuncBody(t, src, "f")
	assign := body.List[1]
	target := newCastTargetFromName("x")

	if got := firstUseValidateOrderInNode(nil, assign, target, nil, nil, nil); got != ubvOrderUseBeforeValidate {
		t.Fatalf("alias assignment should count as use-before-validate, got %v", got)
	}
}
