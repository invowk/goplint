// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"go/token"
	"os"

	"golang.org/x/tools/go/analysis"
)

// FindingStreamRecord is one JSONL entry in the internal findings stream used
// by -emit-findings-jsonl / -update-baseline plumbing.
type FindingStreamRecord struct {
	Category string `json:"category"`
	ID       string `json:"id"`
	Message  string `json:"message"`
	Posn     string `json:"posn,omitempty"`
}

func writeFindingToSink(pass *analysis.Pass, pos token.Pos, category, findingID, message string) {
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
	}
	if pass.Fset != nil && pos.IsValid() {
		record.Posn = pass.Fset.Position(pos).String()
	}

	line, err := json.Marshal(record)
	if err != nil {
		return
	}

	line = append(line, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	_, _ = file.Write(line)
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
