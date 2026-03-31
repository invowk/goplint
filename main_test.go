// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/invowk/invowk/tools/goplint/goplint"
)

func TestExtractUpdateBaselinePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "double-dash form",
			args: []string{"--update-baseline=out.toml", "./..."},
			want: "out.toml",
		},
		{
			name: "single-dash form",
			args: []string{"-update-baseline=/tmp/baseline.toml"},
			want: "/tmp/baseline.toml",
		},
		{
			name: "mixed with other flags",
			args: []string{"-check-all", "--update-baseline=b.toml", "-config=e.toml", "./..."},
			want: "b.toml",
		},
		{
			name: "space separated value",
			args: []string{"--update-baseline", "out.toml", "./..."},
			want: "out.toml",
		},
		{
			name: "absent flag returns empty",
			args: []string{"-check-all", "-config=e.toml", "./..."},
			want: "",
		},
		{
			name: "empty args",
			args: []string{},
			want: "",
		},
		{
			name: "nil args",
			args: nil,
			want: "",
		},
		{
			name: "flag without equals returns empty",
			args: []string{"--update-baseline"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractUpdateBaselinePath(tt.args)
			if got != tt.want {
				t.Errorf("extractUpdateBaselinePath(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestBuildSubprocessArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "removes update-baseline and adds json",
			args: []string{"--update-baseline=out.toml", "-check-all", "./..."},
			want: []string{"-json", "-check-all", "./..."},
		},
		{
			name: "preserves existing json flag",
			args: []string{"-json", "--update-baseline=out.toml", "-check-all"},
			want: []string{"-json", "-check-all"},
		},
		{
			name: "removes single-dash update-baseline",
			args: []string{"-update-baseline=out.toml", "./..."},
			want: []string{"-json", "./..."},
		},
		{
			name: "preserves all other flags and patterns",
			args: []string{"-check-all", "-config=e.toml", "--update-baseline=b.toml", "./internal/...", "./pkg/..."},
			want: []string{"-json", "-check-all", "-config=e.toml", "./internal/...", "./pkg/..."},
		},
		{
			name: "removes space separated update-baseline and value",
			args: []string{"--update-baseline", "out.toml", "-check-all", "./..."},
			want: []string{"-json", "-check-all", "./..."},
		},
		{
			name: "empty args adds json only",
			args: []string{},
			want: []string{"-json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildSubprocessArgs(tt.args)
			if !slices.Equal(got, tt.want) {
				t.Errorf("buildSubprocessArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestDispatch(t *testing.T) {
	t.Parallel()

	t.Run("update-baseline missing path returns usage error", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		args, code, handled := dispatch([]string{"--update-baseline"}, &stderr)
		if !handled {
			t.Fatal("expected update-baseline path validation to be handled")
		}
		if code != 2 {
			t.Fatalf("dispatch() code = %d, want 2", code)
		}
		if len(args) != 0 {
			t.Fatalf("dispatch() args = %v, want empty", args)
		}
		if !strings.Contains(stderr.String(), "missing required path value") {
			t.Fatalf("expected missing-path message, got %q", stderr.String())
		}
	})

	t.Run("update-baseline success delegates to handler", func(t *testing.T) {
		called := false
		deps := dispatchDeps{
			generateBaseline: func(outputPath string, _ []string) error {
				called = true
				if outputPath != "out.toml" {
					t.Fatalf("outputPath = %q, want %q", outputPath, "out.toml")
				}
				return nil
			},
			auditExceptionsGlobal: func(_ []string) error { return nil },
		}
		var stderr bytes.Buffer
		args, code, handled := dispatchWithDeps([]string{"--update-baseline=out.toml", "./..."}, &stderr, deps)
		if !handled || code != 0 {
			t.Fatalf("dispatch() = (args=%v, code=%d, handled=%v), want handled success", args, code, handled)
		}
		if !called {
			t.Fatal("expected generateBaseline handler to be called")
		}
	})

	t.Run("global true delegates to audit handler", func(t *testing.T) {
		called := false
		deps := dispatchDeps{
			generateBaseline: func(_ string, _ []string) error { return nil },
			auditExceptionsGlobal: func(args []string) error {
				called = true
				if !hasFlagToken(args, "global") {
					t.Fatalf("expected original args to include global flag, got %v", args)
				}
				return nil
			},
		}

		var stderr bytes.Buffer
		nextArgs, code, handled := dispatchWithDeps([]string{"--global", "./..."}, &stderr, deps)
		if !handled || code != 0 {
			t.Fatalf("dispatch() = (args=%v, code=%d, handled=%v), want handled success", nextArgs, code, handled)
		}
		if !called {
			t.Fatal("expected audit handler to be called")
		}
	})

	t.Run("global false strips meta-flag and delegates to singlechecker", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		nextArgs, code, handled := dispatch([]string{"--global=false", "-check-all", "./..."}, &stderr)
		if handled {
			t.Fatal("expected --global=false path to continue to singlechecker")
		}
		if code != 0 {
			t.Fatalf("dispatch() code = %d, want 0", code)
		}
		if hasFlagToken(nextArgs, "global") {
			t.Fatalf("expected global flag removed, got %v", nextArgs)
		}
		want := []string{"-check-all", "./..."}
		if !slices.Equal(nextArgs, want) {
			t.Fatalf("dispatch() args = %v, want %v", nextArgs, want)
		}
	})
}

func TestHasFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		flag string
		want bool
	}{
		{name: "single-dash present", args: []string{"-global"}, flag: "global", want: true},
		{name: "double-dash present", args: []string{"--global"}, flag: "global", want: true},
		{name: "absent", args: []string{"-check-all"}, flag: "global", want: false},
		{name: "value form true", args: []string{"--global=true"}, flag: "global", want: true},
		{name: "value form false", args: []string{"--global=false"}, flag: "global", want: false},
		{name: "space value true", args: []string{"--global", "true"}, flag: "global", want: true},
		{name: "space value false", args: []string{"--global", "false"}, flag: "global", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hasFlag(tt.args, tt.flag)
			if got != tt.want {
				t.Errorf("hasFlag(%v, %q) = %v, want %v", tt.args, tt.flag, got, tt.want)
			}
		})
	}
}

func TestHasFlagToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		flag string
		want bool
	}{
		{name: "double dash bare", args: []string{"--global"}, flag: "global", want: true},
		{name: "double dash equals", args: []string{"--global=false"}, flag: "global", want: true},
		{name: "single dash equals", args: []string{"-global=true"}, flag: "global", want: true},
		{name: "absent", args: []string{"-check-all"}, flag: "global", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hasFlagToken(tt.args, tt.flag)
			if got != tt.want {
				t.Errorf("hasFlagToken(%v, %q) = %v, want %v", tt.args, tt.flag, got, tt.want)
			}
		})
	}
}

func TestBuildGlobalAuditArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "removes global and adds required flags",
			args: []string{"--global", "./..."},
			want: []string{"-audit-exceptions", "-json", "./..."},
		},
		{
			name: "preserves existing required flags",
			args: []string{"-json", "-audit-exceptions", "--global", "./..."},
			want: []string{"-json", "-audit-exceptions", "./..."},
		},
		{
			name: "keeps unrelated flags",
			args: []string{"--global", "-config=exceptions.toml", "./pkg/..."},
			want: []string{"-audit-exceptions", "-json", "-config=exceptions.toml", "./pkg/..."},
		},
		{
			name: "removes bool value token when explicit",
			args: []string{"--global", "true", "-config=exceptions.toml", "./pkg/..."},
			want: []string{"-audit-exceptions", "-json", "-config=exceptions.toml", "./pkg/..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildGlobalAuditArgs(tt.args)
			if !slices.Equal(got, tt.want) {
				t.Errorf("buildGlobalAuditArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestAggregateGlobalStalePatterns(t *testing.T) {
	t.Parallel()

	t.Run("deduplicates package coverage per pattern", func(t *testing.T) {
		t.Parallel()
		stream := append(
			makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/a": {
					"goplint": {
						{Category: goplint.CategoryStaleException, Message: `stale exception: pattern "dup.pattern" matched no diagnostics (reason: x)`},
						{Category: goplint.CategoryStaleException, Message: `stale exception: pattern "dup.pattern" matched no diagnostics (reason: x)`},
					},
				},
			}),
			makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/b": {
					"goplint": {},
				},
			})...,
		)

		patterns, totalPatterns, totalPackages, err := aggregateGlobalStalePatterns(stream)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if totalPackages != 2 {
			t.Fatalf("expected 2 packages, got %d", totalPackages)
		}
		if totalPatterns != 1 {
			t.Fatalf("expected 1 stale pattern, got %d", totalPatterns)
		}
		if len(patterns) != 0 {
			t.Fatalf("expected no globally stale patterns, got %v", patterns)
		}
	})

	t.Run("reports patterns stale in all packages", func(t *testing.T) {
		t.Parallel()
		stream := append(
			makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/a": {
					"goplint": {
						{Category: goplint.CategoryStaleException, Message: `stale exception: pattern "shared.pattern" matched no diagnostics (reason: x)`},
					},
				},
			}),
			makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/b": {
					"goplint": {
						{Category: goplint.CategoryStaleException, Message: `stale exception: pattern "shared.pattern" matched no diagnostics (reason: y)`},
					},
				},
			})...,
		)

		patterns, totalPatterns, totalPackages, err := aggregateGlobalStalePatterns(stream)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if totalPackages != 2 {
			t.Fatalf("expected 2 packages, got %d", totalPackages)
		}
		if totalPatterns != 1 {
			t.Fatalf("expected 1 stale pattern, got %d", totalPatterns)
		}
		if !slices.Equal(patterns, []string{"shared.pattern"}) {
			t.Fatalf("unexpected globally stale patterns: got %v", patterns)
		}
	})

	t.Run("no packages analyzed returns zero counts", func(t *testing.T) {
		t.Parallel()
		patterns, totalPatterns, totalPackages, err := aggregateGlobalStalePatterns(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(patterns) != 0 {
			t.Fatalf("expected no patterns, got %v", patterns)
		}
		if totalPatterns != 0 || totalPackages != 0 {
			t.Fatalf("expected zero counts, got patterns=%d packages=%d", totalPatterns, totalPackages)
		}
	})

	t.Run("whitespace-only stream returns zero counts", func(t *testing.T) {
		t.Parallel()
		patterns, totalPatterns, totalPackages, err := aggregateGlobalStalePatterns([]byte(" \n\t "))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(patterns) != 0 {
			t.Fatalf("expected no patterns, got %v", patterns)
		}
		if totalPatterns != 0 || totalPackages != 0 {
			t.Fatalf("expected zero counts, got patterns=%d packages=%d", totalPatterns, totalPackages)
		}
	})

	t.Run("malformed stream returns decode error", func(t *testing.T) {
		t.Parallel()
		_, _, _, err := aggregateGlobalStalePatterns([]byte("{invalid"))
		if err == nil {
			t.Fatal("expected decode error")
		}
	})
}

func TestAuditExceptionsGlobalExitBehavior(t *testing.T) {
	t.Parallel()

	t.Run("fails when globally stale patterns are found", func(t *testing.T) {
		t.Parallel()
		stream := append(
			makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/a": {
					"goplint": {
						{Category: goplint.CategoryStaleException, Message: `stale exception: pattern "shared.pattern" matched no diagnostics (reason: x)`},
					},
				},
			}),
			makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/b": {
					"goplint": {
						{Category: goplint.CategoryStaleException, Message: `stale exception: pattern "shared.pattern" matched no diagnostics (reason: y)`},
					},
				},
			})...,
		)

		runner := func(cmd *exec.Cmd) error {
			buf, ok := cmd.Stdout.(*bytes.Buffer)
			if !ok {
				t.Fatalf("expected *bytes.Buffer stdout, got %T", cmd.Stdout)
			}
			_, err := buf.Write(stream)
			return err
		}

		err := auditExceptionsGlobalWithRunner([]string{"--global", "./..."}, runner, io.Discard)
		if err == nil {
			t.Fatal("expected non-nil error when globally stale patterns exist")
		}
	})

	t.Run("succeeds when no globally stale patterns are found", func(t *testing.T) {
		t.Parallel()
		stream := append(
			makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/a": {
					"goplint": {
						{Category: goplint.CategoryStaleException, Message: `stale exception: pattern "only.in.a" matched no diagnostics (reason: x)`},
					},
				},
			}),
			makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/b": {
					"goplint": {},
				},
			})...,
		)

		runner := func(cmd *exec.Cmd) error {
			buf, ok := cmd.Stdout.(*bytes.Buffer)
			if !ok {
				t.Fatalf("expected *bytes.Buffer stdout, got %T", cmd.Stdout)
			}
			_, err := buf.Write(stream)
			return err
		}

		if err := auditExceptionsGlobalWithRunner([]string{"--global", "./..."}, runner, io.Discard); err != nil {
			t.Fatalf("expected nil error when no globally stale patterns exist, got %v", err)
		}
	})
}

func TestFilterAndEnsureFlags(t *testing.T) {
	t.Parallel()

	t.Run("skip and prepend required flags", func(t *testing.T) {
		t.Parallel()
		got := filterAndEnsureFlags(
			[]string{"--drop", "-x", "./..."},
			func(trimmed string) bool { return trimmed == "drop" },
			[]string{"-a", "-b"},
		)
		want := []string{"-b", "-a", "-x", "./..."}
		if !slices.Equal(got, want) {
			t.Errorf("filterAndEnsureFlags() = %v, want %v", got, want)
		}
	})

	t.Run("does not duplicate existing required flags", func(t *testing.T) {
		t.Parallel()
		got := filterAndEnsureFlags(
			[]string{"-a", "-x"},
			func(string) bool { return false },
			[]string{"-a"},
		)
		want := []string{"-a", "-x"}
		if !slices.Equal(got, want) {
			t.Errorf("filterAndEnsureFlags() = %v, want %v", got, want)
		}
	})
}

func TestExtractPatternFromStaleMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "valid format",
			message: `stale exception: pattern "pkg.Type.Field" matched no diagnostics (reason: test)`,
			want:    "pkg.Type.Field",
		},
		{
			name:    "invalid format",
			message: "something else",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractPatternFromStaleMessage(tt.message)
			if got != tt.want {
				t.Errorf("extractPatternFromStaleMessage(%q) = %q, want %q", tt.message, got, tt.want)
			}
		})
	}
}

func TestExtractPatternFromStaleDiagnostic(t *testing.T) {
	t.Parallel()

	t.Run("prefers URL metadata", func(t *testing.T) {
		t.Parallel()
		diag := analysisDiagnostic{
			Category: goplint.CategoryStaleException,
			Message:  `stale exception: pattern "wrong" matched no diagnostics (reason: test)`,
			URL: goplint.DiagnosticURLForFindingWithMeta(
				goplint.StableFindingID(goplint.CategoryStaleException, "pkg.Type.Field"),
				map[string]string{"pattern": "pkg.Type.Field"},
			),
		}
		got := extractPatternFromStaleDiagnostic(diag)
		if got != "pkg.Type.Field" {
			t.Fatalf("extractPatternFromStaleDiagnostic() = %q, want %q", got, "pkg.Type.Field")
		}
	})

	t.Run("falls back to message parsing", func(t *testing.T) {
		t.Parallel()
		diag := analysisDiagnostic{
			Category: goplint.CategoryStaleException,
			Message:  `stale exception: pattern "pkg.Type.Legacy" matched no diagnostics (reason: legacy)`,
		}
		got := extractPatternFromStaleDiagnostic(diag)
		if got != "pkg.Type.Legacy" {
			t.Fatalf("extractPatternFromStaleDiagnostic() = %q, want %q", got, "pkg.Type.Legacy")
		}
	})
}

func TestTolerateAnalyzerExit(t *testing.T) {
	t.Parallel()

	t.Run("nil error accepted", func(t *testing.T) {
		t.Parallel()
		if err := tolerateAnalyzerExit(nil, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("exit error with json output accepted", func(t *testing.T) {
		t.Parallel()
		exitErr := makeExitError(t)
		stdout := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {"goplint": {}},
		})
		if err := tolerateAnalyzerExit(exitErr, stdout); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("exit error with empty output rejected", func(t *testing.T) {
		t.Parallel()
		exitErr := makeExitError(t)
		if err := tolerateAnalyzerExit(exitErr, nil); err == nil {
			t.Fatal("expected error for empty stdout")
		}
	})

	t.Run("exit error with malformed output rejected", func(t *testing.T) {
		t.Parallel()
		exitErr := makeExitError(t)
		if err := tolerateAnalyzerExit(exitErr, []byte("{invalid")); err == nil {
			t.Fatal("expected error for malformed analyzer stdout")
		}
	})
}

func TestGenerateBaseline(t *testing.T) {
	t.Parallel()

	t.Run("happy path writes baseline file", func(t *testing.T) {
		t.Parallel()

		runner := func(cmd *exec.Cmd) error {
			buf, ok := cmd.Stdout.(*bytes.Buffer)
			if !ok {
				t.Fatalf("expected *bytes.Buffer stdout, got %T", cmd.Stdout)
			}
			stream := makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/pkg": {"goplint": {}},
			})
			if _, err := buf.Write(stream); err != nil {
				return err
			}

			findingsPath := ""
			for _, arg := range cmd.Args {
				if after, ok := strings.CutPrefix(arg, "-emit-findings-jsonl="); ok {
					findingsPath = after
					break
				}
			}
			if findingsPath == "" {
				t.Fatal("expected -emit-findings-jsonl arg")
			}
			findings := []byte(strings.Join([]string{
				`{"category":"primitive","id":"id-primitive","message":"struct field pkg.A.B uses primitive type string","posn":"pkg/a.go:10:2"}`,
				`{"category":"unused-validate-result","id":"id-uvr-1","message":"Validate() result discarded — error return is unused","posn":"pkg/a.go:20:2"}`,
				`{"category":"unused-validate-result","id":"id-uvr-2","message":"Validate() result discarded — error return is unused","posn":"pkg/a.go:30:2"}`,
				`{"category":"stale-exception","id":"id-stale","message":"stale exception: pattern \"x\" matched no diagnostics (reason: y)","posn":"pkg/a.go:1:1"}`,
				"",
			}, "\n"))
			return os.WriteFile(findingsPath, findings, 0o644)
		}

		outPath := filepath.Join(t.TempDir(), "baseline.toml")
		if err := generateBaselineWithRunner(outPath, []string{"--update-baseline", outPath, "./..."}, runner, io.Discard); err != nil {
			t.Fatalf("generateBaseline() error = %v", err)
		}

		data, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("reading baseline output: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "[primitive]") {
			t.Fatal("expected primitive section in baseline output")
		}
		if !strings.Contains(content, "[unused-validate-result]") {
			t.Fatal("expected unused-validate-result section in baseline output")
		}
		if strings.Contains(content, "[stale-exception]") {
			t.Fatal("did not expect stale-exception section in baseline output")
		}
		if got := strings.Count(content, "Validate() result discarded — error return is unused"); got != 2 {
			t.Fatalf("expected 2 validate-usage entries, got %d", got)
		}
	})

	t.Run("malformed JSON stream returns parse error", func(t *testing.T) {
		t.Parallel()
		runner := func(cmd *exec.Cmd) error {
			buf, ok := cmd.Stdout.(*bytes.Buffer)
			if !ok {
				t.Fatalf("expected *bytes.Buffer stdout, got %T", cmd.Stdout)
			}
			if _, err := buf.Write(makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/pkg": {"goplint": {}},
			})); err != nil {
				return err
			}
			findingsPath := ""
			for _, arg := range cmd.Args {
				if after, ok := strings.CutPrefix(arg, "-emit-findings-jsonl="); ok {
					findingsPath = after
					break
				}
			}
			if findingsPath == "" {
				t.Fatal("expected -emit-findings-jsonl arg")
			}
			return os.WriteFile(findingsPath, []byte("{invalid\n"), 0o644)
		}
		outPath := filepath.Join(t.TempDir(), "baseline.toml")
		err := generateBaselineWithRunner(outPath, []string{"--update-baseline", outPath, "./..."}, runner, io.Discard)
		if err == nil {
			t.Fatal("expected parse error from malformed JSON stream")
		}
		if !errors.Is(err, ErrParsingAnalysisOutput) {
			t.Fatalf("expected parsing analysis output error, got %v", err)
		}
	})

	t.Run("empty findings stream with analyzer findings fails closed", func(t *testing.T) {
		t.Parallel()
		runner := func(cmd *exec.Cmd) error {
			buf, ok := cmd.Stdout.(*bytes.Buffer)
			if !ok {
				t.Fatalf("expected *bytes.Buffer stdout, got %T", cmd.Stdout)
			}
			if _, err := buf.Write(makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
				"example.com/pkg": {
					"goplint": {
						{
							Category: "primitive",
							Message:  "struct field pkg.A.B uses primitive type string",
							URL: goplint.DiagnosticURLForFinding(
								goplint.StableFindingID("primitive", "main_test", "empty-stream-check"),
							),
						},
					},
				},
			})); err != nil {
				return err
			}
			// Simulate sink write failure by leaving the findings stream empty.
			return &exec.ExitError{}
		}
		outPath := filepath.Join(t.TempDir(), "baseline.toml")
		err := generateBaselineWithRunner(outPath, []string{"--update-baseline", outPath, "./..."}, runner, io.Discard)
		if err == nil {
			t.Fatal("expected empty findings stream error")
		}
		if !errors.Is(err, ErrFindingsStreamEmpty) {
			t.Fatalf("expected fail-closed findings stream error, got %v", err)
		}
	})

	t.Run("non-exit command error is returned", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("boom")
		runner := func(*exec.Cmd) error { return expectedErr }

		outPath := filepath.Join(t.TempDir(), "baseline.toml")
		err := generateBaselineWithRunner(outPath, []string{"--update-baseline", outPath, "./..."}, runner, io.Discard)
		if err == nil {
			t.Fatal("expected command error")
		}
		if !errors.Is(err, ErrAnalyzerSubprocess) {
			t.Fatalf("expected wrapped subprocess error, got %v", err)
		}
	})
}

func TestUpdateBaselineRoundTripSuppression(t *testing.T) {
	t.Parallel()

	runner := func(cmd *exec.Cmd) error {
		buf, ok := cmd.Stdout.(*bytes.Buffer)
		if !ok {
			t.Fatalf("expected *bytes.Buffer stdout, got %T", cmd.Stdout)
		}
		if _, err := buf.Write(makeAnalysisJSON(t, map[string]map[string][]analysisDiagnostic{
			"example.com/pkg": {"goplint": {}},
		})); err != nil {
			return err
		}

		findingsPath := ""
		for _, arg := range cmd.Args {
			if after, ok := strings.CutPrefix(arg, "-emit-findings-jsonl="); ok {
				findingsPath = after
				break
			}
		}
		if findingsPath == "" {
			t.Fatal("expected -emit-findings-jsonl arg")
		}

		nameID := goplint.StableFindingID(
			goplint.CategoryPrimitive,
			"baseline_roundtrip",
			"struct-field",
			"baseline_roundtrip.RoundTrip.Name",
			"string",
		)
		ageID := goplint.StableFindingID(
			goplint.CategoryPrimitive,
			"baseline_roundtrip",
			"struct-field",
			"baseline_roundtrip.RoundTrip.Age",
			"int",
		)
		findings := []byte(strings.Join([]string{
			`{"category":"primitive","id":"` + nameID + `","message":"struct field baseline_roundtrip.RoundTrip.Name uses primitive type string","posn":"baseline_roundtrip.go:4:2"}`,
			`{"category":"primitive","id":"` + ageID + `","message":"struct field baseline_roundtrip.RoundTrip.Age uses primitive type int","posn":"baseline_roundtrip.go:5:2"}`,
			"",
		}, "\n"))
		return os.WriteFile(findingsPath, findings, 0o644)
	}

	outPath := filepath.Join(t.TempDir(), "baseline.toml")
	if err := generateBaselineWithRunner(outPath, []string{"--update-baseline=" + outPath, "./..."}, runner, io.Discard); err != nil {
		t.Fatalf("generateBaseline() error = %v", err)
	}

	analyzer := goplint.NewAnalyzer()
	if err := analyzer.Flags.Set("baseline", outPath); err != nil {
		t.Fatalf("setting baseline flag: %v", err)
	}

	testdata, err := filepath.Abs(filepath.Join("goplint", "testdata"))
	if err != nil {
		t.Fatalf("resolving testdata path: %v", err)
	}
	analysistest.Run(t, testdata, analyzer, "baseline_roundtrip")
}

// makeAnalysisJSON serializes the go/analysis -json output format for testing.
func makeAnalysisJSON(t *testing.T, result analysisResult) []byte {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshaling test JSON: %v", err)
	}
	return data
}

func makeExitError(t *testing.T) error {
	t.Helper()
	return &exec.ExitError{}
}
