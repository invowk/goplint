// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestWriteFindingToSink_WritesJSONLRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "findings.jsonl")
	analyzer := NewAnalyzer()
	if err := analyzer.Flags.Set("emit-findings-jsonl", path); err != nil {
		t.Fatalf("set emit-findings-jsonl flag: %v", err)
	}

	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 64)
	pos := file.Pos(12)
	pass := &analysis.Pass{
		Analyzer: analyzer,
		Fset:     fset,
	}

	writeFindingToSink(pass, pos, CategoryPrimitive, "id-1", "struct field pkg.A.B uses primitive type string")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read findings stream: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSONL record, got %d", len(lines))
	}
	var record FindingStreamRecord
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("decode JSONL record: %v", err)
	}
	if record.Category != CategoryPrimitive || record.ID != "id-1" {
		t.Fatalf("unexpected record identity: %+v", record)
	}
	if !strings.Contains(record.Posn, "fixture.go:1:13") {
		t.Fatalf("unexpected posn value %q", record.Posn)
	}
}

func TestWriteFindingToSink_NoPathNoWrite(t *testing.T) {
	t.Parallel()

	analyzer := NewAnalyzer()
	if err := analyzer.Flags.Set("emit-findings-jsonl", ""); err != nil {
		t.Fatalf("set emit-findings-jsonl flag: %v", err)
	}

	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 8)
	pass := &analysis.Pass{
		Analyzer: analyzer,
		Fset:     fset,
	}
	writeFindingToSink(pass, file.Pos(1), CategoryPrimitive, "id-2", "message")
}

func TestWriteFindingToSink_InvalidPathNoPanic(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	path := filepath.Join(base, "missing", "findings.jsonl")
	analyzer := NewAnalyzer()
	if err := analyzer.Flags.Set("emit-findings-jsonl", path); err != nil {
		t.Fatalf("set emit-findings-jsonl flag: %v", err)
	}

	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 8)
	pass := &analysis.Pass{
		Analyzer: analyzer,
		Fset:     fset,
	}
	writeFindingToSink(pass, file.Pos(1), CategoryPrimitive, "id-3", "message")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no output file for invalid path, stat err=%v", err)
	}
}

func TestEmitFindingsPathFromPass(t *testing.T) {
	t.Parallel()

	t.Run("nil pass", func(t *testing.T) {
		t.Parallel()
		if got := emitFindingsPathFromPass(nil); got != "" {
			t.Fatalf("emitFindingsPathFromPass(nil) = %q, want empty", got)
		}
	})

	t.Run("nil analyzer", func(t *testing.T) {
		t.Parallel()
		pass := &analysis.Pass{}
		if got := emitFindingsPathFromPass(pass); got != "" {
			t.Fatalf("emitFindingsPathFromPass(pass without analyzer) = %q, want empty", got)
		}
	})

	t.Run("missing flag", func(t *testing.T) {
		t.Parallel()
		pass := &analysis.Pass{Analyzer: &analysis.Analyzer{}}
		if got := emitFindingsPathFromPass(pass); got != "" {
			t.Fatalf("emitFindingsPathFromPass(pass missing flag) = %q, want empty", got)
		}
	})

	t.Run("returns flag value", func(t *testing.T) {
		t.Parallel()
		analyzer := &analysis.Analyzer{}
		analyzer.Flags.String("emit-findings-jsonl", "", "path to findings sink")
		const want = "/tmp/findings.jsonl"
		if err := analyzer.Flags.Set("emit-findings-jsonl", want); err != nil {
			t.Fatalf("set emit-findings-jsonl: %v", err)
		}
		pass := &analysis.Pass{Analyzer: analyzer}
		if got := emitFindingsPathFromPass(pass); got != want {
			t.Fatalf("emitFindingsPathFromPass() = %q, want %q", got, want)
		}
	})
}
