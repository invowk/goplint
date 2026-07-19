// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/stableidmigration"
)

func TestRunWritesAcceptedReportAndRejectsInvalidMigration(t *testing.T) {
	t.Parallel()

	oldPath := writeScan(t, "old.jsonl", `{"package":"p","category":"c","id":"old","message":"m","posn":"a.go:1:1"}`)
	newPath := writeScan(t, "new.jsonl", `{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`)
	repeatPath := writeScan(t, "repeat.jsonl", `{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`)

	var stdout bytes.Buffer
	if err := run([]string{"-old", oldPath, "-new", newPath, "-repeat", repeatPath}, &stdout); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"accepted": true`)) {
		t.Fatalf("run() output = %s, want accepted report", stdout.String())
	}

	badRepeat := writeScan(t, "bad-repeat.jsonl", `{"package":"p","category":"c","id":"other","message":"m","posn":"a.go:1:1"}`)
	outPath := filepath.Join(t.TempDir(), "rejected.json")
	if err := run([]string{"-old", oldPath, "-new", newPath, "-repeat", badRepeat, "-out", outPath}, &stdout); err == nil {
		t.Fatal("run() accepted nondeterministic migration")
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read rejected report: %v", err)
	}
	if !bytes.Contains(data, []byte(`"accepted": false`)) {
		t.Fatalf("rejected report = %s, want accepted=false", data)
	}
}

func TestRunRequiresAllScanPaths(t *testing.T) {
	t.Parallel()

	if err := run(nil, &bytes.Buffer{}); err == nil {
		t.Fatal("run() accepted missing scan paths")
	}
}

func TestRunAcceptsExactPopulationReviewManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	oldPath := filepath.Join(root, "old.jsonl")
	if err := os.WriteFile(oldPath, nil, 0o644); err != nil {
		t.Fatalf("write empty old scan: %v", err)
	}
	line := `{"package":"p","category":"nonzero-value-field","id":"new","message":"m","posn":"a.go:1:1"}`
	newPath := writeScan(t, "new.jsonl", line)
	repeatPath := writeScan(t, "repeat.jsonl", line)
	report, err := stableidmigration.Build(nil, []byte(line+"\n"), []byte(line+"\n"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	change := report.PopulationChanges[0]
	manifest := populationReviewManifest{
		SchemaVersion: populationReviewSchemaVersion,
		Reviews: []stableidmigration.PopulationChangeReview{{
			Status:          change.Status,
			Category:        change.Category,
			Population:      change.Population,
			CanonicalSHA256: change.CanonicalSHA256,
			Reason:          "reviewed scan-scope expansion",
			Evidence:        "evidence/scan-scope.md",
		}},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal population review: %v", err)
	}
	manifestPath := filepath.Join(root, "population-review.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("write population review: %v", err)
	}
	var stdout bytes.Buffer
	if err := run([]string{
		"-old", oldPath,
		"-new", newPath,
		"-repeat", repeatPath,
		"-population-review", manifestPath,
	}, &stdout); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"reviewed": true`)) {
		t.Fatalf("run() output = %s, want reviewed population", stdout.String())
	}
}

func writeScan(t testing.TB, name, line string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write scan: %v", err)
	}
	return path
}
