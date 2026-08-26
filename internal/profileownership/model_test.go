// SPDX-License-Identifier: MPL-2.0

package profileownership

import (
	"testing"

	"github.com/invowk/goplint/internal/soundnessgate"
)

func testManifest() Manifest {
	return Manifest{FormatVersion: FormatVersion, Rules: []Rule{
		{Pattern: ".github/workflows/**", Class: ClassHarness},
		{Pattern: "AGENTS.md", Class: ClassDocumentation},
		{Pattern: "cmd/**", Class: ClassConsumer},
		{Pattern: "docs/goplint/**", Class: ClassDocumentation},
		{Pattern: "internal/**", Class: ClassConsumer},
		{Pattern: "openspec/changes/**", Class: ClassDocumentation},
		{Pattern: "openspec/changes/archive/2026-07-19-close-goplint-soundness-review-gaps/evidence/**", Class: ClassAnalyzerSemantics},
		{Pattern: "openspec/changes/archive/2026-07-19-close-residual-goplint-soundness-gaps/evidence/**", Class: ClassAnalyzerSemantics},
		{Pattern: "tools/goplint/**", Class: ClassAnalyzerSemantics},
		{Pattern: "tools/goplint/CLAUDE.md", Class: ClassDocumentation},
		{Pattern: "tools/goplint/internal/soundnessgate/**", Class: ClassHarness},
	}}
}

func TestRouteConservativelyClassifiesEveryGovernedContext(t *testing.T) {
	t.Parallel()

	manifest := testManifest()
	tests := []struct {
		name  string
		input Context
		want  soundnessgate.ProfileID
	}{
		{name: "documentation only", input: changed("pull_request", "docs/goplint/evidence-index.md", "AGENTS.md"), want: soundnessgate.ProfileDocumentation},
		{name: "openspec change artifacts", input: changed("pre_commit", "openspec/changes/some-change/tasks.md", "openspec/changes/some-change/proposal.md"), want: soundnessgate.ProfileDocumentation},
		{name: "goplint markdown exact override", input: changed("pull_request", "tools/goplint/CLAUDE.md"), want: soundnessgate.ProfileDocumentation},
		{name: "consumer", input: changed("pull_request", "cmd/invowk/main.go", "internal/config/config.go"), want: soundnessgate.ProfileConsumer},
		{name: "documentation plus consumer", input: changed("pull_request", "docs/goplint/evidence-index.md", "cmd/invowk/main.go"), want: soundnessgate.ProfileConsumer},
		{name: "harness workflow", input: changed("pull_request", ".github/workflows/lint.yml"), want: soundnessgate.ProfileHarness},
		{name: "harness executor subfamily", input: changed("pull_request", "tools/goplint/internal/soundnessgate/scheduler.go"), want: soundnessgate.ProfileHarness},
		{name: "consumer plus harness", input: changed("pull_request", "internal/config/config.go", ".github/workflows/lint.yml"), want: soundnessgate.ProfileHarness},
		{name: "documentation plus harness", input: changed("pre_commit", "docs/goplint/evidence-index.md", ".github/workflows/lint.yml"), want: soundnessgate.ProfileHarness},
		{name: "analyzer semantics", input: changed("pull_request", "tools/goplint/goplint/analyzer.go"), want: soundnessgate.ProfileSemantic},
		{name: "harness plus analyzer semantics", input: changed("pull_request", "tools/goplint/internal/soundnessgate/scheduler.go", "tools/goplint/goplint/analyzer.go"), want: soundnessgate.ProfileSemantic},
		{name: "retained evidence carve-out", input: changed("pull_request", "openspec/changes/archive/2026-07-19-close-residual-goplint-soundness-gaps/evidence/targeted-mutation-run.v1.json"), want: soundnessgate.ProfileSemantic},
		{name: "multi area", input: changed("pull_request", "cmd/invowk/main.go", "spec/semantic-rules.v1.json"), want: soundnessgate.ProfileSemantic},
		{name: "rename deletion paths", input: changed("push", "cmd/old.go", "cmd/new.go"), want: soundnessgate.ProfileConsumer},
		{name: "rename across classes", input: changed("push", "openspec/changes/some-change/tasks.md", "openspec/changes/archive/2026-07-23-some-change/tasks.md"), want: soundnessgate.ProfileDocumentation},
		{name: "unknown", input: changed("pull_request", "README.md"), want: soundnessgate.ProfileSemantic},
		{name: "empty", input: changed("pull_request"), want: soundnessgate.ProfileSemantic},
		{name: "missing merge base", input: Context{Event: "pull_request", ChangedPaths: []string{"cmd/main.go"}}, want: soundnessgate.ProfileSemantic},
		{name: "shallow", input: Context{Event: "pull_request", ChangedPaths: []string{"cmd/main.go"}, MergeBaseAvailable: true, ShallowRepository: true}, want: soundnessgate.ProfileSemantic},
		{name: "invalid path", input: changed("pull_request", "../escape.go"), want: soundnessgate.ProfileSemantic},
		{name: "dispatch", input: changed("workflow_dispatch", "docs/goplint/evidence-index.md"), want: soundnessgate.ProfileComplete},
		{name: "schedule", input: changed("schedule", "docs/goplint/evidence-index.md"), want: soundnessgate.ProfileComplete},
		{name: "release", input: changed("release", "cmd/main.go"), want: soundnessgate.ProfileComplete},
		{name: "completion", input: changed("completion", "docs/goplint/evidence-index.md"), want: soundnessgate.ProfileComplete},
		{name: "unknown event", input: changed("mystery", "docs/goplint/evidence-index.md"), want: soundnessgate.ProfileSemantic},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			decision, err := manifest.Route(testCase.input)
			if err != nil {
				t.Fatalf("Route() error = %v", err)
			}
			if decision.Profile != testCase.want {
				t.Fatalf("Route() profile = %q, want %q; reason = %s", decision.Profile, testCase.want, decision.Reason)
			}
		})
	}
}

func TestRealManifestRoutesEveryGovernedFamily(t *testing.T) {
	t.Parallel()

	manifest, err := Load("../../spec/soundness-ownership.v2.json")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	tests := []struct {
		path string
		want soundnessgate.ProfileID
	}{
		{path: ".github/workflows/lint.yml", want: soundnessgate.ProfileHarness},
		{path: ".gitignore", want: soundnessgate.ProfileHarness},
		{path: ".golangci.toml", want: soundnessgate.ProfileHarness},
		{path: ".pre-commit-config.yaml", want: soundnessgate.ProfileHarness},
		{path: "AGENTS.md", want: soundnessgate.ProfileDocumentation},
		{path: "CLAUDE.md", want: soundnessgate.ProfileDocumentation},
		{path: "LICENSE", want: soundnessgate.ProfileHarness},
		{path: "Makefile", want: soundnessgate.ProfileHarness},
		{path: "README.md", want: soundnessgate.ProfileDocumentation},
		{path: "baseline.toml", want: soundnessgate.ProfileConsumer},
		{path: "bench/thresholds.toml", want: soundnessgate.ProfileSemantic},
		{path: "cmd/repository-audit/main.go", want: soundnessgate.ProfileSemantic},
		{path: "cmd/soundness-gate/main.go", want: soundnessgate.ProfileHarness},
		{path: "cmd/soundness-profile/main.go", want: soundnessgate.ProfileSemantic},
		{path: "cmd/soundness-report-compare/main.go", want: soundnessgate.ProfileHarness},
		{path: "docs/goplint/evidence-index.md", want: soundnessgate.ProfileDocumentation},
		{path: "docs/goplint/soundness-gate-performance.md", want: soundnessgate.ProfileDocumentation},
		{path: "exceptions.toml", want: soundnessgate.ProfileConsumer},
		{path: "go.mod", want: soundnessgate.ProfileSemantic},
		{path: "go.sum", want: soundnessgate.ProfileSemantic},
		{path: "goplint/analyzer.go", want: soundnessgate.ProfileSemantic},
		{path: "inconclusive_orchestration_test.go", want: soundnessgate.ProfileSemantic},
		{path: "internal/cleantreeevidence/model.go", want: soundnessgate.ProfileSemantic},
		{path: "internal/profileownership/model.go", want: soundnessgate.ProfileSemantic},
		{path: "internal/soundnessgate/scheduler.go", want: soundnessgate.ProfileHarness},
		{path: "main.go", want: soundnessgate.ProfileSemantic},
		{path: "scripts/check-routed-soundness.sh", want: soundnessgate.ProfileHarness},
		{path: "spec/protocol-domain.md", want: soundnessgate.ProfileSemantic},
		{path: "spec/semantic-rules.v1.json", want: soundnessgate.ProfileSemantic},
		{path: "testdata/gates/clean-tree-v5.json", want: soundnessgate.ProfileSemantic},
	}
	for _, testCase := range tests {
		t.Run(testCase.path, func(t *testing.T) {
			t.Parallel()
			decision, err := manifest.Route(changed("pull_request", testCase.path))
			if err != nil {
				t.Fatalf("Route() error = %v", err)
			}
			if decision.Profile != testCase.want {
				t.Fatalf("Route(%q) profile = %q, want %q; reason = %s", testCase.path, decision.Profile, testCase.want, decision.Reason)
			}
		})
	}
}

func TestValidateRejectsDocumentationOverExecutableInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rules []Rule
	}{
		{name: "documentation family inside executable family", rules: []Rule{
			{Pattern: "spec/**", Class: ClassDocumentation},
		}},
		{name: "documentation family engulfing executable family without cover", rules: []Rule{
			{Pattern: "testdata/**", Class: ClassDocumentation},
		}},
		{name: "documentation family engulfing evidence archives without cover", rules: []Rule{
			{Pattern: "spec/evidence/**", Class: ClassDocumentation},
		}},
		{name: "documentation family engulfing executable exact path", rules: []Rule{
			{Pattern: "go.mod", Class: ClassDocumentation},
			{Pattern: "go.sum", Class: ClassConsumer},
		}},
		{name: "exact documentation path is not markdown", rules: []Rule{
			{Pattern: "baseline.toml", Class: ClassDocumentation},
		}},
		{name: "exact markdown inside executable family", rules: []Rule{
			{Pattern: "testdata/fuzz/README.md", Class: ClassDocumentation},
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			manifest := Manifest{FormatVersion: FormatVersion, Rules: testCase.rules}
			if err := manifest.Validate(); err == nil {
				t.Fatalf("Validate() accepted documentation classification over executable inputs: %+v", testCase.rules)
			}
		})
	}
}

func TestValidateAcceptsDocumentationWithExecutableCarveOuts(t *testing.T) {
	t.Parallel()

	manifest := Manifest{FormatVersion: FormatVersion, Rules: []Rule{
		{Pattern: "openspec/changes/**", Class: ClassDocumentation},
		{Pattern: "openspec/changes/archive/2026-07-19-close-goplint-soundness-review-gaps/evidence/**", Class: ClassAnalyzerSemantics},
		{Pattern: "openspec/changes/archive/2026-07-19-close-residual-goplint-soundness-gaps/evidence/**", Class: ClassAnalyzerSemantics},
	}}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() rejected documentation family with executable carve-outs: %v", err)
	}
}

func TestValidateRejectsMalformedManifests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest Manifest
	}{
		{name: "wrong version", manifest: Manifest{FormatVersion: 1, Rules: []Rule{{Pattern: "cmd/**", Class: ClassConsumer}}}},
		{name: "empty rules", manifest: Manifest{FormatVersion: FormatVersion}},
		{name: "unknown class", manifest: Manifest{FormatVersion: FormatVersion, Rules: []Rule{{Pattern: "cmd/**", Class: Class("core")}}}},
		{name: "legacy profile value", manifest: Manifest{FormatVersion: FormatVersion, Rules: []Rule{{Pattern: "tools/goplint/**", Class: Class("semantic")}}}},
		{name: "unsorted", manifest: Manifest{FormatVersion: FormatVersion, Rules: []Rule{
			{Pattern: "internal/**", Class: ClassConsumer},
			{Pattern: "cmd/**", Class: ClassConsumer},
		}}},
		{name: "duplicate", manifest: Manifest{FormatVersion: FormatVersion, Rules: []Rule{
			{Pattern: "cmd/**", Class: ClassConsumer},
			{Pattern: "cmd/**", Class: ClassHarness},
		}}},
		{name: "unsafe pattern", manifest: Manifest{FormatVersion: FormatVersion, Rules: []Rule{{Pattern: "../cmd/**", Class: ClassConsumer}}}},
		{name: "mid-pattern wildcard", manifest: Manifest{FormatVersion: FormatVersion, Rules: []Rule{{Pattern: "cmd/*/main.go", Class: ClassConsumer}}}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if err := testCase.manifest.Validate(); err == nil {
				t.Fatal("Validate() accepted a malformed manifest")
			}
		})
	}
}

func TestClassForPathPrefersMostSpecificRule(t *testing.T) {
	t.Parallel()

	manifest := testManifest()
	tests := []struct {
		path string
		want Class
	}{
		{path: "tools/goplint/CLAUDE.md", want: ClassDocumentation},
		{path: "tools/goplint/internal/soundnessgate/model.go", want: ClassHarness},
		{path: "tools/goplint/internal/profileownership/model.go", want: ClassAnalyzerSemantics},
		{path: "openspec/changes/archive/2026-07-19-close-residual-goplint-soundness-gaps/evidence/targeted-mutation-run.v1.json", want: ClassAnalyzerSemantics},
		{path: "openspec/changes/archive/2026-07-19-close-residual-goplint-soundness-gaps/tasks.md", want: ClassDocumentation},
	}
	for _, testCase := range tests {
		t.Run(testCase.path, func(t *testing.T) {
			t.Parallel()
			class, matched := manifest.ClassForPath(testCase.path)
			if !matched {
				t.Fatalf("ClassForPath(%q) matched = false", testCase.path)
			}
			if class != testCase.want {
				t.Fatalf("ClassForPath(%q) = %q, want %q", testCase.path, class, testCase.want)
			}
		})
	}
	if _, matched := manifest.ClassForPath("website/docs/index.md"); matched {
		t.Fatal("ClassForPath() matched an ungoverned path")
	}
}

func changed(event string, paths ...string) Context {
	return Context{Event: event, ChangedPaths: paths, MergeBaseAvailable: true}
}
