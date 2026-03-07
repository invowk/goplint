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
	Category string            `json:"category,omitempty"`
	ID       string            `json:"id,omitempty"`
	Message  string            `json:"message,omitempty"`
	Posn     string            `json:"posn,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

var findingSinkWarnings sync.Map // map[string]*sync.Once

func writeFindingToSink(pass *analysis.Pass, pos token.Pos, category, findingID, message string) {
	writeFindingToSinkWithMeta(pass, pos, category, findingID, message, nil)
}

func writeFindingToSinkWithMeta(pass *analysis.Pass, pos token.Pos, category, findingID, message string, meta map[string]string) {
	if pass == nil {
		return
	}

	path := emitFindingsPathFromPass(pass)
	if path == "" {
		return
	}

	record := FindingStreamRecord{
		Category: category,
		ID:       findingID,
		Message:  message,
		Meta:     compactFindingMeta(meta),
	}
	if pass.Fset != nil && pos.IsValid() {
		record.Posn = pass.Fset.Position(pos).String()
	}

	writeFindingStreamRecord(path, record)
}

func writeFindingStreamRecord(path string, record FindingStreamRecord) {
	line, err := json.Marshal(record)
	if err != nil {
		warnFindingSinkError(os.Stderr, &findingSinkWarnings, path, fmt.Errorf("encoding finding stream record: %w", err))
		return
	}

	line = append(line, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		warnFindingSinkError(os.Stderr, &findingSinkWarnings, path, fmt.Errorf("opening finding stream: %w", err))
		return
	}
	if _, err := file.Write(line); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			warnFindingSinkError(os.Stderr, &findingSinkWarnings, path, fmt.Errorf("closing finding stream after write failure: %w", closeErr))
		}
		warnFindingSinkError(os.Stderr, &findingSinkWarnings, path, fmt.Errorf("writing finding stream: %w", err))
		return
	}
	if err := file.Close(); err != nil {
		warnFindingSinkError(os.Stderr, &findingSinkWarnings, path, fmt.Errorf("closing finding stream: %w", err))
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
	once := onceValue.(*sync.Once)
	once.Do(writeWarning)
}

func emitFindingsPathFromPass(pass *analysis.Pass) string {
	if pass == nil || pass.Analyzer == nil {
		return ""
	}
	flagSet := pass.Analyzer.Flags
	flag := flagSet.Lookup("emit-findings-jsonl")
	if flag == nil || flag.Value == nil {
		return ""
	}
	return flag.Value.String()
}
