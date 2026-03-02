// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/token"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestStableFindingID(t *testing.T) {
	t.Parallel()

	id1 := StableFindingID(CategoryPrimitive, "struct-field", "pkg.Type.Field", "string")
	id2 := StableFindingID(CategoryPrimitive, "struct-field", "pkg.Type.Field", "string")
	if id1 == "" {
		t.Fatal("StableFindingID returned empty ID")
	}
	if id1 != id2 {
		t.Fatalf("StableFindingID not deterministic: %q != %q", id1, id2)
	}

	id3 := StableFindingID(CategoryPrimitive, "struct-field", "pkg.Type.Other", "string")
	if id1 == id3 {
		t.Fatalf("expected different semantic inputs to produce different IDs: %q", id1)
	}
}

func TestDiagnosticURLRoundTrip(t *testing.T) {
	t.Parallel()

	id := StableFindingID(CategoryMissingConstructor, "pkg.Config", "NewConfig")
	url := DiagnosticURLForFinding(id)
	got := FindingIDFromDiagnosticURL(url)
	if got != id {
		t.Fatalf("FindingIDFromDiagnosticURL(%q) = %q, want %q", url, got, id)
	}

	if other := FindingIDFromDiagnosticURL("https://example.com/not-goplint"); other != "" {
		t.Fatalf("expected non-goplint URL to return empty ID, got %q", other)
	}
}

func TestDiagnosticURLWithMeta(t *testing.T) {
	t.Parallel()

	id := StableFindingID(CategoryStaleException, "pkg.Type.Field")
	url := DiagnosticURLForFindingWithMeta(id, map[string]string{
		"pattern": "pkg.Type.Field",
		"reason":  "legacy",
	})
	if got := FindingIDFromDiagnosticURL(url); got != id {
		t.Fatalf("FindingIDFromDiagnosticURL(%q) = %q, want %q", url, got, id)
	}
	if got := FindingMetaFromDiagnosticURL(url, "pattern"); got != "pkg.Type.Field" {
		t.Fatalf("FindingMetaFromDiagnosticURL(..., pattern) = %q, want %q", got, "pkg.Type.Field")
	}
}

func TestFallbackFindingID(t *testing.T) {
	t.Parallel()

	id := FallbackFindingID(CategoryPrimitive, "struct field pkg.Foo.Bar uses primitive type string")
	if id == "" {
		t.Fatal("FallbackFindingID returned empty ID")
	}
}

func TestFallbackFindingIDForDiagnostic(t *testing.T) {
	t.Parallel()

	id1 := FallbackFindingIDForDiagnostic(CategoryUnusedValidateResult, "a.go:10:2", "Validate() result discarded")
	id2 := FallbackFindingIDForDiagnostic(CategoryUnusedValidateResult, "a.go:20:2", "Validate() result discarded")
	if id1 == "" || id2 == "" {
		t.Fatal("FallbackFindingIDForDiagnostic returned empty ID")
	}
	if id1 == id2 {
		t.Fatalf("expected different positions to produce different IDs: %q", id1)
	}
}

func TestFallbackFindingIDForDiagnostic_EmptyPosUsesFallback(t *testing.T) {
	t.Parallel()

	category := CategoryUnusedValidateResult
	message := "Validate() result discarded"
	got := FallbackFindingIDForDiagnostic(category, "", message)
	want := FallbackFindingID(category, message)
	if got != want {
		t.Fatalf("FallbackFindingIDForDiagnostic(empty pos) = %q, want %q", got, want)
	}
}

func TestStablePosKey(t *testing.T) {
	t.Parallel()

	t.Run("unknown guards", func(t *testing.T) {
		t.Parallel()

		if got := stablePosKey(nil, token.NoPos); got != "unknown-pos" {
			t.Fatalf("stablePosKey(nil, NoPos) = %q, want %q", got, "unknown-pos")
		}

		pass := &analysis.Pass{Fset: token.NewFileSet()}
		if got := stablePosKey(pass, token.NoPos); got != "unknown-pos" {
			t.Fatalf("stablePosKey(pass, NoPos) = %q, want %q", got, "unknown-pos")
		}
	})

	t.Run("empty filename", func(t *testing.T) {
		t.Parallel()

		fset := token.NewFileSet()
		file := fset.AddFile("", -1, 20)
		pos := file.Pos(1)
		pass := &analysis.Pass{Fset: fset}

		if got := stablePosKey(pass, pos); got != "unknown-pos" {
			t.Fatalf("stablePosKey(empty filename) = %q, want %q", got, "unknown-pos")
		}
	})

	t.Run("formats base filename line and column", func(t *testing.T) {
		t.Parallel()

		fset := token.NewFileSet()
		file := fset.AddFile("/tmp/example/sample.go", -1, 32)
		pos := file.Pos(5)
		pass := &analysis.Pass{Fset: fset}
		got := stablePosKey(pass, pos)

		if !strings.HasPrefix(got, "sample.go:1:") {
			t.Fatalf("stablePosKey() = %q, want prefix %q", got, "sample.go:1:")
		}
	})
}

func TestDiagnosticURLForFinding_EmptyID(t *testing.T) {
	t.Parallel()

	if got := DiagnosticURLForFinding(""); got != "" {
		t.Fatalf("DiagnosticURLForFinding(empty) = %q, want empty", got)
	}
}

func TestFindingMetaFromDiagnosticURL(t *testing.T) {
	t.Parallel()

	id := StableFindingID(CategoryStaleException, "pkg.Type.Field")
	url := DiagnosticURLForFindingWithMeta(id, map[string]string{
		"pattern": "pkg.Type.Field",
		"reason":  "legacy",
	})

	tests := []struct {
		name string
		raw  string
		key  string
		want string
	}{
		{
			name: "extracts existing key",
			raw:  url,
			key:  "pattern",
			want: "pkg.Type.Field",
		},
		{
			name: "missing key returns empty",
			raw:  url,
			key:  "missing",
			want: "",
		},
		{
			name: "empty key returns empty",
			raw:  url,
			key:  "",
			want: "",
		},
		{
			name: "non goplint url returns empty",
			raw:  "https://example.com?id=1",
			key:  "pattern",
			want: "",
		},
		{
			name: "no query returns empty",
			raw:  DiagnosticURLForFinding(id),
			key:  "pattern",
			want: "",
		},
		{
			name: "invalid query returns empty",
			raw:  DiagnosticURLForFinding(id) + "?pattern=%zz",
			key:  "pattern",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FindingMetaFromDiagnosticURL(tt.raw, tt.key); got != tt.want {
				t.Fatalf("FindingMetaFromDiagnosticURL(%q, %q) = %q, want %q", tt.raw, tt.key, got, tt.want)
			}
		})
	}
}
