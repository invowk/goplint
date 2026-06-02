// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"strconv"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestReportValidateUsageFinding(t *testing.T) {
	t.Parallel()

	const (
		qualFuncName = "pkg.Config.Validate"
		message      = "Validate() result discarded"
	)
	pos := token.Pos(9)

	t.Run("reports when not excepted and not baselined", func(t *testing.T) {
		t.Parallel()

		var diags []analysis.Diagnostic
		pass := &analysis.Pass{
			Report: func(diag analysis.Diagnostic) {
				diags = append(diags, diag)
			},
		}

		reportValidateUsageFinding(pass, pos, qualFuncName, &ExceptionConfig{}, nil, message)

		if len(diags) != 1 {
			t.Fatalf("expected 1 diagnostic, got %d", len(diags))
		}
		if diags[0].Category != CategoryUnusedValidateResult {
			t.Fatalf("diagnostic category = %q, want %q", diags[0].Category, CategoryUnusedValidateResult)
		}
		if diags[0].Message != message {
			t.Fatalf("diagnostic message = %q, want %q", diags[0].Message, message)
		}
		if diags[0].URL == "" {
			t.Fatal("diagnostic URL should include stable finding ID")
		}
	})

	t.Run("excepted finding does not report", func(t *testing.T) {
		t.Parallel()

		var diags []analysis.Diagnostic
		pass := &analysis.Pass{
			Report: func(diag analysis.Diagnostic) {
				diags = append(diags, diag)
			},
		}

		cfg := &ExceptionConfig{
			Exceptions: []Exception{
				{Pattern: qualFuncName + ".validate-usage"},
			},
		}
		reportValidateUsageFinding(pass, pos, qualFuncName, cfg, nil, message)
		if len(diags) != 0 {
			t.Fatalf("expected 0 diagnostics for excepted finding, got %d", len(diags))
		}
	})

	t.Run("baselined finding does not report", func(t *testing.T) {
		t.Parallel()

		var diags []analysis.Diagnostic
		pass := &analysis.Pass{
			Report: func(diag analysis.Diagnostic) {
				diags = append(diags, diag)
			},
		}

		findingID := StableFindingID(CategoryUnusedValidateResult, qualFuncName, message, strconv.Itoa(int(pos)))
		bl := &BaselineConfig{
			lookupByID: map[string]map[string]bool{
				CategoryUnusedValidateResult: {findingID: true},
			},
		}
		reportValidateUsageFinding(pass, pos, qualFuncName, &ExceptionConfig{}, bl, message)
		if len(diags) != 0 {
			t.Fatalf("expected 0 diagnostics for baselined finding, got %d", len(diags))
		}
	})
}

func TestIsAllBlankForValidate(t *testing.T) {
	t.Parallel()

	call := &ast.CallExpr{}
	assign := &ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent("kept"), ast.NewIdent("_")},
		Rhs: []ast.Expr{ast.NewIdent("other"), call},
	}
	if !isAllBlankForValidate(assign, call) {
		t.Fatal("isAllBlankForValidate() = false, want true for matching blank LHS")
	}

	nonBlank := &ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent("_"), ast.NewIdent("err")},
		Rhs: []ast.Expr{ast.NewIdent("other"), call},
	}
	if isAllBlankForValidate(nonBlank, call) {
		t.Fatal("isAllBlankForValidate() = true, want false for non-blank matching LHS")
	}

	outOfRange := &ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent("_")},
		Rhs: []ast.Expr{ast.NewIdent("other"), call},
	}
	if isAllBlankForValidate(outOfRange, call) {
		t.Fatal("isAllBlankForValidate() = true, want false when RHS index has no matching LHS")
	}

	absent := &ast.CallExpr{}
	if isAllBlankForValidate(assign, absent) {
		t.Fatal("isAllBlankForValidate() = true, want false for absent call")
	}
}

func TestIsBlankValueSpecForValidate(t *testing.T) {
	t.Parallel()

	call := &ast.CallExpr{}
	valueSpec := &ast.ValueSpec{
		Names: []*ast.Ident{ast.NewIdent("kept"), ast.NewIdent("_")},
		Values: []ast.Expr{
			ast.NewIdent("other"),
			call,
		},
	}
	if !isBlankValueSpecForValidate(valueSpec, call) {
		t.Fatal("isBlankValueSpecForValidate() = false, want true for matching blank name")
	}

	nonBlank := &ast.ValueSpec{
		Names:  []*ast.Ident{ast.NewIdent("_"), ast.NewIdent("err")},
		Values: []ast.Expr{ast.NewIdent("other"), call},
	}
	if isBlankValueSpecForValidate(nonBlank, call) {
		t.Fatal("isBlankValueSpecForValidate() = true, want false for non-blank matching name")
	}

	outOfRange := &ast.ValueSpec{
		Names:  []*ast.Ident{ast.NewIdent("_")},
		Values: []ast.Expr{ast.NewIdent("other"), call},
	}
	if isBlankValueSpecForValidate(outOfRange, call) {
		t.Fatal("isBlankValueSpecForValidate() = true, want false when value index has no matching name")
	}

	absent := &ast.CallExpr{}
	if isBlankValueSpecForValidate(valueSpec, absent) {
		t.Fatal("isBlankValueSpecForValidate() = true, want false for absent call")
	}
}
