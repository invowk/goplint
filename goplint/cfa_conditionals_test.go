// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestContainsValidateOnReceiverSeenIfMatrix(t *testing.T) {
	t.Parallel()

	matchX := func(_ *analysis.Pass, expr ast.Expr) bool {
		ident, ok := stripParensAndStar(expr).(*ast.Ident)
		return ok && ident.Name == "x"
	}

	tests := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "if init validate",
			src: `package p
func f(x T) {
	if err := x.Validate(); err != nil {
		return
	}
}`,
			want: true,
		},
		{
			name: "if cond validate",
			src: `package p
func f(x T) {
	if x.Validate() == nil {
		return
	}
}`,
			want: true,
		},
		{
			name: "validate in one branch only",
			src: `package p
func f(x T, ok bool) {
	if ok {
		_ = x.Validate()
	}
}`,
			want: false,
		},
		{
			name: "validate in both branches",
			src: `package p
func f(x T, ok bool) {
	if ok {
		_ = x.Validate()
	} else {
		_ = x.Validate()
	}
}`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, _ := parseFuncBody(t, tt.src)
			got := containsValidateOnReceiver(nil, body, matchX, nil, nil, nil)
			if got != tt.want {
				t.Fatalf("containsValidateOnReceiver() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsConditionallyEvaluatedContextMatrix(t *testing.T) {
	t.Parallel()

	src := `package p
func initCall() bool { return true }
func condCall() bool { return true }
func rhsCall() bool { return true }
func forCondCall() bool { return true }
func switchTagCall() int { return 1 }
func switchBodyCall() {}
func typeSwitchExpr() any { return 1 }
func rangeBodyCall() {}
func goCall() {}

func f(xs []int) {
	if initCall(); condCall() && rhsCall() {
		switchBodyCall()
	}
	for forCondCall() {
		break
	}
	switch switchTagCall() {
	case 1:
		switchBodyCall()
	}
	switch typeSwitchExpr().(type) {
	case int:
		switchBodyCall()
	}
	for range xs {
		rangeBodyCall()
	}
	go goCall()
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	parentMap := buildParentMap(file)

	calls := make(map[string][]*ast.CallExpr)
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok {
			calls[ident.Name] = append(calls[ident.Name], call)
		}
		return true
	})

	checks := []struct {
		name     string
		callName string
		index    int
		want     bool
	}{
		{name: "if init", callName: "initCall", index: 0, want: false},
		{name: "if cond lhs", callName: "condCall", index: 0, want: false},
		{name: "if cond rhs short-circuit", callName: "rhsCall", index: 0, want: true},
		{name: "if body", callName: "switchBodyCall", index: 0, want: true},
		{name: "for cond", callName: "forCondCall", index: 0, want: true},
		{name: "switch tag", callName: "switchTagCall", index: 0, want: false},
		{name: "switch case body", callName: "switchBodyCall", index: 1, want: true},
		{name: "type switch assign", callName: "typeSwitchExpr", index: 0, want: false},
		{name: "type switch body", callName: "switchBodyCall", index: 2, want: true},
		{name: "range body", callName: "rangeBodyCall", index: 0, want: true},
		{name: "go call", callName: "goCall", index: 0, want: true},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			callList := calls[tc.callName]
			if len(callList) <= tc.index {
				t.Fatalf("missing call %s at index %d", tc.callName, tc.index)
			}
			got := isConditionallyEvaluated(callList[tc.index], parentMap)
			if got != tc.want {
				t.Fatalf("isConditionallyEvaluated(%s[%d]) = %v, want %v", tc.callName, tc.index, got, tc.want)
			}
		})
	}
}

func TestIsKnownNoReturnFuncTable(t *testing.T) {
	t.Parallel()

	osPkg := types.NewPackage("os", "os")
	runtimePkg := types.NewPackage("runtime", "runtime")
	logPkg := types.NewPackage("log", "log")
	testingPkg := types.NewPackage("testing", "testing")
	customPkg := types.NewPackage("example.com/x", "x")

	tests := []struct {
		name string
		pkg  *types.Package
		fn   string
		want bool
	}{
		{name: "panic", pkg: nil, fn: "panic", want: true},
		{name: "os exit", pkg: osPkg, fn: "Exit", want: true},
		{name: "runtime goexit", pkg: runtimePkg, fn: "Goexit", want: true},
		{name: "log fatal", pkg: logPkg, fn: "Fatal", want: true},
		{name: "log fatalf", pkg: logPkg, fn: "Fatalf", want: true},
		{name: "testing fatal", pkg: testingPkg, fn: "Fatal", want: true},
		{name: "testing failnow", pkg: testingPkg, fn: "FailNow", want: true},
		{name: "custom", pkg: customPkg, fn: "Fatal", want: false},
		{name: "os other", pkg: osPkg, fn: "Getenv", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isKnownNoReturnFunc(tt.pkg, tt.fn); got != tt.want {
				t.Fatalf("isKnownNoReturnFunc(%v, %q) = %v, want %v", tt.pkg, tt.fn, got, tt.want)
			}
		})
	}
}

func TestCallMayReturnWithNoReturnAliases(t *testing.T) {
	t.Parallel()

	src := `package testpkg
import "os"

func mayReturn(int) {}

func use(raw int) {
	exit := os.Exit
	exit(raw)
	exit = mayReturn
	exit(raw)
}`

	pass, file := buildTypedPassFromSource(t, src)
	useFn := findFuncDecl(t, file, "use")
	if useFn.Body == nil {
		t.Fatal("expected use body")
	}
	aliases := collectNoReturnFuncAliasEvents(pass, useFn.Body)
	calls := findDirectIdentCallsInFunc(t, useFn, "exit")
	if len(calls) != 2 {
		t.Fatalf("expected 2 exit(...) calls, got %d", len(calls))
	}
	if got := callMayReturn(pass, calls[0], aliases); got {
		t.Fatal("expected first aliased os.Exit call to be no-return")
	}
	if got := callMayReturn(pass, calls[1], aliases); !got {
		t.Fatal("expected re-bound alias call to be may-return")
	}
}

func findDirectIdentCallsInFunc(t *testing.T, fn *ast.FuncDecl, identName string) []*ast.CallExpr {
	t.Helper()

	var calls []*ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := stripParens(call.Fun).(*ast.Ident)
		if !ok || ident.Name != identName {
			return true
		}
		calls = append(calls, call)
		return true
	})
	return calls
}
