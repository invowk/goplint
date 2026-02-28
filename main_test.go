// SPDX-License-Identifier: MPL-2.0

package main

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/invowk/invowk/tools/goplint/goplint"
)

func TestExtractUpdateBaselinePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "double-dash form",
			args: []string{"--update-baseline=out.toml", "./..."},
			want: "out.toml",
		},
		{
			name: "single-dash form",
			args: []string{"-update-baseline=/tmp/baseline.toml"},
			want: "/tmp/baseline.toml",
		},
		{
			name: "mixed with other flags",
			args: []string{"-check-all", "--update-baseline=b.toml", "-config=e.toml", "./..."},
			want: "b.toml",
		},
		{
			name: "absent flag returns empty",
			args: []string{"-check-all", "-config=e.toml", "./..."},
			want: "",
		},
		{
			name: "empty args",
			args: []string{},
			want: "",
		},
		{
			name: "nil args",
			args: nil,
			want: "",
		},
		{
			name: "flag without equals returns empty",
			args: []string{"--update-baseline"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractUpdateBaselinePath(tt.args)
			if got != tt.want {
				t.Errorf("extractUpdateBaselinePath(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestBuildSubprocessArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "removes update-baseline and adds json",
			args: []string{"--update-baseline=out.toml", "-check-all", "./..."},
			want: []string{"-json", "-check-all", "./..."},
		},
		{
			name: "preserves existing json flag",
			args: []string{"-json", "--update-baseline=out.toml", "-check-all"},
			want: []string{"-json", "-check-all"},
		},
		{
			name: "removes single-dash update-baseline",
			args: []string{"-update-baseline=out.toml", "./..."},
			want: []string{"-json", "./..."},
		},
		{
			name: "preserves all other flags and patterns",
			args: []string{"-check-all", "-config=e.toml", "--update-baseline=b.toml", "./internal/...", "./pkg/..."},
			want: []string{"-json", "-check-all", "-config=e.toml", "./internal/...", "./pkg/..."},
		},
		{
			name: "empty args adds json only",
			args: []string{},
			want: []string{"-json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildSubprocessArgs(tt.args)
			if !slices.Equal(got, tt.want) {
				t.Errorf("buildSubprocessArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

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
		wantID := goplint.FallbackFindingID(category, message)
		if findings[category][0].ID != wantID {
			t.Errorf("expected fallback ID %q, got %q", wantID, findings[category][0].ID)
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

// makeAnalysisJSON serializes the go/analysis -json output format for testing.
func makeAnalysisJSON(t *testing.T, result analysisResult) []byte {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshaling test JSON: %v", err)
	}
	return data
}
