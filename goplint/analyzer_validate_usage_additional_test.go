// SPDX-License-Identifier: MPL-2.0

package goplint

import (
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
