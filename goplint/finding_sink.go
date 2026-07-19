// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"fmt"
	"go/token"
	"io"
	"os"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// FindingStreamRecord is one JSONL entry in the internal findings stream used
// by -emit-findings-jsonl / -update-baseline plumbing.
type FindingStreamRecord struct {
	Kind     string            `json:"kind,omitempty"`
	Package  string            `json:"package,omitempty"`
	Category string            `json:"category,omitempty"`
	ID       string            `json:"id,omitempty"`
	Message  string            `json:"message,omitempty"`
	Posn     string            `json:"posn,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

var findingSinkWarnings sync.Map // map[string]*sync.Once

var findingReporters sync.Map // map[*analysis.Pass]*diagnosticReporter

type diagnosticReporter struct {
	fset   *token.FileSet
	pkg    string
	report func(analysis.Diagnostic)
	stream *findingStreamWriter
}

type findingStreamWriter struct {
	path     string
	stderr   io.Writer
	warnings *sync.Map
}

func installDiagnosticReporter(pass *analysis.Pass, findingsPath string) func() {
	if pass == nil {
		return func() {}
	}

	originalReport := pass.Report
	reporter := &diagnosticReporter{
		fset:   pass.Fset,
		report: originalReport,
	}
	if pass.Pkg != nil {
		reporter.pkg = pass.Pkg.Path()
	}
	if findingsPath != "" {
		reporter.stream = &findingStreamWriter{
			path:     findingsPath,
			stderr:   os.Stderr,
			warnings: &findingSinkWarnings,
		}
	}

	pass.Report = reporter.Report
	findingReporters.Store(pass, reporter)
	return func() {
		pass.Report = originalReport
		findingReporters.Delete(pass)
	}
}

func reporterForPass(pass *analysis.Pass) *diagnosticReporter {
	if pass == nil {
		return nil
	}
	value, ok := findingReporters.Load(pass)
	if !ok {
		return nil
	}
	reporter, _ := value.(*diagnosticReporter)
	return reporter
}

func (r *diagnosticReporter) Report(d analysis.Diagnostic) {
	if r == nil {
		return
	}
	r.writeDiagnostic(d)
	if r.report != nil {
		r.report(d)
	}
}

func (r *diagnosticReporter) WriteRecord(record FindingStreamRecord) {
	if r == nil || r.stream == nil {
		return
	}
	r.stream.Write(record)
}

func (r *diagnosticReporter) writeDiagnostic(d analysis.Diagnostic) {
	if r == nil || r.stream == nil {
		return
	}
	record, ok := findingStreamRecordFromDiagnostic(r.fset, d)
	if !ok {
		return
	}
	record.Package = r.pkg
	r.stream.Write(record)
}

func (w *findingStreamWriter) Write(record FindingStreamRecord) {
	if w == nil || w.path == "" {
		return
	}
	writeFindingStreamRecord(w.path, record, w.stderr, w.warnings)
}

func findingStreamRecordFromDiagnostic(fset *token.FileSet, diagnostic analysis.Diagnostic) (FindingStreamRecord, bool) {
	findingID := FindingIDFromDiagnosticURL(diagnostic.URL)
	if findingID == "" {
		return FindingStreamRecord{}, false
	}
	record := FindingStreamRecord{
		Category: diagnostic.Category,
		ID:       findingID,
		Message:  diagnostic.Message,
		Meta:     compactFindingMeta(findingMetaFromDiagnosticURL(diagnostic.URL)),
	}
	if fset != nil && diagnostic.Pos.IsValid() {
		record.Posn = fset.Position(diagnostic.Pos).String()
	}
	return record, true
}

func writeFindingStreamRecord(path string, record FindingStreamRecord, stderr io.Writer, warnings *sync.Map) {
	line, err := json.Marshal(record)
	if err != nil {
		warnFindingSinkError(stderr, warnings, path, fmt.Errorf("encoding finding stream record: %w", err))
		return
	}

	line = append(line, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		warnFindingSinkError(stderr, warnings, path, fmt.Errorf("opening finding stream: %w", err))
		return
	}
	if _, err := file.Write(line); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			warnFindingSinkError(stderr, warnings, path, fmt.Errorf("closing finding stream after write failure: %w", closeErr))
		}
		warnFindingSinkError(stderr, warnings, path, fmt.Errorf("writing finding stream: %w", err))
		return
	}
	if err := file.Close(); err != nil {
		warnFindingSinkError(stderr, warnings, path, fmt.Errorf("closing finding stream: %w", err))
		return
	}
}

func compactFindingMeta(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]string, len(meta))
	for k, v := range meta {
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func warnFindingSinkError(stderr io.Writer, dedupe *sync.Map, path string, err error) {
	if stderr == nil || err == nil {
		return
	}

	writeWarning := func() {
		if _, writeErr := fmt.Fprintf(stderr, "goplint: warning: findings sink %q disabled after write error: %v\n", path, err); writeErr != nil {
			return
		}
	}
	if dedupe == nil {
		writeWarning()
		return
	}

	key := path + "|" + err.Error()
	onceValue, _ := dedupe.LoadOrStore(key, &sync.Once{})
	once, ok := onceValue.(*sync.Once)
	if !ok {
		writeWarning()
		return
	}
	once.Do(writeWarning)
}
