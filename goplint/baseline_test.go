// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadBaseline(t *testing.T) {
	t.Parallel()

	t.Run("empty path returns empty baseline", func(t *testing.T) {
		t.Parallel()
		bl, err := loadBaseline("", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bl.Count() != 0 {
			t.Errorf("expected 0 entries, got %d", bl.Count())
		}
		// Empty baseline should match nothing.
		if bl.ContainsFinding(CategoryPrimitive, "anything", "") {
			t.Error("empty baseline should not match anything")
		}
	})

	t.Run("nonexistent file returns empty baseline", func(t *testing.T) {
		t.Parallel()
		bl, err := loadBaseline("/nonexistent/path/baseline.toml", false)
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
entries = [
    { id = "primitive-1", message = "struct field pkg.Foo.Bar uses primitive type string" },
    { id = "primitive-2", message = "parameter \"name\" of pkg.Func uses primitive type string" },
]

[missing-validate]
entries = [
    { id = "missing-validate-1", message = "named type pkg.MyType has no Validate() method" },
]

[missing-constructor]
entries = [
    { id = "missing-constructor-1", message = "exported struct pkg.Config has no NewConfig() constructor" },
]
`
		path := writeTempFile(t, "baseline.toml", content)
		bl, err := loadBaseline(path, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if bl.Count() != 4 {
			t.Errorf("expected 4 entries, got %d", bl.Count())
		}
		if !bl.ContainsFinding(CategoryPrimitive, "primitive-1", "") {
			t.Error("expected primitive-1 to be present")
		}
		if !bl.ContainsFinding(CategoryMissingValidate, "missing-validate-1", "") {
			t.Error("expected missing-validate-1 to be present")
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
		bl, err := loadBaseline(path, false)
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

	t.Run("legacy messages key rejected", func(t *testing.T) {
		t.Parallel()
		content := `
[primitive]
messages = [
    "struct field pkg.Foo.Bar uses primitive type string",
]
`
		path := writeTempFile(t, "legacy-messages.toml", content)
		_, err := loadBaseline(path, false)
		if err == nil {
			t.Fatal("expected error for legacy messages baseline schema")
		}
	})

	t.Run("empty id in entry returns error", func(t *testing.T) {
		t.Parallel()
		content := `
[primitive]
entries = [
    { id = "", message = "x" },
]
`
		path := writeTempFile(t, "empty-id.toml", content)
		_, err := loadBaseline(path, false)
		if err == nil {
			t.Fatal("expected error for empty baseline entry ID")
		}
	})

	t.Run("empty message in entry returns error", func(t *testing.T) {
		t.Parallel()
		content := `
[primitive]
entries = [
    { id = "id-1", message = "" },
]
`
		path := writeTempFile(t, "empty-message.toml", content)
		_, err := loadBaseline(path, false)
		if err == nil {
			t.Fatal("expected error for empty baseline entry message")
		}
	})

	t.Run("invalid TOML returns error", func(t *testing.T) {
		t.Parallel()
		path := writeTempFile(t, "bad.toml", "[[invalid toml")
		_, err := loadBaseline(path, false)
		if err == nil {
			t.Fatal("expected error for invalid TOML")
		}
	})

	t.Run("unknown top-level baseline category returns error", func(t *testing.T) {
		t.Parallel()
		content := `
[unknown-category]
entries = [
    { id = "id-1", message = "x" },
]
`
		path := writeTempFile(t, "unknown-category.toml", content)
		_, err := loadBaseline(path, false)
		if err == nil {
			t.Fatal("expected error for unknown top-level baseline category")
		}
	})

	t.Run("unknown field in entry returns error", func(t *testing.T) {
		t.Parallel()
		content := `
[primitive]
entries = [
    { id = "id-1", message = "x", extra = "unexpected" },
]
`
		path := writeTempFile(t, "unknown-entry-field.toml", content)
		_, err := loadBaseline(path, false)
		if err == nil {
			t.Fatal("expected error for unknown baseline entry field")
		}
	})

	t.Run("nonexistent file returns error when strict", func(t *testing.T) {
		t.Parallel()
		_, err := loadBaseline("/nonexistent/path/baseline.toml", true)
		if err == nil {
			t.Fatal("expected error for missing baseline in strict mode")
		}
	})
}

func TestLoadBaselineCached_ReusesConfig(t *testing.T) {
	t.Parallel()

	content := `
[primitive]
entries = [
    { id = "id-1", message = "struct field pkg.Foo.Bar uses primitive type string" },
]
`
	path := writeTempFile(t, "cached-baseline.toml", content)
	state := &flagState{}
	resetFlagStateDefaults(state)

	first, err := loadBaselineCached(state, path, false)
	if err != nil {
		t.Fatalf("first loadBaselineCached error: %v", err)
	}
	second, err := loadBaselineCached(state, path, false)
	if err != nil {
		t.Fatalf("second loadBaselineCached error: %v", err)
	}

	if first != second {
		t.Fatal("expected cached baseline pointer reuse across loads")
	}
	if !second.ContainsFinding(CategoryPrimitive, "id-1", "") {
		t.Fatal("expected cached baseline to retain loaded entries")
	}
}

func TestLoadBaselineCached_StrictModeCacheIsolation(t *testing.T) {
	t.Parallel()

	content := `
[primitive]
entries = [
    { id = "id-1", message = "struct field pkg.Foo.Bar uses primitive type string" },
]
`
	path := writeTempFile(t, "strict-mode-baseline.toml", content)
	state := &flagState{}
	resetFlagStateDefaults(state)

	nonStrict, err := loadBaselineCached(state, path, false)
	if err != nil {
		t.Fatalf("non-strict load error: %v", err)
	}
	strict, err := loadBaselineCached(state, path, true)
	if err != nil {
		t.Fatalf("strict load error: %v", err)
	}
	if nonStrict == strict {
		t.Fatal("expected strict and non-strict cache keys to use distinct cache entries")
	}

	missingPath := filepath.Join(t.TempDir(), "missing-baseline.toml")
	nonStrictMissing, err := loadBaselineCached(state, missingPath, false)
	if err != nil {
		t.Fatalf("non-strict missing baseline should not error: %v", err)
	}
	if nonStrictMissing == nil {
		t.Fatal("expected non-strict missing baseline load to return empty baseline")
	}

	if _, err := loadBaselineCached(state, missingPath, true); err == nil {
		t.Fatal("expected strict missing baseline load to return error")
	}
}

func TestBaselineContainsFinding(t *testing.T) {
	t.Parallel()

	content := `
[primitive]
entries = [
    { id = "primitive-1", message = "struct field pkg.Foo.Bar uses primitive type string" },
    { id = "primitive-2", message = "return value of pkg.Func uses primitive type int" },
]

[missing-validate]
entries = [
    { id = "missing-validate-1", message = "named type pkg.MyType has no Validate() method" },
]

[missing-stringer]
entries = [
    { id = "missing-stringer-1", message = "named type pkg.MyType has no String() method" },
]

[missing-constructor]
entries = [
    { id = "missing-constructor-1", message = "exported struct pkg.Config has no NewConfig() constructor" },
]

[wrong-constructor-sig]
entries = [
    { id = "wrong-constructor-sig-1", message = "constructor NewFoo() for pkg.Foo returns Bar, expected Foo" },
]

[wrong-validate-sig]
entries = [
    { id = "wrong-validate-sig-1", message = "named type pkg.BadValid has Validate() but wrong signature (want func() error)" },
]

[wrong-stringer-sig]
entries = [
    { id = "wrong-stringer-sig-1", message = "named type pkg.BadStr has String() but wrong signature (want func() string)" },
]

[missing-func-options]
entries = [
    { id = "missing-func-options-1", message = "constructor NewBig() for pkg.Big has 5 non-option parameters; consider using functional options" },
]

[missing-immutability]
entries = [
    { id = "missing-immutability-1", message = "struct pkg.Svc has NewSvc() constructor but field Addr is exported" },
]

[missing-struct-validate]
entries = [
    { id = "missing-struct-validate-1", message = "struct pkg.Svc has constructor but no Validate() method" },
]

[wrong-struct-validate-sig]
entries = [
    { id = "wrong-struct-validate-sig-1", message = "struct pkg.BadSvc has Validate() but wrong signature (want func() error)" },
]
`
	path := writeTempFile(t, "baseline.toml", content)
	bl, err := loadBaseline(path, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		category string
		id       string
		want     bool
	}{
		{
			name:     "primitive match",
			category: CategoryPrimitive,
			id:       "primitive-1",
			want:     true,
		},
		{
			name:     "primitive no match",
			category: CategoryPrimitive,
			id:       "primitive-unknown",
			want:     false,
		},
		{
			name:     "wrong category",
			category: CategoryMissingValidate,
			id:       "primitive-1",
			want:     false,
		},
		{
			name:     "missing-validate match",
			category: CategoryMissingValidate,
			id:       "missing-validate-1",
			want:     true,
		},
		{
			name:     "missing-stringer match",
			category: CategoryMissingStringer,
			id:       "missing-stringer-1",
			want:     true,
		},
		{
			name:     "missing-constructor match",
			category: CategoryMissingConstructor,
			id:       "missing-constructor-1",
			want:     true,
		},
		{
			name:     "wrong-constructor-sig match",
			category: CategoryWrongConstructorSig,
			id:       "wrong-constructor-sig-1",
			want:     true,
		},
		{
			name:     "wrong-validate-sig match",
			category: CategoryWrongValidateSig,
			id:       "wrong-validate-sig-1",
			want:     true,
		},
		{
			name:     "wrong-stringer-sig match",
			category: CategoryWrongStringerSig,
			id:       "wrong-stringer-sig-1",
			want:     true,
		},
		{
			name:     "missing-func-options match",
			category: CategoryMissingFuncOptions,
			id:       "missing-func-options-1",
			want:     true,
		},
		{
			name:     "missing-immutability match",
			category: CategoryMissingImmutability,
			id:       "missing-immutability-1",
			want:     true,
		},
		{
			name:     "missing-struct-validate match",
			category: CategoryMissingStructValidate,
			id:       "missing-struct-validate-1",
			want:     true,
		},
		{
			name:     "wrong-struct-validate-sig match",
			category: CategoryWrongStructValidateSig,
			id:       "wrong-struct-validate-sig-1",
			want:     true,
		},
		{
			name:     "empty ID never matches",
			category: CategoryPrimitive,
			id:       "",
			want:     false,
		},
		{
			name:     "nil baseline",
			category: CategoryPrimitive,
			id:       "anything",
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
			got := target.ContainsFinding(tt.category, tt.id, "ignored")
			if got != tt.want {
				t.Errorf("ContainsFinding(%q, %q) = %v, want %v", tt.category, tt.id, got, tt.want)
			}
		})
	}
}

func TestContainsFinding_StrictIDNoMessageFallback(t *testing.T) {
	t.Parallel()

	content := `
[primitive]
entries = [
    { id = "primitive-1", message = "struct field pkg.Foo.Bar uses primitive type string" },
]
`
	path := writeTempFile(t, "baseline.toml", content)
	bl, err := loadBaseline(path, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bl.ContainsFinding(CategoryPrimitive, "non-matching-id", "struct field pkg.Foo.Bar uses primitive type string") {
		t.Fatal("expected non-matching ID to fail without message fallback")
	}
	if bl.ContainsFinding(CategoryPrimitive, "", "struct field pkg.Foo.Bar uses primitive type string") {
		t.Fatal("expected empty ID to fail without message fallback")
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
			CategoryMissingValidate: {
				{ID: "mv-1", Message: "named type pkg.MyType has no Validate() method"},
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
		if !containsStr(content, "[missing-validate]") {
			t.Error("non-empty [missing-validate] section should be present")
		}
	})

	t.Run("invalid entries are dropped", func(t *testing.T) {
		t.Parallel()
		outPath := filepath.Join(t.TempDir(), "baseline.toml")
		findings := map[string][]BaselineFinding{
			CategoryPrimitive: {
				{ID: "", Message: "struct field pkg.Foo.Bar uses primitive type string"},
				{ID: "id-2", Message: ""},
				{ID: "id-3", Message: "struct field pkg.Foo.Baz uses primitive type int"},
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
		if containsStr(content, `id = ""`) {
			t.Error("entries with empty IDs should be dropped")
		}
		if !containsStr(content, `id = "id-3"`) {
			t.Error("expected valid entry to be retained")
		}
		if !containsStr(content, "Total: 1 findings") {
			t.Error("expected only one valid finding to be counted")
		}
	})
}

func TestBaselineRoundTrip(t *testing.T) {
	t.Parallel()

	findings := map[string][]BaselineFinding{
		CategoryPrimitive: {
			{ID: "primitive-1", Message: "struct field pkg.Foo.Bar uses primitive type string"},
			{ID: "primitive-2", Message: `parameter "name" of pkg.Func uses primitive type string`},
		},
		CategoryMissingValidate: {
			{ID: "missing-validate-1", Message: "named type pkg.MyType has no Validate() method"},
		},
		CategoryMissingStringer: {
			{ID: "missing-stringer-1", Message: "named type pkg.MyType has no String() method"},
		},
		CategoryMissingConstructor: {
			{ID: "missing-constructor-1", Message: "exported struct pkg.Config has no NewConfig() constructor"},
		},
		CategoryWrongConstructorSig: {
			{ID: "wrong-constructor-sig-1", Message: "constructor NewFoo() for pkg.Foo returns Bar, expected Foo"},
		},
		CategoryWrongValidateSig: {
			{ID: "wrong-validate-sig-1", Message: "named type pkg.BadValid has Validate() but wrong signature (want func() error)"},
		},
		CategoryWrongStringerSig: {
			{ID: "wrong-stringer-sig-1", Message: "named type pkg.BadStr has String() but wrong signature (want func() string)"},
		},
		CategoryMissingFuncOptions: {
			{ID: "missing-func-options-1", Message: "constructor NewBig() for pkg.Big has 5 non-option parameters; consider using functional options"},
		},
		CategoryMissingImmutability: {
			{ID: "missing-immutability-1", Message: "struct pkg.Svc has NewSvc() constructor but field Addr is exported"},
		},
		CategoryMissingStructValidate: {
			{ID: "missing-struct-validate-1", Message: "struct pkg.Svc has constructor but no Validate() method"},
		},
		CategoryWrongStructValidateSig: {
			{ID: "wrong-struct-validate-sig-1", Message: "struct pkg.BadSvc has Validate() but wrong signature (want func() error)"},
		},
		CategoryNonZeroValueField: {
			{ID: "nonzero-1", Message: "struct field pkg.Foo.Bar uses nonzero type X as value; use *X for optional fields"},
		},
	}

	outPath := filepath.Join(t.TempDir(), "baseline.toml")
	if err := WriteBaseline(outPath, findings); err != nil {
		t.Fatalf("write error: %v", err)
	}

	bl, err := loadBaseline(outPath, false)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	// Verify all original findings are present.
	for cat, entries := range findings {
		for _, entry := range entries {
			if !bl.ContainsFinding(cat, entry.ID, entry.Message) {
				t.Errorf("round-trip lost: category=%q, id=%q", cat, entry.ID)
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

	// Authoritative list comes from the canonical category registry.
	baselinedCategories := BaselinedCategoryNames()

	// Verify buildLookup() initializes an entry for each category.
	bl := emptyBaseline()
	baselineFields := bl.categoriesByName()
	for _, cat := range baselinedCategories {
		if _, ok := baselineFields[cat]; !ok {
			t.Errorf("BaselineConfig missing TOML field for category %q", cat)
		}
		if _, ok := bl.lookupByID[cat]; !ok {
			t.Errorf("buildLookup() missing ID entry for category %q", cat)
		}
	}
	for cat := range baselineFields {
		if !slices.Contains(baselinedCategories, cat) {
			t.Errorf("BaselineConfig has unexpected TOML category %q", cat)
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

	// Verify WriteBaseline handles every category by checking that
	// a baseline with one entry per category round-trips correctly.
	findings := make(map[string][]BaselineFinding, len(baselinedCategories))
	for _, cat := range baselinedCategories {
		findings[cat] = []BaselineFinding{{ID: "id-" + cat, Message: "test message for " + cat}}
	}

	outPath := writeTempFile(t, "completeness.toml", "")
	if err := WriteBaseline(outPath, findings); err != nil {
		t.Fatalf("WriteBaseline error: %v", err)
	}
	loaded, err := loadBaseline(outPath, false)
	if err != nil {
		t.Fatalf("loadBaseline error: %v", err)
	}

	for _, cat := range baselinedCategories {
		id := "id-" + cat
		if !loaded.ContainsFinding(cat, id, "test message for "+cat) {
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
