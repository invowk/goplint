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
