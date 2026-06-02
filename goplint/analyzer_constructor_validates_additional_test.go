// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestFactMatchesReturnType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fact      ValidatesTypeFact
		returnTyp string
		returnPkg string
		want      bool
	}{
		{
			name:      "exact match",
			fact:      ValidatesTypeFact{TypeName: "Server", TypePkgPath: "example.com/model"},
			returnTyp: "Server",
			returnPkg: "example.com/model",
			want:      true,
		},
		{
			name:      "legacy fact without package path",
			fact:      ValidatesTypeFact{TypeName: "Server", TypePkgPath: ""},
			returnTyp: "Server",
			returnPkg: "example.com/model",
			want:      true,
		},
		{
			name:      "type mismatch",
			fact:      ValidatesTypeFact{TypeName: "Config", TypePkgPath: "example.com/model"},
			returnTyp: "Server",
			returnPkg: "example.com/model",
			want:      false,
		},
		{
			name:      "package mismatch",
			fact:      ValidatesTypeFact{TypeName: "Server", TypePkgPath: "example.com/other"},
			returnTyp: "Server",
			returnPkg: "example.com/model",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := factMatchesReturnType(tt.fact, tt.returnTyp, tt.returnPkg)
			if got != tt.want {
				t.Fatalf("factMatchesReturnType(%+v, %q, %q) = %v, want %v", tt.fact, tt.returnTyp, tt.returnPkg, got, tt.want)
			}
		})
	}
}

func TestResolveDirectiveTypeIdentity(t *testing.T) {
	t.Parallel()

	t.Run("full package path in directive", func(t *testing.T) {
		t.Parallel()

		typeName, typePkgPath := resolveDirectiveTypeIdentity(nil, nil, "example.com/model.Server")
		if typeName != "Server" || typePkgPath != "example.com/model" {
			t.Fatalf("resolveDirectiveTypeIdentity(full path) = (%q, %q), want (%q, %q)", typeName, typePkgPath, "Server", "example.com/model")
		}
	})

	t.Run("fallback to current package", func(t *testing.T) {
		t.Parallel()

		pass := &analysis.Pass{Pkg: types.NewPackage("example.com/current", "current")}
		typeName, typePkgPath := resolveDirectiveTypeIdentity(pass, nil, "alias.Server")
		if typeName != "Server" || typePkgPath != "example.com/current" {
			t.Fatalf("resolveDirectiveTypeIdentity(alias fallback) = (%q, %q), want (%q, %q)", typeName, typePkgPath, "Server", "example.com/current")
		}
	})

	t.Run("empty directive", func(t *testing.T) {
		t.Parallel()

		typeName, typePkgPath := resolveDirectiveTypeIdentity(nil, nil, "   ")
		if typeName != "" || typePkgPath != "" {
			t.Fatalf("resolveDirectiveTypeIdentity(empty) = (%q, %q), want empty", typeName, typePkgPath)
		}
	})

	t.Run("alias disambiguates matching signature types", func(t *testing.T) {
		t.Parallel()

		currentPkg := types.NewPackage("example.com/current", "current")
		otherPkg := types.NewPackage("example.com/other", "other")
		modelPkg := types.NewPackage("example.com/model", "model")
		otherServer := types.NewNamed(types.NewTypeName(0, otherPkg, "Server", nil), types.NewStruct(nil, nil), nil)
		modelServer := types.NewNamed(types.NewTypeName(0, modelPkg, "Server", nil), types.NewStruct(nil, nil), nil)
		sig := types.NewSignatureType(
			nil,
			nil,
			nil,
			types.NewTuple(types.NewVar(0, nil, "input", otherServer)),
			types.NewTuple(types.NewVar(0, nil, "", modelServer)),
			false,
		)
		fn := &ast.FuncDecl{Name: ast.NewIdent("helper")}
		pass := &analysis.Pass{
			Pkg:       currentPkg,
			TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{fn.Name: types.NewFunc(0, currentPkg, "helper", sig)}},
		}

		typeName, typePkgPath := resolveDirectiveTypeIdentity(pass, fn, "model.Server")
		if typeName != "Server" || typePkgPath != "example.com/model" {
			t.Fatalf("resolveDirectiveTypeIdentity(alias) = (%q, %q), want (%q, %q)", typeName, typePkgPath, "Server", "example.com/model")
		}
	})
}

func TestInferDirectiveTypePkgPath(t *testing.T) {
	t.Parallel()

	t.Run("guard conditions", func(t *testing.T) {
		t.Parallel()

		if got := inferDirectiveTypePkgPath(nil, nil, "Server", ""); got != "" {
			t.Fatalf("inferDirectiveTypePkgPath(nil) = %q, want empty", got)
		}

		fn := &ast.FuncDecl{Name: ast.NewIdent("helper")}
		pass := &analysis.Pass{TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{}}}
		if got := inferDirectiveTypePkgPath(pass, fn, "Server", ""); got != "" {
			t.Fatalf("inferDirectiveTypePkgPath(def missing) = %q, want empty", got)
		}

		pass.TypesInfo.Defs[fn.Name] = types.NewVar(0, nil, "helper", types.Typ[types.Int])
		if got := inferDirectiveTypePkgPath(pass, fn, "Server", ""); got != "" {
			t.Fatalf("inferDirectiveTypePkgPath(non-func def) = %q, want empty", got)
		}
	})

	t.Run("matches params and alias", func(t *testing.T) {
		t.Parallel()

		modelPkg := types.NewPackage("example.com/model", "model")
		serverObj := types.NewTypeName(0, modelPkg, "Server", nil)
		serverType := types.NewNamed(serverObj, types.NewStruct(nil, nil), nil)
		sig := types.NewSignatureType(
			nil,
			nil,
			nil,
			types.NewTuple(types.NewVar(0, nil, "s", serverType)),
			types.NewTuple(),
			false,
		)
		fnObj := types.NewFunc(0, nil, "helper", sig)
		fn := &ast.FuncDecl{Name: ast.NewIdent("helper")}
		pass := &analysis.Pass{
			TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{fn.Name: fnObj}},
		}

		if got := inferDirectiveTypePkgPath(pass, fn, "Server", ""); got != "example.com/model" {
			t.Fatalf("inferDirectiveTypePkgPath(params) = %q, want %q", got, "example.com/model")
		}
		if got := inferDirectiveTypePkgPath(pass, fn, "Server", "model"); got != "example.com/model" {
			t.Fatalf("inferDirectiveTypePkgPath(alias match) = %q, want %q", got, "example.com/model")
		}
		if got := inferDirectiveTypePkgPath(pass, fn, "Server", "other"); got != "" {
			t.Fatalf("inferDirectiveTypePkgPath(alias mismatch) = %q, want empty", got)
		}
	})

	t.Run("matches results including pointers", func(t *testing.T) {
		t.Parallel()

		modelPkg := types.NewPackage("example.com/remote", "remote")
		serverObj := types.NewTypeName(0, modelPkg, "Server", nil)
		serverType := types.NewNamed(serverObj, types.NewStruct(nil, nil), nil)
		sig := types.NewSignatureType(
			nil,
			nil,
			nil,
			types.NewTuple(types.NewVar(0, nil, "id", types.Typ[types.Int])),
			types.NewTuple(types.NewVar(0, nil, "", types.NewPointer(serverType))),
			false,
		)
		fnObj := types.NewFunc(0, nil, "helperResult", sig)
		fn := &ast.FuncDecl{Name: ast.NewIdent("helperResult")}
		pass := &analysis.Pass{
			TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{fn.Name: fnObj}},
		}

		if got := inferDirectiveTypePkgPath(pass, fn, "Server", ""); got != "example.com/remote" {
			t.Fatalf("inferDirectiveTypePkgPath(results) = %q, want %q", got, "example.com/remote")
		}
	})
}

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
	returnType := pass.TypesInfo.TypeOf(fn.Type.Results.List[0].Type)
	returnTypeKey := typeIdentityKey(returnType)
	bareReturnIncludesTarget := constructorBareReturnIncludesType(pass, fn, returnTypeKey)
	returnKeys := collectConstructorReturnTargetKeys(pass, fn, returnTypeKey, bareReturnIncludesTarget)
	if len(returnKeys) == 0 {
		t.Fatal("expected return target keys to be collected")
	}

	matcher := constructorReturnTargetMatcher(returnTypeKey, returnKeys)
	realIdent := findIdentInFunc(t, fn, "real")
	if !matcher(pass, realIdent) {
		t.Fatal("expected matcher to accept returned variable")
	}
	decoyIdent := findIdentInFunc(t, fn, "decoy")
	if matcher(pass, decoyIdent) {
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

	errorInfo := resolveReturnTypeValidateInfo(pass, findFuncDecl(t, file, "NewOnlyError"))
	if errorInfo.HasValidate || errorInfo.TypeKey != "error" {
		t.Fatalf("error-only return info = %+v, want non-validatable error info", errorInfo)
	}
}

func TestBodyCallsValidateTransitiveFollowsBareAndMethodHelpers(t *testing.T) {
	t.Parallel()

	src := `package testpkg
type Server struct{}
func (s *Server) Validate() error { return nil }
func (s *Server) init() error { return s.Validate() }

type Other struct{}
func (o *Other) init() error { return nil }

func helper(s *Server) error { return s.Validate() }
func nested(s *Server) error { return helper(s) }

func bareCaller(s *Server) error { return nested(s) }
func methodCaller(s *Server) error { return s.init() }
func conditionalCaller(s *Server, ok bool) error {
	if ok {
		return helper(s)
	}
	return nil
}
func wrongMethodCaller(o *Other) error { return o.init() }`

	pass, file := buildTypedPassFromSource(t, src)
	returnTypeKey := typeIdentityKey(pass.Pkg.Scope().Lookup("Server").Type())
	if !bodyCallsValidateTransitive(pass, findFuncDecl(t, file, "bareCaller").Body, "Server", "testpkg", returnTypeKey, nil, 0) {
		t.Fatal("expected bare helper chain to validate Server")
	}
	if !bodyCallsValidateTransitive(pass, findFuncDecl(t, file, "methodCaller").Body, "Server", "testpkg", returnTypeKey, nil, 0) {
		t.Fatal("expected method helper chain to validate Server")
	}
	if bodyCallsValidateTransitive(pass, findFuncDecl(t, file, "conditionalCaller").Body, "Server", "testpkg", returnTypeKey, nil, 0) {
		t.Fatal("conditional helper call should not prove all paths validate")
	}
	if bodyCallsValidateTransitive(pass, findFuncDecl(t, file, "wrongMethodCaller").Body, "Server", "testpkg", returnTypeKey, nil, 0) {
		t.Fatal("method call on another type should not validate Server")
	}
	if bodyCallsValidateTransitive(pass, findFuncDecl(t, file, "bareCaller").Body, "Server", "testpkg", returnTypeKey, nil, maxTransitiveDepth) {
		t.Fatal("depth at maxTransitiveDepth should not recurse")
	}
}
