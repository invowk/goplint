// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

type realDeterminismPackage struct {
	path    string
	sources []string
}

type realDeterminismPackageRun struct {
	Records []FindingStreamRecord `json:"records"`
	Facts   []FindingStreamRecord `json:"facts"`
}

type realDeterminismRun struct {
	packages map[string]realDeterminismPackageRun
}

type realDeterminismOptions struct {
	reverseFiles    bool
	reverseTypeMaps bool
	parallel        bool
	worklistOrder   protocolWorklistOrder
}

type realDeterminismOrderControl struct {
	order protocolWorklistOrder
}

func (realDeterminismOrderControl) Expired() bool {
	return false
}

func (control realDeterminismOrderControl) protocolWorklistOrder() protocolWorklistOrder {
	return control.order
}

func TestRealAnalyzerDeterminismAcrossPackageFileAndScheduleOrder(t *testing.T) {
	t.Parallel()
	runRealAnalyzerDeterminismMatrix(t)
}

func TestCategoryDeterminismEvidence(t *testing.T) {
	t.Parallel()

	registrations := semanticEvidenceRegistrations(t)
	catalog := mustLoadSemanticRuleCatalog(t)
	for _, category := range protocolEvidenceCategories() {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			forward := executeNonCastCategoryEvidence(t, category, false)
			reordered := executeNonCastCategoryEvidence(t, category, true)
			if !reflect.DeepEqual(forward.production.hits, reordered.production.hits) ||
				forward.cases != reordered.cases || forward.diagnostics != reordered.diagnostics {
				t.Fatalf("category %q changed under equivalent fixture schedule", category)
			}
			trace := forward.trace
			trace.merge(reordered.trace)
			trace.addProperties("equivalent-schedules-compared", "production-analyzer-executed")
			trace.addDimensions(
				"equivalent-schedules",
				semanticFeatureForCategory(t, category),
			)
			registration := registrations[category+"."+string(soundnessevidence.LayerDeterminism)]
			oracle := semanticOraclesByCategory(catalog)[category]
			cases := categoryDeterminismCases(
				t,
				registration,
				category,
				forward,
				reordered,
				oracle,
				trace,
			)
			emitSemanticObservation(t, semanticObservationFromCases(
				t,
				registration.ID,
				"",
				t.Name(),
				cases,
			))
		})
	}
}

func categoryDeterminismCases(
	t *testing.T,
	registration soundnessevidence.Registration,
	category string,
	forward,
	reordered nonCastCategoryExecution,
	oracle semanticOracleSpec,
	trace generatedExecutionTrace,
) []soundnessevidence.SemanticCase {
	t.Helper()

	properties := trace.sortedProperties()
	dimensions := trace.sortedDimensions()
	cases := make([]soundnessevidence.SemanticCase, 0, forward.cases)
	appendEntries := func(prefix, diagnosticCategory string, entries []semanticOracleEntry) {
		for _, entry := range entries {
			caseKey := entry.Fixture + "\x00" + entry.Symbol
			caseTrace := generatedExecutionTrace{}
			for _, stage := range forward.production.traces.stagesForCase(t, category, entry.Fixture) {
				caseTrace.addStage(stage)
			}
			for _, stage := range reordered.production.traces.stagesForCase(t, category, entry.Fixture) {
				caseTrace.addStage(stage)
			}
			cases = append(cases, soundnessevidence.SemanticCase{
				ID:        prefix + "/" + entry.Fixture + "/" + entry.Symbol,
				Category:  category,
				Layer:     soundnessevidence.LayerDeterminism,
				FeatureID: registration.FeatureID,
				Boundary:  soundnessevidence.BoundaryDeterminismAnalyzer,
				ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
					soundnessevidence.BoundaryDeterminismAnalyzer,
					soundnessevidence.BoundaryProductionAnalyzer,
				),
				Outcome: soundnessevidence.OutcomeDeterministic,
				DiagnosticCount: forward.production.hits[diagnosticCategory][caseKey] +
					reordered.production.hits[diagnosticCategory][caseKey],
				Stages:     caseTrace.orderedStages(),
				Properties: slices.Clone(properties),
				Dimensions: slices.Clone(dimensions),
			})
		}
	}
	appendEntries("must-report", category, oracle.MustReport)
	appendEntries("must-not-report", category, oracle.MustNotReport)
	appendEntries("must-be-inconclusive", semanticInconclusiveCategory(category), oracle.MustBeInconclusive)
	slices.SortFunc(cases, func(left, right soundnessevidence.SemanticCase) int {
		return strings.Compare(left.ID, right.ID)
	})
	return cases
}

func runRealAnalyzerDeterminismMatrix(t *testing.T) int {
	t.Helper()

	packages := realDeterminismPackages()
	forward, err := runRealAnalyzerDeterminism(t.TempDir(), packages, realDeterminismOptions{})
	if err != nil {
		t.Fatalf("forward real-analyzer run: %v", err)
	}
	reversed := slices.Clone(packages)
	slices.Reverse(reversed)
	reordered, err := runRealAnalyzerDeterminism(t.TempDir(), reversed, realDeterminismOptions{
		reverseFiles:    true,
		reverseTypeMaps: true,
	})
	if err != nil {
		t.Fatalf("reordered real-analyzer run: %v", err)
	}
	parallel, err := runRealAnalyzerDeterminism(t.TempDir(), reversed, realDeterminismOptions{
		reverseFiles:    true,
		reverseTypeMaps: true,
		parallel:        true,
		worklistOrder:   protocolWorklistLIFO,
	})
	if err != nil {
		t.Fatalf("parallel real-analyzer run: %v", err)
	}

	assertRealDeterminismEvidenceNonVacuous(t, forward)
	want := rawRealAnalyzerEnvelope(forward)
	for name, run := range map[string]realDeterminismRun{
		"package-file-and-map-reordered": reordered,
		"parallel-lifo-schedule":         parallel,
	} {
		assertRealDeterminismEvidenceNonVacuous(t, run)
		if got := rawRealAnalyzerEnvelope(run); !bytes.Equal(got, want) {
			t.Fatalf("%s real-analyzer envelope differs:\nwant: %s\n got: %s", name, want, got)
		}
	}
	return 3
}

func realDeterminismPackages() []realDeterminismPackage {
	return []realDeterminismPackage{
		{
			path: "determinism.example/alpha",
			sources: []string{
				`package alpha
type Name string
func (n Name) Validate() error { return nil }
func (n Name) String() string { return string(n) }
func useName(Name) {}
type mutator interface { Apply(*Name) }
`,
				`package alpha
func Unsafe(raw string) Name { return Name(raw) }
func Keep(n Name) Name { return n }
func Safe(raw string) (Name, error) {
		n := Name(raw)
		if err := n.Validate(); err != nil { return "", err }
		return n, nil
}
var Stored = func(raw string) {
	n := Name(raw)
	useName(n)
}
func NestedCapture(raw string) {
	_ = func() {
		_ = func() {
			n := Name(raw)
			useName(n)
		}
	}
}
func recursivePreserve(n Name, depth int) error {
	if depth <= 0 { return nil }
	return recursivePreserve(n, depth-1)
}
func RecursiveUnsafe(raw string, depth int) error {
	n := Name(raw)
	return recursivePreserve(n, depth)
}
func JoinedUnsafe(raw string) {
	n := Name(raw)
	if raw == "" {
		if raw != "" {
			if err := n.Validate(); err != nil { return }
		}
	}
	useName(n)
}
func InfeasibleUnsafe(raw string) error {
	n := Name(raw)
	if raw == "" {
		if raw != "" {
			useName(n)
			return nil
		}
	}
	if err := n.Validate(); err != nil { return err }
	return nil
}
func UnknownAfterValidation(value mutator, raw string) error {
	n := Name(raw)
	if err := n.Validate(); err != nil { return err }
	value.Apply(&n)
	return nil
}
`,
			},
		},
		{
			path: "determinism.example/beta",
			sources: []string{
				`package beta
type Token string
func (t Token) Validate() error { return nil }
func (t Token) String() string { return string(t) }
`,
				`package beta
func Rebound(first, second string) Token {
		t := Token(first)
		_ = t.Validate()
		t = Token(second)
		return t
}
func ValidateToken(token Token) error { return token.Validate() }
func useToken(Token) {}
func Branch(raw string, ok bool) {
	t := Token(raw)
	if ok {
		if err := t.Validate(); err != nil { return }
	}
	useToken(t)
}
`,
			},
		},
	}
}

func runRealAnalyzerDeterminism(
	root string,
	packages []realDeterminismPackage,
	options realDeterminismOptions,
) (realDeterminismRun, error) {
	type indexedRun struct {
		index int
		run   realDeterminismPackageRun
		err   error
	}
	results := make(chan indexedRun, len(packages))
	var wait sync.WaitGroup
	for index := range packages {
		runPackage := func() {
			run, err := runRealAnalyzerPackage(root, packages[index], options)
			results <- indexedRun{index: index, run: run, err: err}
		}
		if options.parallel {
			wait.Go(func() {
				runPackage()
			})
			continue
		}
		runPackage()
	}
	wait.Wait()
	close(results)

	ordered := make([]realDeterminismPackageRun, len(packages))
	for result := range results {
		if result.err != nil {
			return realDeterminismRun{}, result.err
		}
		ordered[result.index] = result.run
	}
	combined := realDeterminismRun{packages: make(map[string]realDeterminismPackageRun, len(packages))}
	for index, run := range ordered {
		combined.packages[packages[index].path] = run
	}
	return combined, nil
}

func runRealAnalyzerPackage(
	root string,
	spec realDeterminismPackage,
	options realDeterminismOptions,
) (realDeterminismPackageRun, error) {
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(spec.sources))
	for index, source := range spec.sources {
		filename := fmt.Sprintf("%s/file-%d.go", spec.path, index)
		file, err := parser.ParseFile(fset, filename, source, parser.ParseComments)
		if err != nil {
			return realDeterminismPackageRun{}, fmt.Errorf("parse %s: %w", filename, err)
		}
		files = append(files, file)
	}
	if options.reverseFiles {
		slices.Reverse(files)
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Instances:  make(map[*ast.Ident]types.Instance),
	}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check(spec.path, fset, files, info)
	if err != nil {
		return realDeterminismPackageRun{}, fmt.Errorf("type-check %s: %w", spec.path, err)
	}
	if options.reverseTypeMaps {
		rebuildRealDeterminismTypeMaps(info)
	}

	findingsPath := filepath.Join(root, stableFileComponent(spec.path)+".jsonl")
	var facts []FindingStreamRecord
	pass := &analysis.Pass{
		Fset:      fset,
		Files:     files,
		Pkg:       pkg,
		TypesInfo: info,
		ResultOf: map[*analysis.Analyzer]any{
			inspect.Analyzer: inspector.New(files),
		},
		Report: func(analysis.Diagnostic) {},
		ExportObjectFact: func(object types.Object, fact analysis.Fact) {
			encoded, marshalErr := json.Marshal(fact)
			if marshalErr != nil {
				return
			}
			facts = append(facts, FindingStreamRecord{
				Kind:    "analysis-fact",
				ID:      objectKey(object),
				Message: string(encoded),
			})
		},
		ImportObjectFact: func(types.Object, analysis.Fact) bool { return false },
	}
	state := &flagState{}
	analyzer := newAnalyzerWithState(state)
	state.protocolControlFactory = func(time.Duration) protocolAnalysisControl {
		return realDeterminismOrderControl{order: options.worklistOrder}
	}
	if err := analyzer.Flags.Set("check-all", "true"); err != nil {
		return realDeterminismPackageRun{}, fmt.Errorf("enable check-all: %w", err)
	}
	if err := analyzer.Flags.Set("emit-findings-jsonl", findingsPath); err != nil {
		return realDeterminismPackageRun{}, fmt.Errorf("set findings path: %w", err)
	}
	if _, err := analyzer.Run(pass); err != nil {
		return realDeterminismPackageRun{}, fmt.Errorf("run analyzer for %s: %w", spec.path, err)
	}
	data, err := os.ReadFile(findingsPath)
	if err != nil {
		return realDeterminismPackageRun{}, fmt.Errorf("read findings for %s: %w", spec.path, err)
	}
	var records []FindingStreamRecord
	if err := forEachFindingsRecord(data, func(record FindingStreamRecord) error {
		records = append(records, record)
		return nil
	}); err != nil {
		return realDeterminismPackageRun{}, fmt.Errorf("decode findings for %s: %w", spec.path, err)
	}
	return realDeterminismPackageRun{Records: records, Facts: facts}, nil
}

func rawRealAnalyzerEnvelope(run realDeterminismRun) []byte {
	encoded, err := json.Marshal(run.packages)
	if err != nil {
		panic("marshal raw real analyzer determinism envelope: " + err.Error())
	}
	return encoded
}

func assertRealDeterminismEvidenceNonVacuous(t *testing.T, run realDeterminismRun) {
	t.Helper()

	var findings, facts, reasons, qualifiedWitnesses, summaries, summaryReuses, refinements, evidenceDigests int
	for _, packageRun := range run.packages {
		facts += len(packageRun.Facts)
		for _, record := range packageRun.Records {
			if record.Kind == findingStreamKindRefinementTrace {
				refinements++
				if record.Meta["cfg_refinement_evidence_digest"] != "" {
					evidenceDigests++
				}
				if record.Meta["cfg_refinement_witness_hash"] == "" {
					t.Fatal("real-analyzer refinement trace omitted its qualified witness hash")
				}
				continue
			}
			findings++
			if record.Meta["cfg_inconclusive_reason"] != "" {
				reasons++
			}
			if chain := record.Meta["cfg_witness_call_chain"]; chain != "" && bytes.Contains([]byte(chain), []byte(".")) {
				qualifiedWitnesses++
			}
			if count, err := strconv.Atoi(record.Meta["cfg_tabulation_summaries"]); err == nil && count > 0 {
				summaries++
			}
			if count, err := strconv.Atoi(record.Meta["cfg_tabulation_summary_reuses"]); err == nil && count > 0 {
				summaryReuses++
			}
		}
	}
	if findings == 0 || facts == 0 || reasons == 0 || qualifiedWitnesses == 0 || summaries == 0 ||
		summaryReuses == 0 || refinements == 0 || evidenceDigests == 0 {
		t.Fatalf(
			"real-analyzer determinism evidence is vacuous: findings=%d facts=%d reasons=%d qualified_witnesses=%d summaries=%d summary_reuses=%d refinements=%d evidence_digests=%d",
			findings,
			facts,
			reasons,
			qualifiedWitnesses,
			summaries,
			summaryReuses,
			refinements,
			evidenceDigests,
		)
	}
}

func rebuildRealDeterminismTypeMaps(info *types.Info) {
	info.Types = rebuildRealDeterminismPositionMap(info.Types)
	info.Defs = rebuildRealDeterminismPositionMap(info.Defs)
	info.Uses = rebuildRealDeterminismPositionMap(info.Uses)
	info.Selections = rebuildRealDeterminismPositionMap(info.Selections)
	info.Instances = rebuildRealDeterminismPositionMap(info.Instances)
}

func rebuildRealDeterminismPositionMap[K interface {
	comparable
	Pos() token.Pos
}, V any](source map[K]V) map[K]V {
	keys := make([]K, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		return keys[left].Pos() > keys[right].Pos()
	})
	rebuilt := make(map[K]V, len(source))
	for _, key := range keys {
		rebuilt[key] = source[key]
	}
	return rebuilt
}

func stableFileComponent(value string) string {
	result := make([]byte, 0, len(value))
	for index := range len(value) {
		char := value[index]
		if char == '/' || char == '.' {
			char = '-'
		}
		result = append(result, char)
	}
	return string(result)
}
