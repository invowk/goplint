// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestBoundaryRequestWithoutProtocolParamsSkipsSummaryWork(t *testing.T) {
	t.Parallel()

	const source = `package testpkg
type Value string
func (value Value) Validate() error { return nil }
func helper(value Value) error { return value.Validate() }
func Exported(value Value) error { return helper(value) }
`
	pass, file := buildTypedPassFromSource(t, source)
	function := findFuncDecl(t, file, "Exported")
	ssaResult := buildSSAForPass(pass)
	cache := &sync.Map{}

	inspectBoundaryRequestValidation(
		pass,
		function,
		nil,
		nil,
		defaultCFGMaxStates,
		cfgProtocolRefinementOptions{},
		ssaResult,
		cache,
	)

	cache.Range(func(key, value any) bool {
		t.Fatalf("non-boundary declaration populated callee summary cache: key=%v value=%T", key, value)
		return false
	})
}

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
	if params[0].target.typeKey != "testpkg.Request" || params[1].target.typeKey != "testpkg.RunOptions" {
		t.Fatalf(
			"param type keys = %q, %q; want testpkg.Request, testpkg.RunOptions",
			params[0].target.typeKey,
			params[1].target.typeKey,
		)
	}
	if params[0].target.staticType == nil || params[1].target.staticType == nil {
		t.Fatal("boundary targets must retain their static Go types")
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

func TestBoundaryRequestDefaulting(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Request struct {
	Name string
	Image string
}
func (Request) Validate() error { return nil }

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
}

func TestBoundaryRequestLocalAliasStmt(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Request struct{}
func (Request) Validate() error { return nil }
var escaped any

func Alias(req Request) error {
	alias := &req
	_ = alias
	return nil
}

func Escape(req Request) error {
	escaped = &req
	return nil
}`

	pass, file := buildTypedPassFromSource(t, src)
	aliasFn := findFuncDecl(t, file, "Alias")
	aliasTarget := collectBoundaryRequestParams(pass, aliasFn)[0].target
	if !boundaryRequestLocalAliasStmt(pass, aliasFn.Body.List[0], aliasTarget) {
		t.Fatal("local address alias should not consume the boundary request")
	}

	escapeFn := findFuncDecl(t, file, "Escape")
	escapeTarget := collectBoundaryRequestParams(pass, escapeFn)[0].target
	if boundaryRequestLocalAliasStmt(pass, escapeFn.Body.List[0], escapeTarget) {
		t.Fatal("package-level assignment must remain an escape")
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
