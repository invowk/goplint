// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestResolveConstantValue(t *testing.T) {
	t.Parallel()

	src := `package testpkg
import "net/http"

const LocalS = "s"
const LocalI = 7
const LocalB = true
var LocalVar = "v"

func use() {
	_ = LocalS
	_ = LocalI
	_ = LocalB
	_ = LocalVar
	_ = http.MethodGet
}`

	pass, file := buildTypedPassFromSource(t, src)
	useFn := findFuncDecl(t, file, "use")

	localS := findIdentInFunc(t, useFn, "LocalS")
	localI := findIdentInFunc(t, useFn, "LocalI")
	localB := findIdentInFunc(t, useFn, "LocalB")
	localVar := findIdentInFunc(t, useFn, "LocalVar")
	methodGet := findSelectorExprInFunc(t, useFn, "http", "MethodGet")

	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{
			name: "string const ident",
			expr: localS,
			want: "s",
		},
		{
			name: "int const ident",
			expr: localI,
			want: "7",
		},
		{
			name: "bool const unsupported kind",
			expr: localB,
			want: "",
		},
		{
			name: "non const object",
			expr: localVar,
			want: "",
		},
		{
			name: "selector const",
			expr: methodGet,
			want: "GET",
		},
		{
			name: "non ident or selector",
			expr: &ast.BasicLit{},
			want: "",
		},
		{
			name: "unresolved ident",
			expr: ast.NewIdent("Missing"),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveConstantValue(pass, tt.expr); got != tt.want {
				t.Fatalf("resolveConstantValue() = %q, want %q", got, tt.want)
			}
		})
	}
}
