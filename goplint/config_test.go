// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	t.Run("empty path returns empty config", func(t *testing.T) {
		t.Parallel()
		cfg, err := loadConfig("", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Exceptions) != 0 {
			t.Errorf("expected 0 exceptions, got %d", len(cfg.Exceptions))
		}
	})

	t.Run("nonexistent file returns empty config", func(t *testing.T) {
		t.Parallel()
		cfg, err := loadConfig(filepath.Join(t.TempDir(), "nonexistent.toml"), false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Exceptions) != 0 {
			t.Errorf("expected 0 exceptions, got %d", len(cfg.Exceptions))
		}
	})

	t.Run("valid TOML parses correctly", func(t *testing.T) {
		t.Parallel()
		content := `
[settings]
skip_types = ["bool", "error"]
exclude_paths = ["specs/"]

[[exceptions]]
pattern = "Foo.Bar"
reason = "test reason"

[[exceptions]]
pattern = "*.Baz"
reason = "wildcard test"
`
		path := filepath.Join(t.TempDir(), "test.toml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		cfg, err := loadConfig(path, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Settings.SkipTypes) != 2 {
			t.Errorf("expected 2 skip_types, got %d", len(cfg.Settings.SkipTypes))
		}
		if len(cfg.Settings.ExcludePaths) != 1 {
			t.Errorf("expected 1 exclude_paths, got %d", len(cfg.Settings.ExcludePaths))
		}
		if len(cfg.Exceptions) != 2 {
			t.Errorf("expected 2 exceptions, got %d", len(cfg.Exceptions))
		}
		if cfg.Exceptions[0].Pattern != "Foo.Bar" {
			t.Errorf("expected pattern 'Foo.Bar', got %q", cfg.Exceptions[0].Pattern)
		}
		if cfg.Exceptions[0].Reason != "test reason" {
			t.Errorf("expected reason 'test reason', got %q", cfg.Exceptions[0].Reason)
		}
	})

	t.Run("invalid TOML returns error", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "bad.toml")
		if err := os.WriteFile(path, []byte("{{{invalid toml"), 0o644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		cfg, err := loadConfig(path, false)
		if err == nil {
			t.Fatal("expected error for invalid TOML, got nil")
		}
		if cfg != nil {
			t.Errorf("expected nil config on error, got %+v", cfg)
		}
	})

	t.Run("unknown top-level key returns error", func(t *testing.T) {
		t.Parallel()
		content := `
[settings]
skip_types = ["bool"]

unknown_key = "oops"
`
		path := filepath.Join(t.TempDir(), "unknown-top.toml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		_, err := loadConfig(path, false)
		if err == nil {
			t.Fatal("expected error for unknown top-level key")
		}
	})

	t.Run("unknown exception field returns error", func(t *testing.T) {
		t.Parallel()
		content := `
[[exceptions]]
pattern = "Foo.Bar"
reason = "test"
unexpected = "nope"
`
		path := filepath.Join(t.TempDir(), "unknown-exception-field.toml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		_, err := loadConfig(path, false)
		if err == nil {
			t.Fatal("expected error for unknown exception field")
		}
	})

	t.Run("invalid exception glob returns error", func(t *testing.T) {
		t.Parallel()
		content := `
[[exceptions]]
pattern = "pkg.[bad.Type"
reason = "invalid glob"
`
		path := filepath.Join(t.TempDir(), "invalid-exception-glob.toml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		_, err := loadConfig(path, false)
		if err == nil {
			t.Fatal("expected error for invalid exception glob")
		}
	})

	t.Run("nonexistent file returns error when strict", func(t *testing.T) {
		t.Parallel()
		_, err := loadConfig(filepath.Join(t.TempDir(), "nonexistent.toml"), true)
		if err == nil {
			t.Fatal("expected error for missing config in strict mode")
		}
	})
}

func TestLoadConfigCached_CloneIsolation(t *testing.T) {
	t.Parallel()

	content := `
[settings]
skip_types = ["bool"]
exclude_paths = ["specs/"]
include_packages = ["github.com/invowk/invowk"]

[[exceptions]]
pattern = "pkg.Type.Field"
reason = "test"
`
	path := writeTempFile(t, "cached-config.toml", content)
	state := &flagState{}
	resetFlagStateDefaults(state)

	first, err := loadConfigCached(state, path, false)
	if err != nil {
		t.Fatalf("first loadConfigCached error: %v", err)
	}
	second, err := loadConfigCached(state, path, false)
	if err != nil {
		t.Fatalf("second loadConfigCached error: %v", err)
	}

	if first == second {
		t.Fatal("expected per-run config clone, got shared pointer")
	}

	first.Settings.SkipTypes[0] = "mutated-skip-type"
	first.Settings.ExcludePaths[0] = "mutated-path"
	first.Settings.IncludePackages[0] = "mutated-pkg"
	first.Exceptions[0].Pattern = "mutated.pattern"
	first.matchCounts[0] = 42

	if second.Settings.SkipTypes[0] != "bool" {
		t.Fatalf("skip_types leaked across clones: got %q", second.Settings.SkipTypes[0])
	}
	if second.Settings.ExcludePaths[0] != "specs/" {
		t.Fatalf("exclude_paths leaked across clones: got %q", second.Settings.ExcludePaths[0])
	}
	if second.Settings.IncludePackages[0] != "github.com/invowk/invowk" {
		t.Fatalf("include_packages leaked across clones: got %q", second.Settings.IncludePackages[0])
	}
	if second.Exceptions[0].Pattern != "pkg.Type.Field" {
		t.Fatalf("exceptions leaked across clones: got %q", second.Exceptions[0].Pattern)
	}
	if second.matchCounts[0] != 0 {
		t.Fatalf("matchCounts leaked across clones: got %d", second.matchCounts[0])
	}
}

func TestConfigTemplate(t *testing.T) {
	t.Parallel()

	t.Run("nil config returns empty template", func(t *testing.T) {
		t.Parallel()

		got := configTemplate(nil)
		if got == nil {
			t.Fatal("configTemplate(nil) returned nil")
		}
		if len(got.Exceptions) != 0 {
			t.Fatalf("configTemplate(nil) exceptions len = %d, want 0", len(got.Exceptions))
		}
		if len(got.Settings.SkipTypes) != 0 || len(got.Settings.ExcludePaths) != 0 || len(got.Settings.IncludePackages) != 0 {
			t.Fatalf("configTemplate(nil) settings should be empty: %+v", got.Settings)
		}
	})

	t.Run("returns clone", func(t *testing.T) {
		t.Parallel()

		orig := &ExceptionConfig{
			Settings: Settings{
				SkipTypes:       []string{"bool"},
				ExcludePaths:    []string{"specs/"},
				IncludePackages: []string{"github.com/invowk/invowk"},
			},
			Exceptions: []Exception{
				{Pattern: "pkg.Type.Field", Reason: "test"},
			},
		}
		got := configTemplate(orig)
		got.Settings.SkipTypes[0] = "mutated"
		got.Settings.ExcludePaths[0] = "mutated"
		got.Settings.IncludePackages[0] = "mutated"
		got.Exceptions[0].Pattern = "mutated"

		if orig.Settings.SkipTypes[0] != "bool" {
			t.Fatalf("SkipTypes was mutated in original: %q", orig.Settings.SkipTypes[0])
		}
		if orig.Settings.ExcludePaths[0] != "specs/" {
			t.Fatalf("ExcludePaths was mutated in original: %q", orig.Settings.ExcludePaths[0])
		}
		if orig.Settings.IncludePackages[0] != "github.com/invowk/invowk" {
			t.Fatalf("IncludePackages was mutated in original: %q", orig.Settings.IncludePackages[0])
		}
		if orig.Exceptions[0].Pattern != "pkg.Type.Field" {
			t.Fatalf("Exceptions was mutated in original: %q", orig.Exceptions[0].Pattern)
		}
	})
}

func TestShouldAnalyzePackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		includePackages []string
		pkgPath         string
		want            bool
	}{
		{
			name:            "empty list allows all",
			includePackages: nil,
			pkgPath:         "fmt",
			want:            true,
		},
		{
			name:            "empty slice allows all",
			includePackages: []string{},
			pkgPath:         "github.com/other/pkg",
			want:            true,
		},
		{
			name:            "exact match",
			includePackages: []string{"github.com/invowk/invowk"},
			pkgPath:         "github.com/invowk/invowk",
			want:            true,
		},
		{
			name:            "prefix match subpackage",
			includePackages: []string{"github.com/invowk/invowk"},
			pkgPath:         "github.com/invowk/invowk/internal/config",
			want:            true,
		},
		{
			name:            "no match stdlib",
			includePackages: []string{"github.com/invowk/invowk"},
			pkgPath:         "fmt",
			want:            false,
		},
		{
			name:            "no match third-party",
			includePackages: []string{"github.com/invowk/invowk"},
			pkgPath:         "github.com/spf13/cobra",
			want:            false,
		},
		{
			name:            "multiple prefixes first matches",
			includePackages: []string{"github.com/invowk/invowk", "github.com/other/pkg"},
			pkgPath:         "github.com/invowk/invowk/pkg/types",
			want:            true,
		},
		{
			name:            "multiple prefixes second matches",
			includePackages: []string{"github.com/invowk/invowk", "github.com/other/pkg"},
			pkgPath:         "github.com/other/pkg/sub",
			want:            true,
		},
		{
			name:            "multiple prefixes none match",
			includePackages: []string{"github.com/invowk/invowk", "github.com/other/pkg"},
			pkgPath:         "github.com/unrelated/lib",
			want:            false,
		},
		{
			name:            "partial prefix does not match",
			includePackages: []string{"github.com/invowk/invowk"},
			pkgPath:         "github.com/invowk/invowk-other",
			want:            false,
		},
		{
			name:            "empty prefixes are ignored",
			includePackages: []string{""},
			pkgPath:         "github.com/invowk/invowk",
			want:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &ExceptionConfig{Settings: Settings{IncludePackages: tt.includePackages}}
			got := cfg.ShouldAnalyzePackage(tt.pkgPath)
			if got != tt.want {
				t.Errorf("ShouldAnalyzePackage(%q) = %v, want %v", tt.pkgPath, got, tt.want)
			}
		})
	}
}

func TestLoadConfig_IncludePackages(t *testing.T) {
	t.Parallel()

	content := `
[settings]
skip_types = ["bool"]
include_packages = ["github.com/invowk/invowk", "github.com/other/pkg"]
`
	path := filepath.Join(t.TempDir(), "include-packages.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := loadConfig(path, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Settings.IncludePackages) != 2 {
		t.Fatalf("expected 2 include_packages, got %d", len(cfg.Settings.IncludePackages))
	}
	if cfg.Settings.IncludePackages[0] != "github.com/invowk/invowk" {
		t.Errorf("expected first prefix %q, got %q", "github.com/invowk/invowk", cfg.Settings.IncludePackages[0])
	}
	if cfg.Settings.IncludePackages[1] != "github.com/other/pkg" {
		t.Errorf("expected second prefix %q, got %q", "github.com/other/pkg", cfg.Settings.IncludePackages[1])
	}
}

func TestLoadConfig_IncludePackagesRejectsEmptyPrefix(t *testing.T) {
	t.Parallel()

	content := `
[settings]
include_packages = ["github.com/invowk/invowk", ""]
`
	path := filepath.Join(t.TempDir(), "include-packages-invalid.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := loadConfig(path, false)
	if err == nil {
		t.Fatal("expected empty include_packages prefix to fail")
	}
	if !strings.Contains(err.Error(), "settings.include_packages[1]: empty package prefix") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMatchPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{name: "exact 2-segment", pattern: "Foo.Bar", input: "Foo.Bar", want: true},
		{name: "exact 3-segment", pattern: "pkg.Foo.Bar", input: "pkg.Foo.Bar", want: true},
		{name: "wildcard first", pattern: "*.Bar", input: "Foo.Bar", want: true},
		{name: "wildcard last", pattern: "Foo.*", input: "Foo.Bar", want: true},
		{name: "wildcard middle", pattern: "pkg.*.name", input: "pkg.Type.name", want: true},
		{name: "all wildcards", pattern: "*.*.*", input: "a.b.c", want: true},
		{name: "no match", pattern: "Foo.Bar", input: "Foo.Baz", want: false},
		{name: "segment count mismatch short", pattern: "Foo", input: "Foo.Bar", want: false},
		{name: "segment count mismatch long", pattern: "Foo.Bar.Baz", input: "Foo.Bar", want: false},
		{name: "case sensitive", pattern: "foo.bar", input: "Foo.Bar", want: false},
		{name: "empty vs empty", pattern: "", input: "", want: true},
		{name: "empty vs non-empty", pattern: "", input: "Foo", want: false},
		{name: "single segment exact", pattern: "Foo", input: "Foo", want: true},
		{name: "single segment no match", pattern: "Foo", input: "Bar", want: false},
		// 4+ segment patterns (validate-delegation, nonzero, enum-sync exceptions)
		{name: "4-segment exact", pattern: "pkg.Type.Field.mode", input: "pkg.Type.Field.mode", want: true},
		{name: "4-segment wildcard", pattern: "pkg.*.Field.*", input: "pkg.Type.Field.mode", want: true},
		{name: "4-segment mismatch count", pattern: "pkg.Type.Field", input: "pkg.Type.Field.mode", want: false},
		{name: "5-segment exact", pattern: "pkg.Type.Method.param.type", input: "pkg.Type.Method.param.type", want: true},
		{name: "5-segment wildcard middle", pattern: "pkg.*.*.param.*", input: "pkg.Type.Method.param.type", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matchPattern(tt.pattern, tt.input)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
			}
		})
	}
}

func TestIsExcepted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		exceptions    []Exception
		qualifiedName string
		want          bool
	}{
		{
			name:          "3-segment exact match",
			exceptions:    []Exception{{Pattern: "pkg.Foo.Bar"}},
			qualifiedName: "pkg.Foo.Bar",
			want:          true,
		},
		{
			name:          "2-segment match via stripped prefix",
			exceptions:    []Exception{{Pattern: "Foo.Bar"}},
			qualifiedName: "pkg.Foo.Bar",
			want:          true,
		},
		{
			name:          "wildcard match",
			exceptions:    []Exception{{Pattern: "*.Foo.*"}},
			qualifiedName: "pkg.Foo.Bar",
			want:          true,
		},
		{
			name:          "no match",
			exceptions:    []Exception{{Pattern: "Other.Field"}},
			qualifiedName: "pkg.Foo.Bar",
			want:          false,
		},
		{
			name:          "empty exceptions list",
			exceptions:    []Exception{},
			qualifiedName: "pkg.Foo.Bar",
			want:          false,
		},
		{
			name:          "multiple exceptions, second matches",
			exceptions:    []Exception{{Pattern: "No.Match"}, {Pattern: "pkg.Foo.Bar"}},
			qualifiedName: "pkg.Foo.Bar",
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &ExceptionConfig{Exceptions: tt.exceptions}
			got := cfg.isExcepted(tt.qualifiedName)
			if got != tt.want {
				t.Errorf("isExcepted(%q) = %v, want %v", tt.qualifiedName, got, tt.want)
			}
		})
	}
}

func TestIsSkippedType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		skipTypes []string
		typeName  string
		want      bool
	}{
		{name: "in list", skipTypes: []string{"bool", "error"}, typeName: "bool", want: true},
		{name: "not in list", skipTypes: []string{"bool", "error"}, typeName: "string", want: false},
		{name: "empty list", skipTypes: []string{}, typeName: "bool", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &ExceptionConfig{Settings: Settings{SkipTypes: tt.skipTypes}}
			got := cfg.isSkippedType(tt.typeName)
			if got != tt.want {
				t.Errorf("isSkippedType(%q) = %v, want %v", tt.typeName, got, tt.want)
			}
		})
	}
}

func TestStaleExceptions(t *testing.T) {
	t.Parallel()

	t.Run("no stale when all matched", func(t *testing.T) {
		t.Parallel()
		cfg := &ExceptionConfig{
			Exceptions:  []Exception{{Pattern: "Foo.Bar"}, {Pattern: "*.Baz"}},
			matchCounts: map[int]int{0: 3, 1: 1},
		}
		if stale := cfg.staleExceptions(); len(stale) != 0 {
			t.Errorf("expected 0 stale, got %v", stale)
		}
	})

	t.Run("reports unmatched entries", func(t *testing.T) {
		t.Parallel()
		cfg := &ExceptionConfig{
			Exceptions:  []Exception{{Pattern: "Foo.Bar"}, {Pattern: "Stale.One"}, {Pattern: "*.Baz"}},
			matchCounts: map[int]int{0: 1},
		}
		stale := cfg.staleExceptions()
		if len(stale) != 2 {
			t.Fatalf("expected 2 stale entries, got %d: %v", len(stale), stale)
		}
		if stale[0] != 1 || stale[1] != 2 {
			t.Errorf("expected stale indices [1, 2], got %v", stale)
		}
	})

	t.Run("empty exceptions list", func(t *testing.T) {
		t.Parallel()
		cfg := &ExceptionConfig{
			Exceptions:  []Exception{},
			matchCounts: map[int]int{},
		}
		if stale := cfg.staleExceptions(); len(stale) != 0 {
			t.Errorf("expected 0 stale, got %v", stale)
		}
	})
}

func TestMatchCountsTracking(t *testing.T) {
	t.Parallel()

	cfg := &ExceptionConfig{
		Exceptions:  []Exception{{Pattern: "Foo.Bar"}, {Pattern: "*.Baz"}},
		matchCounts: make(map[int]int),
	}

	// First exception should match.
	if !cfg.isExcepted("pkg.Foo.Bar") {
		t.Fatal("expected Foo.Bar to be excepted")
	}
	if cfg.matchCounts[0] != 1 {
		t.Errorf("expected matchCounts[0] = 1, got %d", cfg.matchCounts[0])
	}

	// Second exception should match.
	if !cfg.isExcepted("pkg.Type.Baz") {
		t.Fatal("expected *.Baz to match Type.Baz")
	}
	if cfg.matchCounts[1] != 1 {
		t.Errorf("expected matchCounts[1] = 1, got %d", cfg.matchCounts[1])
	}

	// Non-matching name — no counts should change.
	if cfg.isExcepted("pkg.Other.Field") {
		t.Fatal("expected Other.Field to NOT be excepted")
	}
}

func TestIsExcludedPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		excludePaths []string
		filePath     string
		want         bool
	}{
		{
			name:         "contains substring",
			excludePaths: []string{"specs/"},
			filePath:     "/home/foo/specs/bar.go",
			want:         true,
		},
		{
			name:         "no match",
			excludePaths: []string{"specs/"},
			filePath:     "/home/foo/internal/bar.go",
			want:         false,
		},
		{
			name:         "empty exclude list",
			excludePaths: []string{},
			filePath:     "/home/foo/specs/bar.go",
			want:         false,
		},
		{
			name:         "multiple excludes second matches",
			excludePaths: []string{"specs/", "testutil/"},
			filePath:     "/home/foo/testutil/bar.go",
			want:         true,
		},
		{
			name:         "windows separators are normalized",
			excludePaths: []string{"specs/", "testutil/"},
			filePath:     `C:\work\repo\specs\bar.go`,
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &ExceptionConfig{Settings: Settings{ExcludePaths: tt.excludePaths}}
			got := cfg.isExcludedPath(tt.filePath)
			if got != tt.want {
				t.Errorf("isExcludedPath(%q) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}
