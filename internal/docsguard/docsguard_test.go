// SPDX-License-Identifier: MPL-2.0

package docsguard

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	fixtureGuideDocument    = "docs/goplint/guide.md"
	fixtureEvidenceDocument = "docs/goplint/evidence-index.md"
)

var fixtureDocuments = []string{fixtureEvidenceDocument, fixtureGuideDocument}

func TestValidateDocumentsValidFixturePasses(t *testing.T) {
	t.Parallel()

	root := writeFixtureRepo(t, nil)
	report, err := ValidateDocuments(root, fixtureDocuments)
	if err != nil {
		t.Fatalf("ValidateDocuments: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("expected no findings for valid fixture, got %v", report.Findings)
	}
	if report.DocumentsChecked != len(fixtureDocuments) {
		t.Fatalf("DocumentsChecked = %d, want %d", report.DocumentsChecked, len(fixtureDocuments))
	}
	if report.AnchorsResolved == 0 {
		t.Fatal("expected resolved anchors for valid fixture, got zero")
	}
}

func TestValidateDocumentsFindsStaleReferences(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		overrides map[string]string
		want      Finding
	}{
		{
			name: "removed make target fails naming target, document, and line",
			overrides: map[string]string{
				"Makefile": ".PHONY: build\nbuild:\n\tgo build ./...\n",
			},
			want: Finding{
				Document:  fixtureGuideDocument,
				Line:      3,
				Reference: "check-goplint-soundness",
				Message:   "make target `check-goplint-soundness` is not declared in the repository Makefile",
			},
		},
		{
			name: "phony-only target is not a declaration",
			overrides: map[string]string{
				fixtureGuideDocument: "# Guide\n\nRun `make ghost-target` before review.\n",
			},
			want: Finding{
				Document:  fixtureGuideDocument,
				Line:      3,
				Reference: "ghost-target",
				Message:   "make target `ghost-target` is not declared in the repository Makefile",
			},
		},
		{
			name: "renamed subgate in claim row without other anchors is a dangling claim",
			overrides: map[string]string{
				"tools/goplint/spec/soundness-gate.v1.json": `{
  "format_version": 1,
  "profiles": [{"id": "semantic", "subgate_ids": ["full-scan-canonical"]}],
  "subgates": [{"id": "full-scan-canonical"}, {"id": "race-repeat-1"}]
}`,
			},
			want: Finding{
				Document:  fixtureEvidenceDocument,
				Line:      6,
				Reference: "Gate coverage",
				Message:   `dangling claim "Gate coverage": no referenced test, gate, path, or identifier exists in the current tree`,
			},
		},
		{
			name: "missing repository path fails naming the path",
			overrides: map[string]string{
				fixtureGuideDocument: "# Guide\n\nSee `docs/goplint/does-not-exist.md` for details.\n",
			},
			want: Finding{
				Document:  fixtureGuideDocument,
				Line:      3,
				Reference: "docs/goplint/does-not-exist.md",
				Message:   "repository path `docs/goplint/does-not-exist.md` does not exist in the current tree",
			},
		},
		{
			name: "claim row with no resolvable anchor is a dangling claim",
			overrides: map[string]string{
				fixtureEvidenceDocument: "# Evidence\n\n" +
					"| Guarantee | Live production boundary | Blocking proof surface |\n" +
					"|---|---|---|\n" +
					"| Unanchored guarantee | prose boundary text | prose proof text |\n",
			},
			want: Finding{
				Document:  fixtureEvidenceDocument,
				Line:      5,
				Reference: "Unanchored guarantee",
				Message:   `dangling claim "Unanchored guarantee": no referenced test, gate, path, or identifier exists in the current tree`,
			},
		},
		{
			name: "nonexistent path-shaped claim token is a hard failure",
			overrides: map[string]string{
				fixtureEvidenceDocument: "# Evidence\n\n" +
					"| Guarantee | Live production boundary | Blocking proof surface |\n" +
					"|---|---|---|\n" +
					"| Registry pathing | `spec/renamed-registry.v9.json` and `semanticCatalog` | typed registrations |\n",
			},
			want: Finding{
				Document:  fixtureEvidenceDocument,
				Line:      5,
				Reference: "spec/renamed-registry.v9.json",
				Message:   "claim references repository path `spec/renamed-registry.v9.json` that does not exist in the current tree",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := writeFixtureRepo(t, testCase.overrides)
			report, err := ValidateDocuments(root, fixtureDocuments)
			if err != nil {
				t.Fatalf("ValidateDocuments: %v", err)
			}
			if !slices.Contains(report.Findings, testCase.want) {
				t.Fatalf("findings %v do not contain %v", report.Findings, testCase.want)
			}
		})
	}
}

func TestFindingStringNamesDocumentLineAndMessage(t *testing.T) {
	t.Parallel()

	finding := Finding{
		Document:  fixtureGuideDocument,
		Line:      7,
		Reference: "check-types",
		Message:   "make target `check-types` is not declared in the repository Makefile",
	}
	want := "docs/goplint/guide.md:7: make target `check-types` is not declared in the repository Makefile"
	if finding.String() != want {
		t.Fatalf("Finding.String() = %q, want %q", finding.String(), want)
	}
}

func TestPathTokenHeuristics(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		reference string
		want      bool
	}{
		{name: "governed repository file", reference: "docs/goplint/evidence-index.md", want: true},
		{name: "trailing fragment stripped", reference: "docs/goplint/guide.md#profiles", want: true},
		{name: "glob token skipped", reference: "docs/goplint/*.md", want: false},
		{name: "ungoverned first segment skipped", reference: "website/docs/page.md", want: false},
		{name: "slash-less token skipped", reference: "evidence-index.md", want: false},
		{name: "url charset rejected", reference: "https://example.com/docs", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			normalized, eligible := normalizePathToken(testCase.reference)
			got := eligible && looksLikeRepoPath(normalized)
			if got != testCase.want {
				t.Fatalf("path interpretation of %q = %v, want %v", testCase.reference, got, testCase.want)
			}
		})
	}
}

func TestValidateRealRepositoryDocumentation(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "Makefile")); statErr != nil {
		t.Fatalf("repository root %s has no Makefile: %v", root, statErr)
	}
	report, validateErr := Validate(root)
	if validateErr != nil {
		t.Fatalf("Validate: %v", validateErr)
	}
	if len(report.Findings) != 0 {
		messages := make([]string, 0, len(report.Findings))
		for _, finding := range report.Findings {
			messages = append(messages, finding.String())
		}
		t.Fatalf("real repository documentation has stale references:\n%s", strings.Join(messages, "\n"))
	}
	if report.DocumentsChecked != len(DefaultDocuments) {
		t.Fatalf("DocumentsChecked = %d, want %d", report.DocumentsChecked, len(DefaultDocuments))
	}
	if report.AnchorsResolved == 0 {
		t.Fatal("expected resolved anchors in real repository documentation, got zero")
	}
}

func TestValidateLayoutRelocatedModule(t *testing.T) {
	t.Parallel()

	// A standalone-repository shape: the goplint module lives at the
	// repository root, so anchor artifacts resolve without a tools/goplint
	// prefix while governed docs keep their docs/goplint location.
	files := map[string]string{
		"Makefile": ".PHONY: build check-goplint-soundness\n" +
			"build:\n\tgo build ./...\n" +
			"check-goplint-soundness:\n\ttrue\n",
		"spec/soundness-gate.v1.json": `{
  "format_version": 1,
  "profiles": [{"id": "semantic", "subgate_ids": ["full-scan"]}],
  "subgates": [{"id": "full-scan"}]
}`,
		"spec/semantic-evidence.v2.json": `{"format_version": 2, "registrations": []}`,
		"goplint/analyzer.go": "// SPDX-License-Identifier: MPL-2.0\n\n" +
			"package goplint\n\ntype semanticCatalog struct{}\n",
		"docs/goplint/guide.md": "# Guide\n\n" +
			"Run `make check-goplint-soundness` from the root.\n" +
			"The manifest lives at `spec/soundness-gate.v1.json`.\n",
	}
	root := t.TempDir()
	for relPath, content := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create fixture directory for %s: %v", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture file %s: %v", relPath, err)
		}
	}

	layout := Layout{
		ModuleDir:        ".",
		GateManifest:     "spec/soundness-gate.v1.json",
		EvidenceRegistry: "spec/semantic-evidence.v2.json",
		SpecDir:          "spec",
		Documents:        []string{"docs/goplint/guide.md"},
	}
	report, err := ValidateLayout(root, layout)
	if err != nil {
		t.Fatalf("ValidateLayout: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("expected no findings for relocated layout, got %v", report.Findings)
	}
	if report.DocumentsChecked != 1 {
		t.Fatalf("DocumentsChecked = %d, want 1", report.DocumentsChecked)
	}
	if report.AnchorsResolved == 0 {
		t.Fatal("expected resolved anchors for relocated layout, got zero")
	}
}

func TestDefaultLayoutMatchesCanonicalRepository(t *testing.T) {
	t.Parallel()

	layout := DefaultLayout()
	if layout.ModuleDir != "tools/goplint" {
		t.Fatalf("ModuleDir = %q, want tools/goplint", layout.ModuleDir)
	}
	if !slices.Equal(layout.Documents, DefaultDocuments) {
		t.Fatalf("Documents = %v, want DefaultDocuments", layout.Documents)
	}
	// The default layout must be a defensive copy, not an aliased slice.
	layout.Documents[0] = "mutated"
	if DefaultDocuments[0] == "mutated" {
		t.Fatal("DefaultLayout aliases DefaultDocuments instead of copying it")
	}
}

// writeFixtureRepo materializes a minimal synthetic repository with a
// Makefile, soundness manifests, Go sources, and governed documents, then
// applies the per-case overrides.
func writeFixtureRepo(t *testing.T, overrides map[string]string) string {
	t.Helper()

	files := map[string]string{
		"Makefile": ".PHONY: build check-goplint-soundness ghost-target\n" +
			"ROOT_DIR:=/tmp\n" +
			"build: deps\n\tgo build ./...\n" +
			"check-goplint-soundness:\n\ttrue\n",
		"tools/goplint/spec/soundness-gate.v1.json": `{
  "format_version": 1,
  "profiles": [{"id": "semantic", "subgate_ids": ["full-scan"]}],
  "subgates": [{"id": "full-scan"}, {"id": "race-repeat-1"}]
}`,
		"tools/goplint/spec/semantic-evidence.v2.json": `{
  "format_version": 2,
  "registrations": [
    {
      "id": "unvalidated-cast.mutation",
      "category": "unvalidated-cast",
      "layer": "mutation",
      "feature_id": "cast-validation",
      "producer_id": "targeted-mutation",
      "test_id": "TestFixtureEvidence/unvalidated-cast",
      "boundary": "analyzer",
      "expected": {
        "outcome": "caught",
        "required_stages": ["reporting"],
        "required_properties": ["property-checked"],
        "required_dimensions": ["cast-dimension"]
      }
    }
  ]
}`,
		"tools/goplint/goplint/analyzer.go": "// SPDX-License-Identifier: MPL-2.0\n\n" +
			"package goplint\n\ntype semanticCatalog struct{}\n\nfunc routeDiagnostics() semanticCatalog { return semanticCatalog{} }\n",
		"pkg/types/doc.go":   "// SPDX-License-Identifier: MPL-2.0\n\npackage types\n",
		fixtureGuideDocument: fixtureGuideContent(),
		fixtureEvidenceDocument: "# Evidence\n\n" +
			"| Guarantee | Live production boundary | Blocking proof surface |\n" +
			"|---|---|---|\n" +
			"| Catalog ownership | `semanticCatalog`, `routeDiagnostics` | typed category observations |\n" +
			"| Gate coverage | gate populations | `full-scan` subgate census |\n" +
			"| Command surface | `make check-goplint-soundness` routing | routed profile execution |\n" +
			"| Registry pathing | `spec/semantic-evidence.v2.json` | `TestFixtureEvidence/unvalidated-cast` registrations |\n",
	}
	maps.Copy(files, overrides)

	root := t.TempDir()
	for relPath, content := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create fixture directory for %s: %v", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture file %s: %v", relPath, err)
		}
	}
	return root
}

// fixtureGuideContent is the default non-table document; line numbers are
// asserted by the stale-reference cases.
func fixtureGuideContent() string {
	return "# Guide\n" + // line 1
		"\n" + // line 2
		"Run `make build` and `make check-goplint-soundness` from the root.\n" + // line 3
		"The manifest lives at `tools/goplint/spec/soundness-gate.v1.json`.\n" + // line 4
		"Race groups run as `race-repeat-1/6` workers.\n" + // line 5
		"See `pkg/types.FilesystemPath` for CUE-fed path values.\n" + // line 6
		"\n" + // line 7
		"```bash\n" + // line 8
		"make build\n" + // line 9
		"```\n" // line 10
}
