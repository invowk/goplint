// SPDX-License-Identifier: MPL-2.0

package main

import (
	"testing"

	"github.com/invowk/invowk/tools/goplint/goplint"
)

func TestParseAnalysisJSON(t *testing.T) {
	t.Parallel()

	t.Run("single package with findings", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: "primitive", Message: "struct field pkg.Foo.Bar uses primitive type string"},
					{Category: "missing-validate", Message: "named type pkg.MyType has no Validate() method"},
				},
			},
		})

		findings, err := parseAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings["primitive"]) != 1 {
			t.Errorf("expected 1 primitive finding, got %d", len(findings["primitive"]))
		}
		if len(findings["missing-validate"]) != 1 {
			t.Errorf("expected 1 missing-validate finding, got %d", len(findings["missing-validate"]))
		}
	})

	t.Run("deduplicates across packages", func(t *testing.T) {
		t.Parallel()
		// Simulate the same diagnostic appearing in both the package and its test variant.
		diag := analysisDiagnostic{
			Category: "primitive",
			Message:  "struct field pkg.Foo.Bar uses primitive type string",
		}
		pkg1 := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {"goplint": {diag}},
		})
		pkg2 := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg [example.com/pkg.test]": {"goplint": {diag}},
		})
		combined := append(pkg1, pkg2...)

		findings, err := parseAnalysisJSON(combined)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings["primitive"]) != 1 {
			t.Errorf("expected 1 deduplicated finding, got %d", len(findings["primitive"]))
		}
	})

	t.Run("uses finding ID from diagnostic URL", func(t *testing.T) {
		t.Parallel()
		const findingID = "gpl1_deadbeef"
		input := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{
						Category: "primitive",
						Message:  "struct field pkg.Foo.Bar uses primitive type string",
						URL:      goplint.DiagnosticURLForFinding(findingID),
					},
				},
			},
		})

		findings, err := parseAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings["primitive"]) != 1 {
			t.Fatalf("expected 1 primitive finding, got %d", len(findings["primitive"]))
		}
		if findings["primitive"][0].ID != findingID {
			t.Errorf("expected finding ID %q, got %q", findingID, findings["primitive"][0].ID)
		}
	})

	t.Run("handles CFA categories in baseline parsing", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{
						Category: goplint.CategoryUnvalidatedCast,
						Message:  "type conversion to pkg.CommandName from non-constant without Validate() check",
						URL:      goplint.DiagnosticURLForFinding("gpl1_cfa_unvalidated"),
					},
					{
						Category: goplint.CategoryUseBeforeValidate,
						Message:  "variable x of type pkg.CommandName used before Validate() in same block",
						URL:      goplint.DiagnosticURLForFinding("gpl1_cfa_ubv"),
					},
				},
			},
		})

		findings, err := parseAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings[goplint.CategoryUnvalidatedCast]) != 1 {
			t.Fatalf("expected 1 %s finding, got %d", goplint.CategoryUnvalidatedCast, len(findings[goplint.CategoryUnvalidatedCast]))
		}
		if len(findings[goplint.CategoryUseBeforeValidate]) != 1 {
			t.Fatalf("expected 1 %s finding, got %d", goplint.CategoryUseBeforeValidate, len(findings[goplint.CategoryUseBeforeValidate]))
		}
	})

	t.Run("falls back to derived ID when URL is missing", func(t *testing.T) {
		t.Parallel()
		const (
			category = "primitive"
			message  = "struct field pkg.Foo.Bar uses primitive type string"
		)
		input := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: category, Message: message},
				},
			},
		})

		findings, err := parseAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings[category]) != 1 {
			t.Fatalf("expected 1 %s finding, got %d", category, len(findings[category]))
		}
		wantID := goplint.FallbackFindingIDForDiagnostic(category, "example.com/pkg", message)
		if findings[category][0].ID != wantID {
			t.Errorf("expected fallback ID %q, got %q", wantID, findings[category][0].ID)
		}
	})

	t.Run("fallback ID uses position to keep repeated messages distinct", func(t *testing.T) {
		t.Parallel()
		const (
			category = goplint.CategoryUnusedValidateResult
			message  = "Validate() result discarded — error return is unused"
		)
		input := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: category, Posn: "pkg/file.go:10:2", Message: message},
					{Category: category, Posn: "pkg/file.go:20:2", Message: message},
				},
			},
		})

		findings, err := parseAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings[category]) != 2 {
			t.Fatalf("expected 2 %s findings, got %d", category, len(findings[category]))
		}
	})

	t.Run("filters out stale-exception diagnostics", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: "primitive", Message: "real finding"},
					{Category: goplint.CategoryStaleException, Message: "stale exception: pattern ..."},
				},
			},
		})

		findings, err := parseAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings["primitive"]) != 1 {
			t.Errorf("expected 1 primitive finding, got %d", len(findings["primitive"]))
		}
		if len(findings[goplint.CategoryStaleException]) != 0 {
			t.Errorf("expected 0 stale-exception findings, got %d", len(findings[goplint.CategoryStaleException]))
		}
	})

	t.Run("skips entries with empty category or message", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: "", Message: "orphaned message"},
					{Category: "primitive", Message: ""},
					{Category: "primitive", Message: "valid finding"},
				},
			},
		})

		findings, err := parseAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings["primitive"]) != 1 {
			t.Errorf("expected 1 finding (empty category/message filtered), got %d", len(findings["primitive"]))
		}
	})

	t.Run("filters non-suppressible categories", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: goplint.CategoryUnknownDirective, Message: "unknown directive key"},
					{Category: "primitive", Message: "valid finding"},
				},
			},
		})

		findings, err := parseAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings[goplint.CategoryUnknownDirective]) != 0 {
			t.Errorf("expected unknown-directive to be excluded from baseline findings")
		}
		if len(findings["primitive"]) != 1 {
			t.Errorf("expected primitive finding to remain, got %d", len(findings["primitive"]))
		}
	})

	t.Run("unknown category returns error", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {
				"goplint": {
					{Category: "totally-unknown-category", Message: "unexpected"},
				},
			},
		})
		_, err := parseAnalysisJSON(input)
		if err == nil {
			t.Fatal("expected error for unknown category")
		}
	})

	t.Run("empty input returns empty findings", func(t *testing.T) {
		t.Parallel()
		findings, err := parseAnalysisJSON([]byte{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("expected 0 categories, got %d", len(findings))
		}
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		t.Parallel()
		_, err := parseAnalysisJSON([]byte("{invalid json"))
		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})

	t.Run("ignores non-goplint analyzer results", func(t *testing.T) {
		t.Parallel()
		input := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {
				"otherana": {
					{Category: "other", Message: "not our concern"},
				},
				"goplint": {
					{Category: "primitive", Message: "our finding"},
				},
			},
		})

		findings, err := parseAnalysisJSON(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(findings["other"]) != 0 {
			t.Errorf("expected 0 'other' findings, got %d", len(findings["other"]))
		}
		if len(findings["primitive"]) != 1 {
			t.Errorf("expected 1 primitive finding, got %d", len(findings["primitive"]))
		}
	})
}

func TestStableDiagnosticPosKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		posn string
		want string
	}{
		{
			name: "unix absolute path",
			posn: "/tmp/work/pkg/file.go:10:2",
			want: "example.com/pkg:file.go:10:2",
		},
		{
			name: "windows path",
			posn: `C:\work\pkg\file.go:10:2`,
			want: "example.com/pkg:file.go:10:2",
		},
		{
			name: "mixed separators",
			posn: `C:/work/pkg\inner/file.go:22:9`,
			want: "example.com/pkg:file.go:22:9",
		},
		{
			name: "malformed token falls back to raw",
			posn: "file.go:not-a-line",
			want: "example.com/pkg:file.go:not-a-line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stableDiagnosticPosKey("example.com/pkg", tt.posn)
			if got != tt.want {
				t.Fatalf("stableDiagnosticPosKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCanonicalPackagePath(t *testing.T) {
	t.Parallel()

	got := canonicalPackagePath("example.com/pkg [example.com/pkg.test]")
	if got != "example.com/pkg" {
		t.Fatalf("canonicalPackagePath() = %q, want %q", got, "example.com/pkg")
	}
}
