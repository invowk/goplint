// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	t.Run("empty path returns empty config", func(t *testing.T) {
		t.Parallel()
		cfg, err := loadConfig("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Exceptions) != 0 {
			t.Errorf("expected 0 exceptions, got %d", len(cfg.Exceptions))
		}
	})

	t.Run("nonexistent file returns empty config", func(t *testing.T) {
		t.Parallel()
		cfg, err := loadConfig(filepath.Join(t.TempDir(), "nonexistent.toml"))
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

		cfg, err := loadConfig(path)
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

		cfg, err := loadConfig(path)
		if err == nil {
			t.Fatal("expected error for invalid TOML, got nil")
		}
		if cfg != nil {
			t.Errorf("expected nil config on error, got %+v", cfg)
		}
	})
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
