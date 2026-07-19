// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCollectBaselineFindingsRejectsDuplicateAndCollidedIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stream  string
		wantErr string
	}{
		{
			name: "duplicate emission",
			stream: findingJSONL("example.com/a", CategoryPrimitive, "same-id", "finding a", "a.go:1:1") +
				findingJSONL("example.com/a", CategoryPrimitive, "same-id", "finding a", "a.go:1:1"),
			wantErr: "duplicate finding ID",
		},
		{
			name: "cross package collision",
			stream: findingJSONL("example.com/left/shared", CategoryPrimitive, "same-id", "same message", "shared.go:1:1") +
				findingJSONL("example.com/right/shared", CategoryPrimitive, "same-id", "same message", "shared.go:1:1"),
			wantErr: "collided finding ID",
		},
		{
			name: "cross category collision",
			stream: findingJSONL("example.com/a", CategoryPrimitive, "same-id", "finding a", "a.go:1:1") +
				findingJSONL("example.com/a", CategoryMissingValidate, "same-id", "finding b", "a.go:2:1"),
			wantErr: "collided finding ID",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := CollectBaselineFindingsFromStream([]byte(test.stream))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("CollectBaselineFindingsFromStream() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestCollectBaselineFindingsFromAnalysisJSONRejectsDuplicateAndCollidedIDs(t *testing.T) {
	t.Parallel()

	diagnostic := func(category, id, message, position string) AnalysisDiagnostic {
		return AnalysisDiagnostic{
			Category: category,
			URL:      DiagnosticURLForFinding(id),
			Message:  message,
			Posn:     position,
		}
	}
	tests := []struct {
		name    string
		result  AnalysisResult
		wantErr string
	}{
		{
			name: "duplicate emission",
			result: AnalysisResult{"example.com/a": {"goplint": {
				diagnostic(CategoryPrimitive, "same-id", "finding a", "a.go:1:1"),
				diagnostic(CategoryPrimitive, "same-id", "finding a", "a.go:1:1"),
			}}},
			wantErr: "duplicate finding ID",
		},
		{
			name: "cross package collision",
			result: AnalysisResult{
				"example.com/left/shared":  {"goplint": {diagnostic(CategoryPrimitive, "same-id", "same message", "shared.go:1:1")}},
				"example.com/right/shared": {"goplint": {diagnostic(CategoryPrimitive, "same-id", "same message", "shared.go:1:1")}},
			},
			wantErr: "collided finding ID",
		},
		{
			name: "cross category collision",
			result: AnalysisResult{"example.com/a": {"goplint": {
				diagnostic(CategoryPrimitive, "same-id", "finding a", "a.go:1:1"),
				diagnostic(CategoryMissingValidate, "same-id", "finding b", "a.go:2:1"),
			}}},
			wantErr: "collided finding ID",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(test.result)
			if err != nil {
				t.Fatalf("marshal analysis result: %v", err)
			}
			_, err = CollectBaselineFindingsFromAnalysisJSON(data)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("CollectBaselineFindingsFromAnalysisJSON() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestBaselineLoadAndWriteRejectDuplicateAndCollidedIDs(t *testing.T) {
	t.Parallel()

	t.Run("load duplicate", func(t *testing.T) {
		t.Parallel()
		path := writeTempFile(t, "baseline.toml", `
[primitive]
entries = [
    { id = "same-id", message = "same message" },
    { id = "same-id", message = "same message" },
]
`)
		_, err := loadBaseline(path, false)
		if err == nil || !strings.Contains(err.Error(), "duplicate finding ID") {
			t.Fatalf("loadBaseline() error = %v, want duplicate rejection", err)
		}
	})

	t.Run("load collision across categories", func(t *testing.T) {
		t.Parallel()
		path := writeTempFile(t, "baseline.toml", `
[primitive]
entries = [{ id = "same-id", message = "first" }]
[missing-validate]
entries = [{ id = "same-id", message = "second" }]
`)
		_, err := loadBaseline(path, false)
		if err == nil || !strings.Contains(err.Error(), "collided finding ID") {
			t.Fatalf("loadBaseline() error = %v, want collision rejection", err)
		}
	})

	t.Run("write collision preserves destination", func(t *testing.T) {
		t.Parallel()
		path := writeTempFile(t, "baseline.toml", "original\n")
		findings := map[string][]BaselineFinding{
			CategoryPrimitive:       {{ID: "same-id", Message: "first"}},
			CategoryMissingValidate: {{ID: "same-id", Message: "second"}},
		}
		err := WriteBaseline(path, findings)
		if err == nil || !strings.Contains(err.Error(), "collided finding ID") {
			t.Fatalf("WriteBaseline() error = %v, want collision rejection", err)
		}
		if got := string(mustReadFile(t, path)); got != "original\n" {
			t.Fatalf("WriteBaseline() changed destination after rejection: %q", got)
		}
	})
}

func findingJSONL(packagePath, category, id, message, position string) string {
	return `{"package":"` + packagePath + `","category":"` + category + `","id":"` + id +
		`","message":"` + message + `","posn":"` + position + `"}` + "\n"
}

func mustReadFile(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return data
}
