// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"encoding/json"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestWriteFindingToSink_WritesJSONLRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "findings.jsonl")

	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 64)
	pos := file.Pos(12)
	pass := &analysis.Pass{
		Fset:   fset,
		Report: func(analysis.Diagnostic) {},
	}
	restore := installDiagnosticReporter(pass, path)
	defer restore()

	reportDiagnostic(pass, pos, CategoryPrimitive, "id-1", "struct field pkg.A.B uses primitive type string")

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

func TestWriteFindingToSinkWithMeta_WritesCompactMeta(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "findings.jsonl")

	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 64)
	pos := file.Pos(12)
	pass := &analysis.Pass{
		Fset:   fset,
		Report: func(analysis.Diagnostic) {},
	}
	restore := installDiagnosticReporter(pass, path)
	defer restore()

	reportDiagnosticWithMeta(pass, pos, CategoryPrimitive, "id-meta", "msg", map[string]string{
		"ubv_scope": "same-block",
		"":          "ignored",
		"ignored":   "",
	})

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
	if got := record.Meta["ubv_scope"]; got != "same-block" {
		t.Fatalf("record.Meta[ubv_scope] = %q, want same-block", got)
	}
	if _, ok := record.Meta["ignored"]; ok {
		t.Fatal("expected empty-value metadata key to be dropped")
	}
}

func TestWriteFindingToSink_NoPathNoWrite(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 8)
	pass := &analysis.Pass{
		Fset:   fset,
		Report: func(analysis.Diagnostic) {},
	}
	restore := installDiagnosticReporter(pass, "")
	defer restore()

	reportDiagnostic(pass, file.Pos(1), CategoryPrimitive, "id-2", "message")
}

func TestWriteFindingToSink_InvalidPathNoPanic(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	path := filepath.Join(base, "missing", "findings.jsonl")

	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 8)
	pass := &analysis.Pass{
		Fset:   fset,
		Report: func(analysis.Diagnostic) {},
	}
	restore := installDiagnosticReporter(pass, path)
	defer restore()

	reportDiagnostic(pass, file.Pos(1), CategoryPrimitive, "id-3", "message")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no output file for invalid path, stat err=%v", err)
	}
}

func TestInstallDiagnosticReporterRestoresOriginalReport(t *testing.T) {
	t.Parallel()

	originalCalled := false
	original := func(analysis.Diagnostic) {
		originalCalled = true
	}
	pass := &analysis.Pass{Report: original}

	restore := installDiagnosticReporter(pass, "")
	pass.Report(analysis.Diagnostic{Message: "test"})
	if !originalCalled {
		t.Fatal("expected installed reporter to delegate to original Report")
	}
	restore()

	originalCalled = false
	pass.Report(analysis.Diagnostic{Message: "restored"})
	if !originalCalled {
		t.Fatal("expected restored Report to call original directly")
	}
	if reporterForPass(pass) != nil {
		t.Fatal("expected reporter registry entry to be removed")
	}
}

func TestWarnFindingSinkError(t *testing.T) {
	t.Parallel()

	t.Run("dedupes repeated warnings", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		var seen sync.Map
		err := os.ErrPermission

		warnFindingSinkError(&out, &seen, "/tmp/findings.jsonl", err)
		warnFindingSinkError(&out, &seen, "/tmp/findings.jsonl", err)

		if got := strings.Count(out.String(), "findings sink"); got != 1 {
			t.Fatalf("expected 1 warning, got %d; output=%q", got, out.String())
		}
	})

	t.Run("without dedupe map writes every warning", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		err := os.ErrPermission

		warnFindingSinkError(&out, nil, "/tmp/findings.jsonl", err)
		warnFindingSinkError(&out, nil, "/tmp/findings.jsonl", err)

		if got := strings.Count(out.String(), "findings sink"); got != 2 {
			t.Fatalf("expected 2 warnings, got %d; output=%q", got, out.String())
		}
	})
}
