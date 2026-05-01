// SPDX-License-Identifier: MPL-2.0

package main

import (
	"strings"
	"testing"

	"github.com/invowk/invowk/tools/goplint/goplint"
)

func TestParseFindingsJSONL(t *testing.T) {
	t.Parallel()

	t.Run("unknown category fails", func(t *testing.T) {
		t.Parallel()
		input := []byte(`{"category":"unknown-category","id":"id-1","message":"x"}`)
		if _, err := goplint.CollectBaselineFindingsFromStream(input); err == nil {
			t.Fatal("expected unknown category error")
		}
	})

	t.Run("non-suppressible category is dropped", func(t *testing.T) {
		t.Parallel()
		input := []byte(strings.Join([]string{
			`{"category":"unknown-directive","id":"id-ud","message":"unknown directive","posn":"pkg/a.go:1:1"}`,
			`{"category":"primitive","id":"id-prim","message":"struct field pkg.A.B uses primitive type string","posn":"pkg/a.go:2:1"}`,
			"",
		}, "\n"))
		findings, err := goplint.CollectBaselineFindingsFromStream(input)
		if err != nil {
			t.Fatalf("goplint.CollectBaselineFindingsFromStream() error = %v", err)
		}
		if len(findings[goplint.CategoryUnknownDirective]) != 0 {
			t.Fatal("expected unknown-directive findings to be excluded from baseline stream")
		}
		if got := len(findings[goplint.CategoryPrimitive]); got != 1 {
			t.Fatalf("expected 1 primitive finding, got %d", got)
		}
	})

	t.Run("duplicates are deduplicated by id", func(t *testing.T) {
		t.Parallel()
		input := []byte(strings.Join([]string{
			`{"category":"primitive","id":"id-1","message":"msg-a","posn":"pkg/a.go:1:1"}`,
			`{"category":"primitive","id":"id-1","message":"msg-b","posn":"pkg/a.go:2:1"}`,
			"",
		}, "\n"))
		findings, err := goplint.CollectBaselineFindingsFromStream(input)
		if err != nil {
			t.Fatalf("goplint.CollectBaselineFindingsFromStream() error = %v", err)
		}
		entries := findings[goplint.CategoryPrimitive]
		if len(entries) != 1 {
			t.Fatalf("expected 1 deduplicated finding, got %d", len(entries))
		}
		if entries[0].ID != "id-1" {
			t.Fatalf("expected id-1, got %q", entries[0].ID)
		}
	})

	t.Run("malformed record fails", func(t *testing.T) {
		t.Parallel()
		input := []byte(`{"category":"primitive","message":"missing-id"}`)
		if _, err := goplint.CollectBaselineFindingsFromStream(input); err == nil {
			t.Fatal("expected malformed record error")
		}
	})

	t.Run("last line without trailing newline is parsed", func(t *testing.T) {
		t.Parallel()
		input := []byte(`{"category":"primitive","id":"id-final","message":"struct field pkg.A.B uses primitive type string","posn":"pkg/a.go:4:2"}`)
		findings, err := goplint.CollectBaselineFindingsFromStream(input)
		if err != nil {
			t.Fatalf("goplint.CollectBaselineFindingsFromStream() error = %v", err)
		}
		if got := len(findings[goplint.CategoryPrimitive]); got != 1 {
			t.Fatalf("expected 1 primitive finding, got %d", got)
		}
	})

	t.Run("whitespace-only input returns empty findings", func(t *testing.T) {
		t.Parallel()
		findings, err := goplint.CollectBaselineFindingsFromStream([]byte(" \n\t "))
		if err != nil {
			t.Fatalf("goplint.CollectBaselineFindingsFromStream() error = %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("expected 0 categories, got %d", len(findings))
		}
	})

	t.Run("trailing garbage after valid record fails", func(t *testing.T) {
		t.Parallel()
		input := []byte(`{"category":"primitive","id":"id-ok","message":"ok"}{"bad":`)
		if _, err := goplint.CollectBaselineFindingsFromStream(input); err == nil {
			t.Fatal("expected decoding error for trailing garbage")
		}
	})
}

func TestParseFindingsJSONLIgnoresNonFindingKinds(t *testing.T) {
	t.Parallel()

	input := []byte(strings.Join([]string{
		`{"kind":"refinement-trace","category":"unvalidated-cast","id":"trace-1","message":"unsafe"}`,
		`{"category":"primitive","id":"id-1","message":"struct field pkg.A.B uses primitive type string"}`,
		"",
	}, "\n"))

	findings, err := goplint.CollectBaselineFindingsFromStream(input)
	if err != nil {
		t.Fatalf("goplint.CollectBaselineFindingsFromStream() error = %v", err)
	}
	if got := len(findings[goplint.CategoryPrimitive]); got != 1 {
		t.Fatalf("expected 1 primitive finding, got %d", got)
	}
	if got := len(findings[goplint.CategoryUnvalidatedCast]); got != 0 {
		t.Fatalf("expected refinement traces to be ignored, got %d unvalidated-cast entries", got)
	}
}
