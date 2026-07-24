// SPDX-License-Identifier: MPL-2.0

package repositoryaudit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/invowk/invowk/tools/goplint/goplint"
)

func TestRunUsesOneSupersetTraversalWithoutBaselineSuppression(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	analyzerPath := writeRunnerFile(t, root, "bin/goplint", "analyzer")
	baselinePath := filepath.Join(root, "baseline.toml")
	if err := goplint.WriteBaseline(baselinePath, map[string][]goplint.BaselineFinding{
		goplint.CategoryPrimitive: {{ID: "accepted", Message: "accepted primitive"}},
	}); err != nil {
		t.Fatal(err)
	}
	writeRunnerFile(t, root, "exceptions.toml", "")
	writeRunnerFile(t, root, "semantic.json", "{}")
	outputPath := filepath.Join(t.TempDir(), "audit.json")
	executions := 0
	result, err := run(t.Context(), RunOptions{
		Root: root, AnalyzerPath: "bin/goplint", BaselinePath: "baseline.toml",
		ExceptionsPath: "exceptions.toml", SemanticManifestPath: "semantic.json",
		PackagePatterns: []string{"./pkg/..."}, WorkspaceDigest: testDigest,
		CachePolicy: "warm", OutputPath: outputPath,
	}, runnerDependencies{
		execute: func(_ context.Context, gotRoot, gotAnalyzer string, arguments []string) (analyzerExecution, error) {
			executions++
			if gotRoot != root || gotAnalyzer != analyzerPath {
				t.Fatalf("execute root/analyzer = %q/%q", gotRoot, gotAnalyzer)
			}
			for _, argument := range arguments {
				if strings.HasPrefix(argument, "-baseline") {
					t.Fatalf("superset traversal contains baseline suppression: %q", arguments)
				}
			}
			findingsPath := argumentValue(arguments, "-emit-findings-jsonl=")
			findings := strings.Join([]string{
				`{"package":"example.com/a","category":"primitive","id":"accepted","message":"accepted primitive","posn":"pkg/a.go:1:1"}`,
				`{"package":"example.com/a","category":"unvalidated-cast-inconclusive","id":"inconclusive","message":"always visible","posn":"pkg/a.go:2:1"}`,
			}, "\n") + "\n"
			if err := os.WriteFile(findingsPath, []byte(findings), 0o600); err != nil {
				return analyzerExecution{}, err
			}
			return analyzerExecution{
				Stdout: []byte(`{"example.com/a":{"goplint":[` +
					`{"posn":"pkg/a.go:1:1","message":"accepted primitive","category":"primitive"},` +
					`{"posn":"pkg/a.go:2:1","message":"always visible","category":"unvalidated-cast-inconclusive"}` +
					`]}}`),
				ExitCode: 3,
			}, nil
		},
		now:             sequentialTimes(),
		workspaceDigest: func(context.Context, string) (string, error) { return testDigest, nil },
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if executions != 1 {
		t.Fatalf("analyzer executions = %d, want 1", executions)
	}
	if len(result.Baseline.Matched) != 1 || len(result.Baseline.New) != 1 {
		t.Fatalf("baseline verdict = %+v", result.Baseline)
	}
	loaded, err := Load(t.Context(), outputPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ResultID != result.ResultID {
		t.Fatalf("retained result id = %q, want %q", loaded.ResultID, result.ResultID)
	}
}

func TestRunRejectsToolFailureBeforePublishing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRunnerFile(t, root, "bin/goplint", "analyzer")
	if err := goplint.WriteBaseline(filepath.Join(root, "baseline.toml"), map[string][]goplint.BaselineFinding{}); err != nil {
		t.Fatal(err)
	}
	writeRunnerFile(t, root, "exceptions.toml", "")
	writeRunnerFile(t, root, "semantic.json", "{}")
	wantErr := errors.New("tool crashed")
	_, err := run(t.Context(), RunOptions{
		Root: root, AnalyzerPath: "bin/goplint", BaselinePath: "baseline.toml",
		ExceptionsPath: "exceptions.toml", SemanticManifestPath: "semantic.json",
		PackagePatterns: []string{"./..."}, WorkspaceDigest: testDigest,
	}, runnerDependencies{
		execute: func(context.Context, string, string, []string) (analyzerExecution, error) {
			return analyzerExecution{}, wantErr
		},
		now:             sequentialTimes(),
		workspaceDigest: func(context.Context, string) (string, error) { return testDigest, nil },
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("run() error = %v, want %v", err, wantErr)
	}
}

func writeRunnerFile(t *testing.T, root, relativePath, contents string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func argumentValue(arguments []string, prefix string) string {
	for _, argument := range arguments {
		if value, exists := strings.CutPrefix(argument, prefix); exists {
			return value
		}
	}
	return ""
}

func sequentialTimes() func() time.Time {
	current := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		result := current
		current = current.Add(time.Second)
		return result
	}
}
