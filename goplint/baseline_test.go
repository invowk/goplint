// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBaseline(t *testing.T) {
	t.Parallel()

	t.Run("empty path returns empty baseline", func(t *testing.T) {
		t.Parallel()
		bl, err := loadBaseline("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bl.Count() != 0 {
			t.Errorf("expected 0 entries, got %d", bl.Count())
		}
		// Empty baseline should match nothing.
		if bl.Contains(CategoryPrimitive, "anything") {
			t.Error("empty baseline should not match anything")
		}
	})

	t.Run("nonexistent file returns empty baseline", func(t *testing.T) {
		t.Parallel()
		bl, err := loadBaseline("/nonexistent/path/baseline.toml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bl.Count() != 0 {
			t.Errorf("expected 0 entries, got %d", bl.Count())
		}
	})

	t.Run("valid TOML parses correctly", func(t *testing.T) {
		t.Parallel()
		content := `
[primitive]
messages = [
    "struct field pkg.Foo.Bar uses primitive type string",
    "parameter \"name\" of pkg.Func uses primitive type string",
]

[missing-isvalid]
messages = [
    "named type pkg.MyType has no IsValid() method",
]

[missing-constructor]
messages = [
    "exported struct pkg.Config has no NewConfig() constructor",
]
`
		path := writeTempFile(t, "baseline.toml", content)
		bl, err := loadBaseline(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if bl.Count() != 4 {
			t.Errorf("expected 4 entries, got %d", bl.Count())
		}
	})

	t.Run("v2 entries parse correctly", func(t *testing.T) {
		t.Parallel()
		content := `
[primitive]
entries = [
    { id = "id-primitive-1", message = "struct field pkg.Foo.Bar uses primitive type string" },
]
`
		path := writeTempFile(t, "baseline-v2.toml", content)
		bl, err := loadBaseline(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bl.Count() != 1 {
			t.Errorf("expected 1 entry, got %d", bl.Count())
		}
		if !bl.ContainsFinding(CategoryPrimitive, "id-primitive-1", "") {
			t.Error("expected id-based lookup to match v2 entry")
		}
	})

	t.Run("invalid TOML returns error", func(t *testing.T) {
		t.Parallel()
		path := writeTempFile(t, "bad.toml", "[[invalid toml")
		_, err := loadBaseline(path)
		if err == nil {
			t.Fatal("expected error for invalid TOML")
		}
	})
}

func TestBaselineContains(t *testing.T) {
	t.Parallel()

	content := `
[primitive]
messages = [
    "struct field pkg.Foo.Bar uses primitive type string",
    "return value of pkg.Func uses primitive type int",
]

[missing-isvalid]
messages = [
    "named type pkg.MyType has no IsValid() method",
]

[missing-stringer]
messages = [
    "named type pkg.MyType has no String() method",
]

[missing-constructor]
messages = [
    "exported struct pkg.Config has no NewConfig() constructor",
]

[wrong-constructor-sig]
messages = [
    "constructor NewFoo() for pkg.Foo returns Bar, expected Foo",
]

[wrong-isvalid-sig]
messages = [
    "named type pkg.BadValid has IsValid() but wrong signature (want func() (bool, []error))",
]

[wrong-stringer-sig]
messages = [
    "named type pkg.BadStr has String() but wrong signature (want func() string)",
]

[missing-func-options]
messages = [
    "constructor NewBig() for pkg.Big has 5 non-option parameters; consider using functional options",
]

[missing-immutability]
messages = [
    "struct pkg.Svc has NewSvc() constructor but field Addr is exported",
]

[missing-struct-isvalid]
messages = [
    "struct pkg.Svc has constructor but no IsValid() method",
]

[wrong-struct-isvalid-sig]
messages = [
    "struct pkg.BadSvc has IsValid() but wrong signature (want func() (bool, []error))",
]
`
	path := writeTempFile(t, "baseline.toml", content)
	bl, err := loadBaseline(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		category string
		message  string
		want     bool
	}{
		{
			name:     "primitive match",
			category: CategoryPrimitive,
			message:  "struct field pkg.Foo.Bar uses primitive type string",
			want:     true,
		},
		{
			name:     "primitive no match",
			category: CategoryPrimitive,
			message:  "struct field pkg.Foo.Baz uses primitive type string",
			want:     false,
		},
		{
			name:     "wrong category",
			category: CategoryMissingIsValid,
			message:  "struct field pkg.Foo.Bar uses primitive type string",
			want:     false,
		},
		{
			name:     "missing-isvalid match",
			category: CategoryMissingIsValid,
			message:  "named type pkg.MyType has no IsValid() method",
			want:     true,
		},
		{
			name:     "missing-stringer match",
			category: CategoryMissingStringer,
			message:  "named type pkg.MyType has no String() method",
			want:     true,
		},
		{
			name:     "missing-constructor match",
			category: CategoryMissingConstructor,
			message:  "exported struct pkg.Config has no NewConfig() constructor",
			want:     true,
		},
		{
			name:     "wrong-constructor-sig match",
			category: CategoryWrongConstructorSig,
			message:  "constructor NewFoo() for pkg.Foo returns Bar, expected Foo",
			want:     true,
		},
		{
			name:     "wrong-isvalid-sig match",
			category: CategoryWrongIsValidSig,
			message:  "named type pkg.BadValid has IsValid() but wrong signature (want func() (bool, []error))",
			want:     true,
		},
		{
			name:     "wrong-stringer-sig match",
			category: CategoryWrongStringerSig,
			message:  "named type pkg.BadStr has String() but wrong signature (want func() string)",
			want:     true,
		},
		{
			name:     "missing-func-options match",
			category: CategoryMissingFuncOptions,
			message:  "constructor NewBig() for pkg.Big has 5 non-option parameters; consider using functional options",
			want:     true,
		},
		{
			name:     "missing-immutability match",
			category: CategoryMissingImmutability,
			message:  "struct pkg.Svc has NewSvc() constructor but field Addr is exported",
			want:     true,
		},
		{
			name:     "missing-struct-isvalid match",
			category: CategoryMissingStructIsValid,
			message:  "struct pkg.Svc has constructor but no IsValid() method",
			want:     true,
		},
		{
			name:     "wrong-struct-isvalid-sig match",
			category: CategoryWrongStructIsValidSig,
			message:  "struct pkg.BadSvc has IsValid() but wrong signature (want func() (bool, []error))",
			want:     true,
		},
		{
			name:     "nil baseline",
			category: CategoryPrimitive,
			message:  "anything",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var target *BaselineConfig
			if tt.name != "nil baseline" {
				target = bl
			}
			got := target.Contains(tt.category, tt.message)
			if got != tt.want {
				t.Errorf("Contains(%q, %q) = %v, want %v", tt.category, tt.message, got, tt.want)
			}
		})
	}
}

func TestWriteBaseline(t *testing.T) {
	t.Parallel()

	t.Run("writes sorted TOML with header", func(t *testing.T) {
		t.Parallel()
		outPath := filepath.Join(t.TempDir(), "baseline.toml")
		findings := map[string][]BaselineFinding{
			CategoryPrimitive: {
				{ID: "z-id", Message: "struct field pkg.Foo.Baz uses primitive type int"},
				{ID: "a-id", Message: "struct field pkg.Foo.Bar uses primitive type string"},
			},
			CategoryMissingConstructor: {
				{ID: "m-id", Message: "exported struct pkg.Config has no NewConfig() constructor"},
			},
		}

		if err := WriteBaseline(outPath, findings); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("reading output: %v", err)
		}
		content := string(data)

		// Verify SPDX header.
		if !containsStr(content, "SPDX-License-Identifier: MPL-2.0") {
			t.Error("missing SPDX header")
		}

		// Verify total count.
		if !containsStr(content, "Total: 3 findings") {
			t.Error("missing or wrong total count")
		}

		// Verify v2 schema uses entries.
		if !containsStr(content, "entries = [") {
			t.Error("expected entries array in v2 baseline")
		}

		// Verify sorting by ID — a-id should come before z-id.
		aIDIdx := indexOfStr(content, `id = "a-id"`)
		zIDIdx := indexOfStr(content, `id = "z-id"`)
		if aIDIdx < 0 || zIDIdx < 0 || aIDIdx > zIDIdx {
			t.Error("baseline entries not sorted by ID")
		}
	})

	t.Run("empty categories omitted", func(t *testing.T) {
		t.Parallel()
		outPath := filepath.Join(t.TempDir(), "baseline.toml")
		findings := map[string][]BaselineFinding{
			CategoryMissingIsValid: {
				{Message: "named type pkg.MyType has no IsValid() method"},
			},
		}

		if err := WriteBaseline(outPath, findings); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("reading output: %v", err)
		}
		content := string(data)

		if containsStr(content, "[primitive]") {
			t.Error("empty [primitive] section should be omitted")
		}
		if !containsStr(content, "[missing-isvalid]") {
			t.Error("non-empty [missing-isvalid] section should be present")
		}
	})
}

func TestBaselineRoundTrip(t *testing.T) {
	t.Parallel()

	findings := map[string][]BaselineFinding{
		CategoryPrimitive: {
			{Message: "struct field pkg.Foo.Bar uses primitive type string"},
			{Message: `parameter "name" of pkg.Func uses primitive type string`},
		},
		CategoryMissingIsValid: {
			{Message: "named type pkg.MyType has no IsValid() method"},
		},
		CategoryMissingStringer: {
			{Message: "named type pkg.MyType has no String() method"},
		},
		CategoryMissingConstructor: {
			{Message: "exported struct pkg.Config has no NewConfig() constructor"},
		},
		CategoryWrongConstructorSig: {
			{Message: "constructor NewFoo() for pkg.Foo returns Bar, expected Foo"},
		},
		CategoryWrongIsValidSig: {
			{Message: "named type pkg.BadValid has IsValid() but wrong signature (want func() (bool, []error))"},
		},
		CategoryWrongStringerSig: {
			{Message: "named type pkg.BadStr has String() but wrong signature (want func() string)"},
		},
		CategoryMissingFuncOptions: {
			{Message: "constructor NewBig() for pkg.Big has 5 non-option parameters; consider using functional options"},
		},
		CategoryMissingImmutability: {
			{Message: "struct pkg.Svc has NewSvc() constructor but field Addr is exported"},
		},
		CategoryMissingStructIsValid: {
			{Message: "struct pkg.Svc has constructor but no IsValid() method"},
		},
		CategoryWrongStructIsValidSig: {
			{Message: "struct pkg.BadSvc has IsValid() but wrong signature (want func() (bool, []error))"},
		},
	}

	outPath := filepath.Join(t.TempDir(), "baseline.toml")
	if err := WriteBaseline(outPath, findings); err != nil {
		t.Fatalf("write error: %v", err)
	}

	bl, err := loadBaseline(outPath)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	// Verify all original findings are present.
	for cat, entries := range findings {
		for _, entry := range entries {
			if !bl.Contains(cat, entry.Message) {
				t.Errorf("round-trip lost: category=%q, message=%q", cat, entry.Message)
			}
			fallbackID := FallbackFindingID(cat, entry.Message)
			if !bl.ContainsFinding(cat, fallbackID, "") {
				t.Errorf("round-trip lost id match: category=%q, id=%q", cat, fallbackID)
			}
		}
	}

	// Verify count matches.
	total := 0
	for _, entries := range findings {
		total += len(entries)
	}
	if bl.Count() != total {
		t.Errorf("count mismatch: got %d, want %d", bl.Count(), total)
	}
}

func TestQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "hello", `"hello"`},
		{"with quotes", `say "hi"`, `"say \"hi\""`},
		{"with backslash", `path\to\file`, `"path\\to\\file"`},
		{"with newline", "line1\nline2", `"line1\nline2"`},
		{"with carriage return", "line1\rline2", `"line1\rline2"`},
		{"with tab", "col1\tcol2", `"col1\tcol2"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := quote(tt.input)
			if got != tt.want {
				t.Errorf("quote(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

// TestBaselineCategoryCompleteness verifies that every baselined diagnostic
// category constant has a corresponding entry in buildLookup() and Count().
// This catches silent drift when new categories are added to the analyzer but
// not wired into the baseline infrastructure.
func TestBaselineCategoryCompleteness(t *testing.T) {
	t.Parallel()

	// Authoritative list of categories that MUST be present in baseline
	// infrastructure. StaleException and UnknownDirective are intentionally
	// excluded — they are never baselined.
	baselinedCategories := []string{
		CategoryPrimitive,
		CategoryMissingIsValid,
		CategoryMissingStringer,
		CategoryMissingConstructor,
		CategoryWrongConstructorSig,
		CategoryWrongIsValidSig,
		CategoryWrongStringerSig,
		CategoryMissingFuncOptions,
		CategoryMissingImmutability,
		CategoryMissingStructIsValid,
		CategoryWrongStructIsValidSig,
	}

	// Verify buildLookup() initializes an entry for each category.
	bl := emptyBaseline()
	for _, cat := range baselinedCategories {
		if _, ok := bl.lookupByID[cat]; !ok {
			t.Errorf("buildLookup() missing ID entry for category %q", cat)
		}
		if _, ok := bl.lookupByMessage[cat]; !ok {
			t.Errorf("buildLookup() missing message entry for category %q", cat)
		}
	}

	// Verify no extra entries in lookup beyond our list + zero unexpected.
	expectedSet := make(map[string]bool, len(baselinedCategories))
	for _, cat := range baselinedCategories {
		expectedSet[cat] = true
	}
	for cat := range bl.lookupByID {
		if !expectedSet[cat] {
			t.Errorf("buildLookup() ID map has unexpected category %q not in baselinedCategories", cat)
		}
	}
	for cat := range bl.lookupByMessage {
		if !expectedSet[cat] {
			t.Errorf("buildLookup() message map has unexpected category %q not in baselinedCategories", cat)
		}
	}

	// Verify WriteBaseline handles every category by checking that
	// a baseline with one entry per category round-trips correctly.
	findings := make(map[string][]BaselineFinding, len(baselinedCategories))
	for _, cat := range baselinedCategories {
		findings[cat] = []BaselineFinding{{Message: "test message for " + cat}}
	}

	outPath := writeTempFile(t, "completeness.toml", "")
	if err := WriteBaseline(outPath, findings); err != nil {
		t.Fatalf("WriteBaseline error: %v", err)
	}
	loaded, err := loadBaseline(outPath)
	if err != nil {
		t.Fatalf("loadBaseline error: %v", err)
	}

	for _, cat := range baselinedCategories {
		msg := "test message for " + cat
		if !loaded.Contains(cat, msg) {
			t.Errorf("WriteBaseline/loadBaseline round-trip failed for category %q", cat)
		}
	}

	// Verify Count includes all categories.
	if loaded.Count() != len(baselinedCategories) {
		t.Errorf("Count() = %d, want %d (one entry per category)", loaded.Count(), len(baselinedCategories))
	}
}

// writeTempFile creates a temporary file with the given content and returns its path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	return path
}

func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}

func indexOfStr(s, substr string) int {
	return strings.Index(s, substr)
}
