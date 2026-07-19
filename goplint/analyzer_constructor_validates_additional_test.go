// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"
)

func TestConstructorReturnTargetMatcherIgnoresDecoy(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Server struct{}
func (s *Server) Validate() error { return nil }

func NewServer() (*Server, error) {
	decoy := &Server{}
	_ = decoy.Validate()
	real := &Server{}
	return real, nil
}`

	pass, file := buildTypedPassFromSource(t, src)
	fn := findFuncDecl(t, file, "NewServer")
	returnModel, availability := buildConstructorSSAIdentityModel(pass, buildSSAForPass(pass), fn, 0)
	if !availability.ready() {
		t.Fatalf("SSA availability = %+v, want ready", availability)
	}
	returnTargets := returnModel.returnObjectKeys()
	if len(returnTargets) != 1 {
		t.Fatal("expected return target keys to be collected")
	}
	target, ok := returnModel.targetForObject(returnTargets[0])
	if !ok {
		t.Fatal("expected exact SSA return target")
	}

	realIdent := findIdentInFunc(t, fn, "real")
	if !target.matchesExpr(pass, realIdent) {
		t.Fatal("expected matcher to accept returned variable")
	}
	decoyIdent := findIdentInFunc(t, fn, "decoy")
	if target.matchesExpr(pass, decoyIdent) {
		t.Fatal("expected matcher to reject decoy variable")
	}
}

func TestResolveReturnTypeValidateInfoPointerNamedResult(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Server struct{}
func (s *Server) Validate() error { return nil }

func NewServer() (srv *Server, err error) {
	return nil, nil
}

func NewOnlyError() error {
	return nil
}

func NewErrorFirst() (error, *Server) {
	return nil, nil
}`

	pass, file := buildTypedPassFromSource(t, src)
	info := resolveReturnTypeValidateInfo(pass, findFuncDecl(t, file, "NewServer"))
	if !info.HasValidate {
		t.Fatal("expected NewServer return type to have Validate")
	}
	if info.TypeName != "Server" || info.TypePkgName != "testpkg" || info.TypePkgPath != "testpkg" {
		t.Fatalf("return type identity = (%q, %q, %q), want Server/testpkg/testpkg", info.TypeName, info.TypePkgName, info.TypePkgPath)
	}
	if info.TypeKey != "testpkg.Server" {
		t.Fatalf("return type key = %q, want %q", info.TypeKey, "testpkg.Server")
	}
	if info.ResultSlot != 0 {
		t.Fatalf("return type result slot = %d, want 0", info.ResultSlot)
	}

	errorFirstInfo := resolveReturnTypeValidateInfo(pass, findFuncDecl(t, file, "NewErrorFirst"))
	if !errorFirstInfo.HasValidate || errorFirstInfo.TypeKey != "testpkg.Server" || errorFirstInfo.ResultSlot != 1 {
		t.Fatalf("error-first return info = %+v, want validatable Server at slot 1", errorFirstInfo)
	}

	errorInfo := resolveReturnTypeValidateInfo(pass, findFuncDecl(t, file, "NewOnlyError"))
	if errorInfo.HasValidate || errorInfo.TypeKey != "error" {
		t.Fatalf("error-only return info = %+v, want non-validatable error info", errorInfo)
	}
}
