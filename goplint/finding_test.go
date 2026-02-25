// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

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

func TestFallbackFindingID(t *testing.T) {
	t.Parallel()

	id := FallbackFindingID(CategoryPrimitive, "struct field pkg.Foo.Bar uses primitive type string")
	if id == "" {
		t.Fatal("FallbackFindingID returned empty ID")
	}
}
