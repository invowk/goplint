// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestCollectBoundaryRequestParamsFiltersBoundaryValidateParams(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Request struct{}
func (Request) Validate() error { return nil }
type RunOptions struct{}
func (RunOptions) Validate() error { return nil }
type NotBoundary struct{}
func (NotBoundary) Validate() error { return nil }
type MissingRequest struct{}

func Accept(req Request, opts *RunOptions, _ Request, not NotBoundary, missing MissingRequest, plain string) error {
	return nil
}`

	pass, file := buildTypedPassFromSource(t, src)
	fn := findFuncDecl(t, file, "Accept")
	if got := collectBoundaryRequestParams(nil, fn); got != nil {
		t.Fatalf("nil pass produced params: %+v", got)
	}
	if got := collectBoundaryRequestParams(&analysis.Pass{}, fn); got != nil {
		t.Fatalf("pass without TypesInfo produced params: %+v", got)
	}
	if got := collectBoundaryRequestParams(pass, nil); got != nil {
		t.Fatalf("nil function produced params: %+v", got)
	}

	params := collectBoundaryRequestParams(pass, fn)
	if len(params) != 2 {
		t.Fatalf("params = %+v, want req and opts only", params)
	}
	if params[0].name != "req" || params[1].name != "opts" {
		t.Fatalf("param names = %q, %q; want req, opts", params[0].name, params[1].name)
	}
}

func TestBoundaryRequestFuncReturnsErrorShapes(t *testing.T) {
	t.Parallel()

	src := `package testpkg
func NoResults() {}
func BoolOnly() bool { return false }
func Multiple() (int, error) { return 0, nil }
func Named() (err error) { return nil }`

	_, file := buildTypedPassFromSource(t, src)
	if boundaryRequestFuncReturnsError(nil) {
		t.Fatal("nil function should not return error")
	}
	if boundaryRequestFuncReturnsError(findFuncDecl(t, file, "NoResults")) {
		t.Fatal("NoResults should not return error")
	}
	if boundaryRequestFuncReturnsError(findFuncDecl(t, file, "BoolOnly")) {
		t.Fatal("BoolOnly should not return error")
	}
	if !boundaryRequestFuncReturnsError(findFuncDecl(t, file, "Multiple")) {
		t.Fatal("Multiple should return error")
	}
	if !boundaryRequestFuncReturnsError(findFuncDecl(t, file, "Named")) {
		t.Fatal("Named should return error")
	}
}

func TestBoundaryRequestAssignedErrNameShapes(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Request struct{}
func (Request) Validate() error { return nil }

func Guard(req Request) error {
	if err := req.Validate(); err != nil {
		return err
	}
	return nil
}

func Multi(req Request) error {
	err, other := req.Validate(), 0
	_ = other
	return err
}

func Blank(req Request) error {
	_, other := req.Validate(), 0
	_ = other
	return nil
}`

	_, file := buildTypedPassFromSource(t, src)
	guard := findFuncDecl(t, file, "Guard").Body.List[0].(*ast.IfStmt)
	name, ok := boundaryRequestAssignedErrName(guard.Init)
	if !ok || name != "err" {
		t.Fatalf("guard init assigned err name (%q, %v), want (err, true)", name, ok)
	}
	multi := findFuncDecl(t, file, "Multi").Body.List[0]
	name, ok = boundaryRequestAssignedErrName(multi)
	if !ok || name != "err" {
		t.Fatalf("multi assignment assigned err name (%q, %v), want (err, true)", name, ok)
	}
	blank := findFuncDecl(t, file, "Blank").Body.List[0]
	if name, ok := boundaryRequestAssignedErrName(blank); ok {
		t.Fatalf("blank assignment assigned err name %q, want none", name)
	}
	if name, ok := boundaryRequestAssignedErrName(findFuncDecl(t, file, "Guard").Body.List[1]); ok {
		t.Fatalf("non-assignment assigned err name %q, want none", name)
	}
}

func TestBoundaryRequestErrConditionShapes(t *testing.T) {
	t.Parallel()

	trueCases := []string{"err != nil", "nil != err"}
	for _, expr := range trueCases {
		if !boundaryRequestErrCondition(parseExpr(t, expr), "err") {
			t.Fatalf("%q should be a checked error condition", expr)
		}
	}

	falseCases := []string{"err == nil", "other != nil", "err != other", "nil == err"}
	for _, expr := range falseCases {
		if boundaryRequestErrCondition(parseExpr(t, expr), "err") {
			t.Fatalf("%q should not be a checked error condition", expr)
		}
	}
}

func TestBoundaryRequestBlockTerminatesShapes(t *testing.T) {
	t.Parallel()

	if boundaryRequestBlockTerminates(nil) {
		t.Fatal("nil block should not terminate")
	}
	if boundaryRequestBlockTerminates(&ast.BlockStmt{}) {
		t.Fatal("empty block should not terminate")
	}
	if !boundaryRequestBlockTerminates(&ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{}}}) {
		t.Fatal("return block should terminate")
	}
	if !boundaryRequestBlockTerminates(&ast.BlockStmt{List: []ast.Stmt{&ast.BranchStmt{Tok: token.BREAK}}}) {
		t.Fatal("branch block should terminate")
	}
	if boundaryRequestBlockTerminates(&ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: parseExpr(t, "x")}}}) {
		t.Fatal("expression block should not terminate")
	}
}

func TestBoundaryRequestUseIgnoresValidateReceiverAndAssignmentLHS(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Request struct{ Name string }
func (Request) Validate() error { return nil }

func Probe(req Request) error {
	req.Name = "default"
	_ = req.Validate()
	_ = req.Name
	return nil
}`

	pass, file := buildTypedPassFromSource(t, src)
	fn := findFuncDecl(t, file, "Probe")
	target := collectBoundaryRequestParams(pass, fn)[0].target
	if boundaryRequestUse(pass, fn.Body.List[0], target, nil, nil, nil) {
		t.Fatal("assignment LHS should not count as pre-validation use")
	}
	if boundaryRequestUse(pass, fn.Body.List[1], target, nil, nil, nil) {
		t.Fatal("Validate receiver should not count as pre-validation use")
	}
	if !boundaryRequestUse(pass, fn.Body.List[2], target, nil, nil, nil) {
		t.Fatal("field read should count as pre-validation use")
	}
}

func TestBoundaryRequestDefaultingAndSafeDelegation(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Request struct {
	Name string
	Image string
}
func (Request) Validate() error { return nil }

func Run(req Request) error { return nil }
func RunName(name string) error { return nil }

func Default(req Request) error {
	if req.Image == "" {
		req.Image = "debian:stable-slim"
	}
	return nil
}

func UnsafeDefault(req Request) error {
	if req.Image == "" {
		req.Image = req.Name
	}
	return nil
}

func Delegate(req Request) error {
	return Run(req)
}

func UnsafeDelegate(req Request) error {
	return RunName(req.Name)
}`

	pass, file := buildTypedPassFromSource(t, src)
	defaultFn := findFuncDecl(t, file, "Default")
	defaultTarget := collectBoundaryRequestParams(pass, defaultFn)[0].target
	if !boundaryRequestDefaultingStmt(pass, defaultFn.Body.List[0], defaultTarget) {
		t.Fatal("literal default assignment should be accepted")
	}
	unsafeDefaultFn := findFuncDecl(t, file, "UnsafeDefault")
	unsafeDefaultTarget := collectBoundaryRequestParams(pass, unsafeDefaultFn)[0].target
	if boundaryRequestDefaultingStmt(pass, unsafeDefaultFn.Body.List[0], unsafeDefaultTarget) {
		t.Fatal("default assignment from request field should not be accepted")
	}
	delegateFn := findFuncDecl(t, file, "Delegate")
	delegateTarget := collectBoundaryRequestParams(pass, delegateFn)[0].target
	if !boundaryRequestSafeDelegationStmt(pass, delegateFn.Body.List[0], delegateTarget) {
		t.Fatal("exported delegation with request arg should be accepted")
	}
	unsafeDelegateFn := findFuncDecl(t, file, "UnsafeDelegate")
	unsafeDelegateTarget := collectBoundaryRequestParams(pass, unsafeDelegateFn)[0].target
	if boundaryRequestSafeDelegationStmt(pass, unsafeDelegateFn.Body.List[0], unsafeDelegateTarget) {
		t.Fatal("delegation using request field should not be accepted")
	}
}

func TestBoundaryRequestZeroLiteralShapes(t *testing.T) {
	t.Parallel()

	trueCases := []string{`""`, "0", "nil", "false"}
	for _, expr := range trueCases {
		if !boundaryRequestZeroLiteral(parseExpr(t, expr)) {
			t.Fatalf("%q should be accepted as a zero literal", expr)
		}
	}

	falseCases := []string{`"x"`, "1", "true", "makeZero()"}
	for _, expr := range falseCases {
		if boundaryRequestZeroLiteral(parseExpr(t, expr)) {
			t.Fatalf("%q should not be accepted as a zero literal", expr)
		}
	}
}

func parseExpr(t *testing.T, src string) ast.Expr {
	t.Helper()

	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("parse expression %q: %v", src, err)
	}
	return expr
}
