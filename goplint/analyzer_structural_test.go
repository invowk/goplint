// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestCountParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fields *ast.FieldList
		want   int
	}{
		{
			name:   "empty field list",
			fields: &ast.FieldList{},
			want:   0,
		},
		{
			name: "single unnamed param",
			fields: &ast.FieldList{
				List: []*ast.Field{
					{Type: ast.NewIdent("string")},
				},
			},
			want: 1,
		},
		{
			name: "single named param",
			fields: &ast.FieldList{
				List: []*ast.Field{
					{Names: []*ast.Ident{ast.NewIdent("name")}, Type: ast.NewIdent("string")},
				},
			},
			want: 1,
		},
		{
			name: "multi-name field (a, b int) counts as 2",
			fields: &ast.FieldList{
				List: []*ast.Field{
					{
						Names: []*ast.Ident{ast.NewIdent("a"), ast.NewIdent("b")},
						Type:  ast.NewIdent("int"),
					},
				},
			},
			want: 2,
		},
		{
			name: "mixed named and unnamed",
			fields: &ast.FieldList{
				List: []*ast.Field{
					{Names: []*ast.Ident{ast.NewIdent("ctx")}, Type: ast.NewIdent("context.Context")},
					{Type: ast.NewIdent("string")},
					{Names: []*ast.Ident{ast.NewIdent("x"), ast.NewIdent("y")}, Type: ast.NewIdent("int")},
				},
			},
			want: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := countParams(tt.fields)
			if got != tt.want {
				t.Errorf("countParams() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCapitalizeFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"single lowercase", "x", "X"},
		{"single uppercase", "X", "X"},
		{"camelCase", "shellArgs", "ShellArgs"},
		{"already capitalized", "Shell", "Shell"},
		{"all lowercase", "timeout", "Timeout"},
		{"unicode", "über", "Über"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := capitalizeFirst(tt.input)
			if got != tt.want {
				t.Errorf("capitalizeFirst(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCanonicalOptionTypeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{
			name:  "empty input",
			input: nil,
			want:  "",
		},
		{
			name:  "single value",
			input: []string{"ServerOption"},
			want:  "ServerOption",
		},
		{
			name:  "multiple values returns lexicographically smallest",
			input: []string{"ZOption", "AOption", "BOption"},
			want:  "AOption",
		},
		{
			name:  "input order does not matter",
			input: []string{"MOption", "BOption", "COption"},
			want:  "BOption",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := canonicalOptionTypeName(tt.input)
			if got != tt.want {
				t.Errorf("canonicalOptionTypeName(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
