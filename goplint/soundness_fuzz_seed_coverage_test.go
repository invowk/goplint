// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/protocoloracle"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

const fuzzSeedCoverageFormatVersion = 2

type fuzzSeedCoverageManifest struct {
	FormatVersion   int                     `json:"format_version"`
	AuditMatrix     string                  `json:"audit_matrix"`
	Entries         []fuzzSeedCoverageEntry `json:"entries"`
	CategoryEntries []fuzzSeedCategoryEntry `json:"category_entries"`
	BoundaryClasses []fuzzSeedBoundaryClass `json:"boundary_classes"`
}

type fuzzSeedCategoryEntry struct {
	Category            string   `json:"category"`
	RegistrationID      string   `json:"registration_id"`
	Target              string   `json:"target"`
	Seed                string   `json:"seed"`
	SeedDigest          string   `json:"seed_digest"`
	FeatureIDs          []string `json:"feature_ids"`
	Property            string   `json:"property"`
	ExpectedObservation string   `json:"expected_observation"`
}

type fuzzSeedCoverageEntry struct {
	Finding             string   `json:"finding"`
	RegistrationID      string   `json:"registration_id,omitempty"`
	Target              string   `json:"target"`
	Seed                string   `json:"seed"`
	SeedDigest          string   `json:"seed_digest"`
	FeatureIDs          []string `json:"feature_ids"`
	Property            string   `json:"property"`
	ExpectedObservation string   `json:"expected_observation"`
}

type fuzzSeedBoundaryClass struct {
	Class               string   `json:"class"`
	Target              string   `json:"target"`
	Seed                string   `json:"seed"`
	SeedDigest          string   `json:"seed_digest"`
	FeatureIDs          []string `json:"feature_ids"`
	Property            string   `json:"property"`
	ExpectedObservation string   `json:"expected_observation"`
}

type fuzzAuditMatrix struct {
	FormatVersion int                `json:"format_version"`
	Change        string             `json:"change"`
	Source        string             `json:"source"`
	Findings      []fuzzAuditFinding `json:"findings"`
}

type fuzzAuditFinding struct {
	ID                         string   `json:"id"`
	ViolatedRequirement        string   `json:"violated_requirement"`
	ProductionSymbols          []string `json:"production_symbols"`
	SupportingEvidenceSymbols  []string `json:"supporting_evidence_symbols,omitempty"`
	HistoricalForbiddenSymbols []string `json:"historical_forbidden_symbols,omitempty"`
	MinimalCounterexample      string   `json:"minimal_counterexample"`
	ExpectedOutcome            string   `json:"expected_outcome"`
	ImplementationTasks        []string `json:"implementation_tasks"`
	BlockingGates              []string `json:"blocking_gates"`
}

type decodedFuzzSeedObservation struct {
	FeatureIDs  []string
	Property    string
	Observation string
	Comparisons generatedComparisonCounts
	Category    string
	Fixture     string
	Symbol      string
	Outcome     string
}

func TestFuzzSeedCoverageMatchesAuditMatrix(t *testing.T) {
	t.Parallel()

	manifestPath := filepath.Join("testdata", "fuzz", "seed-coverage.v1.json")
	var manifest fuzzSeedCoverageManifest
	readStrictJSONTestFile(t, manifestPath, &manifest)
	if manifest.FormatVersion != fuzzSeedCoverageFormatVersion || manifest.AuditMatrix == "" || len(manifest.Entries) == 0 {
		t.Fatal("fuzz seed coverage manifest is empty or has an unsupported version")
	}
	var matrix fuzzAuditMatrix
	readStrictJSONTestFile(t, filepath.Clean(filepath.Join(filepath.Dir(manifestPath), manifest.AuditMatrix)), &matrix)
	want := make(map[string]bool, len(matrix.Findings))
	for _, finding := range matrix.Findings {
		if finding.ID == "" || want[finding.ID] {
			t.Fatalf("audit matrix has empty or duplicate finding %q", finding.ID)
		}
		want[finding.ID] = true
	}

	got := make(map[string]bool, len(manifest.Entries))
	registrationCounts := make(map[string]generatedComparisonCounts)
	populationObservations := make([]soundnessgate.ObservedMember, 0)
	for _, entry := range manifest.Entries {
		if entry.Finding == "" || got[entry.Finding] {
			t.Fatalf("empty or duplicate fuzz seed coverage finding: %+v", entry)
		}
		got[entry.Finding] = true
		observation := validateCommittedFuzzSeedExpectation(t, fuzzSeedCoverageEntryExpectation(entry))
		populationObservations = append(populationObservations,
			soundnessgate.ObservedMember{PopulationID: "decoded-seeds", MemberID: "finding:" + entry.Finding},
			soundnessgate.ObservedMember{PopulationID: "historical-counterexamples", MemberID: "finding:" + entry.Finding},
		)
		if entry.RegistrationID != "" {
			counts := registrationCounts[entry.RegistrationID]
			counts.add(observation.Comparisons)
			registrationCounts[entry.RegistrationID] = counts
		}
	}
	for finding := range want {
		if !got[finding] {
			t.Errorf("audit finding %q has no committed fuzz seed", finding)
		}
	}
	for finding := range got {
		if !want[finding] {
			t.Errorf("fuzz seed coverage has stale finding %q", finding)
		}
	}
	categoryRegistrations := make(map[string]bool, len(manifest.CategoryEntries))
	for _, entry := range manifest.CategoryEntries {
		if entry.Category == "" || entry.RegistrationID != entry.Category+".fuzz" ||
			categoryRegistrations[entry.RegistrationID] {
			t.Fatalf("invalid or duplicate category fuzz entry: %+v", entry)
		}
		observation := validateCommittedFuzzSeedExpectation(t, fuzzSeedCategoryExpectation(entry))
		if observation.Category != entry.Category || observation.Fixture == "" || observation.Symbol == "" {
			t.Fatalf("category fuzz seed %q decoded disconnected identity: %+v", entry.Seed, observation)
		}
		categoryRegistrations[entry.RegistrationID] = true
		registrationCounts[entry.RegistrationID] = generatedComparisonCounts{Identities: 1}
		populationObservations = append(populationObservations,
			soundnessgate.ObservedMember{PopulationID: "decoded-seeds", MemberID: "category:" + entry.RegistrationID},
			soundnessgate.ObservedMember{PopulationID: "historical-counterexamples", MemberID: "category:" + entry.RegistrationID},
		)
	}

	seenClasses := make(map[string]bool, len(manifest.BoundaryClasses))
	for _, boundary := range manifest.BoundaryClasses {
		if boundary.Class == "" || seenClasses[boundary.Class] {
			t.Fatalf("empty or duplicate fuzz boundary class: %+v", boundary)
		}
		seenClasses[boundary.Class] = true
		_ = validateCommittedFuzzSeedExpectation(t, fuzzSeedBoundaryExpectation(boundary))
		populationObservations = append(populationObservations, soundnessgate.ObservedMember{
			PopulationID: "decoded-seeds",
			MemberID:     "boundary:" + boundary.Class,
		})
	}
	for _, required := range []string{
		"alias-copy", "alias-kill", "branch-join-topology", "call-return-topology",
		"constraint-sat", "constraint-unsat", "initial-fact-needs-validation",
		"initial-fact-validated", "recursive-topology", "unknown-effect", "validation-effect",
	} {
		if !seenClasses[required] {
			t.Errorf("required fuzz boundary class %q has no exact decoded seed", required)
		}
	}

	for registrationID := range registrationCounts {
		populationObservations = append(populationObservations, soundnessgate.ObservedMember{
			PopulationID: "protocol-categories",
			MemberID:     registrationID,
		})
	}
	emitSoundnessSubgateReport(t, observedPopulations(t, populationObservations))
}

type fuzzSeedExpectation struct {
	Target              string
	Seed                string
	SeedDigest          string
	FeatureIDs          []string
	Property            string
	ExpectedObservation string
}

func fuzzSeedCoverageEntryExpectation(entry fuzzSeedCoverageEntry) fuzzSeedExpectation {
	return fuzzSeedExpectation{
		Target:              entry.Target,
		Seed:                entry.Seed,
		SeedDigest:          entry.SeedDigest,
		FeatureIDs:          entry.FeatureIDs,
		Property:            entry.Property,
		ExpectedObservation: entry.ExpectedObservation,
	}
}

func fuzzSeedBoundaryExpectation(boundary fuzzSeedBoundaryClass) fuzzSeedExpectation {
	return fuzzSeedExpectation{
		Target:              boundary.Target,
		Seed:                boundary.Seed,
		SeedDigest:          boundary.SeedDigest,
		FeatureIDs:          boundary.FeatureIDs,
		Property:            boundary.Property,
		ExpectedObservation: boundary.ExpectedObservation,
	}
}

func fuzzSeedCategoryExpectation(entry fuzzSeedCategoryEntry) fuzzSeedExpectation {
	return fuzzSeedExpectation{
		Target:              entry.Target,
		Seed:                entry.Seed,
		SeedDigest:          entry.SeedDigest,
		FeatureIDs:          entry.FeatureIDs,
		Property:            entry.Property,
		ExpectedObservation: entry.ExpectedObservation,
	}
}

func validateCommittedFuzzSeedExpectation(
	t *testing.T,
	expectation fuzzSeedExpectation,
) decodedFuzzSeedObservation {
	t.Helper()

	if expectation.Target == "" || expectation.Seed == "" || expectation.SeedDigest == "" ||
		len(expectation.FeatureIDs) == 0 || expectation.Property == "" || expectation.ExpectedObservation == "" {
		t.Fatalf("incomplete fuzz seed expectation: %+v", expectation)
	}
	if !slices.IsSorted(expectation.FeatureIDs) || slices.ContainsFunc(expectation.FeatureIDs, func(feature string) bool {
		return strings.TrimSpace(feature) == ""
	}) {
		t.Fatalf("fuzz seed feature IDs must be nonempty and canonically sorted: %v", expectation.FeatureIDs)
	}
	seedPath := filepath.Join("testdata", "fuzz", expectation.Target, expectation.Seed)
	data := readCommittedByteFuzzSeed(t, seedPath)
	digest := soundnessevidence.DigestBytes(data)
	if digest != expectation.SeedDigest {
		t.Errorf("fuzz seed %s digest = %q, want %q", seedPath, digest, expectation.SeedDigest)
	}
	observation := observeCommittedFuzzSeed(t, expectation.Target, data)
	if !slices.Equal(observation.FeatureIDs, expectation.FeatureIDs) {
		t.Errorf("fuzz seed %s features = %q, want %q", seedPath, observation.FeatureIDs, expectation.FeatureIDs)
	}
	if observation.Property != expectation.Property {
		t.Errorf("fuzz seed %s property = %q, want %q", seedPath, observation.Property, expectation.Property)
	}
	if observation.Observation != expectation.ExpectedObservation {
		t.Errorf("fuzz seed %s observation = %q, want %q", seedPath, observation.Observation, expectation.ExpectedObservation)
	}
	return observation
}

func observeCommittedFuzzSeed(t *testing.T, target string, data []byte) decodedFuzzSeedObservation {
	t.Helper()

	switch target {
	case "FuzzIFDSTabulation":
		return observeIntegratedIFDSSeed(t, data)
	case "FuzzInterprocSupergraphConstruction":
		return observeInterprocSupergraphSeed(t, data)
	case "FuzzProtocolSummaryFactSerialization":
		return observeProtocolSummarySeed(t, data)
	case "FuzzSSAConstraintNormalizationEvidence":
		return observeSSAConstraintSeed(t, data)
	case "FuzzSemanticCatalogDecoding":
		return observeSemanticCatalogSeed(t, data)
	case "FuzzFindingDeterminism":
		return observeFindingDeterminismSeed(t, data)
	case "FuzzSemanticCategoryEvidence":
		return observeSemanticCategorySeed(t, data)
	default:
		t.Fatalf("unsupported fuzz seed target %q", target)
		return decodedFuzzSeedObservation{}
	}
}

func observeIntegratedIFDSSeed(t *testing.T, data []byte) decodedFuzzSeedObservation {
	t.Helper()

	program, err := protocoloracle.DecodeFuzzProgram(data)
	if err != nil {
		t.Fatalf("DecodeFuzzProgram() error: %v", err)
	}
	comparisons, err := compareGeneratedGoProgram(t, program, 512, "")
	if err != nil {
		t.Fatalf("integrated fuzz seed comparison: %v", err)
	}
	features := []string{
		"alias-" + string(integratedProgramAlias(program)),
		"call-sites-" + strconv.Itoa(program.Metrics().CallSites),
		"condition-" + string(integratedProgramCondition(program)),
		"constraint-" + string(integratedProgramConstraint(program)),
		"fact-0-" + string(program.InitialFactFor(0)),
		"fact-1-" + string(program.InitialFactFor(1)),
		"matched-return",
		"operation-" + string(integratedProgramOperation(program)),
		"procedures-" + strconv.Itoa(program.Metrics().Procedures),
		"topology-" + string(program.Shape.Topology),
		"unknown-" + string(integratedProgramUnknownEffect(program)),
	}
	observation := "safe-observed"
	switch {
	case comparisons.ViolationCases > 0 && comparisons.InconclusiveCases > 0:
		observation = "violation-and-inconclusive-detected"
	case comparisons.ViolationCases > 0:
		observation = "violation-detected"
	case comparisons.InconclusiveCases > 0:
		observation = "inconclusive-detected"
	}
	return decodedFuzzSeedObservation{
		FeatureIDs:  canonicalFuzzFeatures(features),
		Property:    "integrated-analyzer-reference-agreement",
		Observation: observation,
		Comparisons: comparisons,
	}
}

func observeInterprocSupergraphSeed(t *testing.T, data []byte) decodedFuzzSeedObservation {
	t.Helper()

	source, wantFunctions, wantRecursion := fuzzInterprocSource(data)
	pass, file := buildTypedPassFromSource(t, source)
	entry := findFuncDecl(t, file, "entry")
	graph := buildInterprocSupergraphForFunc(pass, entry, buildSSAForPass(pass))
	procedures := make(map[string]bool)
	for _, node := range graph.Nodes {
		procedures[node.FuncKey] = true
	}
	if len(procedures) < wantFunctions {
		t.Fatalf("supergraph procedures = %d, want at least %d", len(procedures), wantFunctions)
	}
	features := []string{"alias-source", "matched-call-return", "multi-procedure", "validation-source"}
	observation := "call-return-observed"
	if wantRecursion {
		if !fuzzGraphHasRecursiveCallAndReturn(graph) {
			t.Fatal("recursive seed omitted its matched recursive call/return")
		}
		features = append(features, "recursive-call-return")
		observation = "recursive-call-return-observed"
	}
	return decodedFuzzSeedObservation{
		FeatureIDs:  canonicalFuzzFeatures(features),
		Property:    "typed-supergraph-preserves-generated-topology",
		Observation: observation,
	}
}

func observeProtocolSummarySeed(t *testing.T, data []byte) decodedFuzzSeedObservation {
	t.Helper()

	fact := ProtocolSummaryFact{
		FormatVersion:    uint32(fuzzByte(data, 0)%byte(protocolSummaryFactVersion) + 1),
		PackagePath:      fmt.Sprintf("example.com/p%d", fuzzByte(data, 1)%4),
		FunctionName:     fmt.Sprintf("F%d", fuzzByte(data, 2)%8),
		FunctionIdentity: fmt.Sprintf("example.com/p%d.F%d", fuzzByte(data, 1)%4, fuzzByte(data, 2)%8),
		Complete:         fuzzByte(data, 6)%2 == 0,
		Effects: []ProtocolSummaryEffectFact{{
			TargetKind:          []string{protocolSummaryTargetReceiver, protocolSummaryTargetParameter, "bad"}[fuzzByte(data, 3)%3],
			TargetSlot:          int(fuzzByte(data, 4) % 4),
			ConditionResultSlot: int(fuzzByte(data, 5) % 4),
			Condition:           protocolSummaryConditionResultNil,
		}},
	}
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(fact); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	var decoded ProtocolSummaryFact
	if err := gob.NewDecoder(bytes.NewReader(encoded.Bytes())).Decode(&decoded); err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if fact.String() != decoded.String() ||
		validateProtocolSummaryFactShape(&fact, fact.PackagePath) != validateProtocolSummaryFactShape(&decoded, decoded.PackagePath) {
		t.Fatal("summary round trip changed semantics or validation")
	}
	completeness := "incomplete"
	if fact.Complete {
		completeness = "complete"
	}
	return decodedFuzzSeedObservation{
		FeatureIDs: canonicalFuzzFeatures([]string{
			"summary-" + completeness,
			"summary-format-" + strconv.FormatUint(uint64(fact.FormatVersion), 10),
			"summary-target-" + fact.Effects[0].TargetKind,
			"summary-validation-parity",
		}),
		Property:    "summary-roundtrip-preserves-validation",
		Observation: "roundtrip-preserved",
	}
}

func observeSSAConstraintSeed(t *testing.T, data []byte) decodedFuzzSeedObservation {
	t.Helper()

	atomCount := int(fuzzByte(data, 0)%8 + 1)
	alternative := make([]cfgPredicateConstraint, 0, atomCount)
	for index := range atomCount {
		alternative = append(alternative, cfgPredicateConstraint{
			subject: fmt.Sprintf("ssa-%d", fuzzByte(data, 1+index)%3),
			op:      []string{"eq", "neq"}[fuzzByte(data, 9+index)%2],
			value:   strconv.FormatUint(uint64(fuzzByte(data, 17+index)%4), 10),
		})
	}
	formula := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{alternative}}
	formula.normalize()
	reordered := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{slices.Clone(alternative)}}
	slices.Reverse(reordered.alternatives[0])
	reordered.normalize()
	if normalizedConstraintFormula(formula) != normalizedConstraintFormula(reordered) {
		t.Fatal("constraint normalization depends on atom order")
	}
	features := []string{"constraint-atoms-" + strconv.Itoa(atomCount), "constraint-normalized"}
	observation := "satisfiable-normalized"
	evidence, unsat := buildSSAConstraintEvidence(formula)
	if unsat {
		evidence.WitnessPath = []int32{0, 1}
		evidence.Subjects = ssaConstraintSubjects(formula)
		if !checkSSAConstraintFormulaEvidence(formula, []int32{0, 1}, evidence) {
			t.Fatal("generated UNSAT evidence was rejected")
		}
		corrupt := evidence
		corrupt.FormatVersion++
		if checkSSAConstraintFormulaEvidence(formula, []int32{0, 1}, corrupt) {
			t.Fatal("corrupt UNSAT evidence was accepted")
		}
		features = append(features, "constraint-unsat", "corrupt-evidence-rejected")
		observation = "unsat-evidence-validated"
	}
	return decodedFuzzSeedObservation{
		FeatureIDs:  canonicalFuzzFeatures(features),
		Property:    "constraint-normalization-and-evidence",
		Observation: observation,
	}
}

func observeSemanticCatalogSeed(t *testing.T, data []byte) decodedFuzzSeedObservation {
	t.Helper()

	var catalog semanticRuleCatalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return decodedFuzzSeedObservation{
			FeatureIDs:  []string{"catalog-decode-rejected"},
			Property:    "catalog-strict-decode-and-validation",
			Observation: "decode-rejected",
		}
	}
	if err := validateSemanticRuleCatalog(catalog); err != nil {
		return decodedFuzzSeedObservation{
			FeatureIDs:  []string{"catalog-validation-rejected"},
			Property:    "catalog-strict-decode-and-validation",
			Observation: "validation-rejected",
		}
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	var roundTrip semanticRuleCatalog
	if err := json.Unmarshal(encoded, &roundTrip); err != nil || validateSemanticRuleCatalog(roundTrip) != nil {
		t.Fatalf("accepted catalog failed round trip: %v", err)
	}
	return decodedFuzzSeedObservation{
		FeatureIDs:  []string{"catalog-roundtrip-valid"},
		Property:    "catalog-strict-decode-and-validation",
		Observation: "roundtrip-preserved",
	}
}

func observeFindingDeterminismSeed(t *testing.T, data []byte) decodedFuzzSeedObservation {
	t.Helper()

	part := string(data)
	firstID := StableFindingID(CategoryUnvalidatedCast, part, "tail")
	secondID := StableFindingID(CategoryUnvalidatedCast, part, "tail")
	if firstID != secondID {
		t.Fatal("stable finding ID changed across identical calls")
	}
	first := mustMarshalFuzzValue(t, FindingStreamRecord{Category: CategoryUnvalidatedCast, ID: firstID, Meta: map[string]string{"b": part, "a": "first"}})
	second := mustMarshalFuzzValue(t, FindingStreamRecord{Category: CategoryUnvalidatedCast, ID: secondID, Meta: map[string]string{"a": "first", "b": part}})
	if !bytes.Equal(first, second) {
		t.Fatal("finding JSON depends on map insertion order")
	}
	return decodedFuzzSeedObservation{
		FeatureIDs:  []string{"finding-json-map-order", "stable-finding-id"},
		Property:    "finding-id-and-json-determinism",
		Observation: "deterministic",
	}
}

func integratedProgramAlias(program protocoloracle.Program) protocoloracle.AliasAction {
	for _, node := range program.Nodes {
		if node.AliasAction != protocoloracle.AliasActionNone {
			return node.AliasAction
		}
	}
	return protocoloracle.AliasActionNone
}

func integratedProgramCondition(program protocoloracle.Program) protocoloracle.ConditionalResult {
	for _, node := range program.Nodes {
		if node.Condition != protocoloracle.ConditionalResultNone {
			return node.Condition
		}
	}
	return protocoloracle.ConditionalResultNone
}

func integratedProgramConstraint(program protocoloracle.Program) protocoloracle.ConstraintKind {
	for _, node := range program.Nodes {
		if node.Constraint != protocoloracle.ConstraintNone {
			return node.Constraint
		}
	}
	return protocoloracle.ConstraintNone
}

func integratedProgramOperation(program protocoloracle.Program) protocoloracle.Operation {
	for _, node := range program.Nodes {
		if node.Operation != protocoloracle.OperationNoop {
			return node.Operation
		}
	}
	return protocoloracle.OperationNoop
}

func integratedProgramUnknownEffect(program protocoloracle.Program) protocoloracle.UnknownEffect {
	for _, node := range program.Nodes {
		if node.UnknownEffect != protocoloracle.UnknownEffectNone {
			return node.UnknownEffect
		}
	}
	return protocoloracle.UnknownEffectNone
}

func canonicalFuzzFeatures(features []string) []string {
	result := slices.Clone(features)
	slices.Sort(result)
	return slices.Compact(result)
}

func readCommittedByteFuzzSeed(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fuzz seed %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || lines[0] != "go test fuzz v1" || !strings.HasPrefix(lines[1], "[]byte(") || !strings.HasSuffix(lines[1], ")") {
		t.Fatalf("unsupported committed byte fuzz seed %s", path)
	}
	decoded, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(lines[1], "[]byte("), ")"))
	if err != nil {
		t.Fatalf("decode fuzz seed %s: %v", path, err)
	}
	return []byte(decoded)
}

func readStrictJSONTestFile(t *testing.T, path string, target any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode %s: %v", path, fmt.Errorf("invalid JSON: %w", err))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			t.Fatalf("decode %s: multiple JSON values are not allowed", path)
		}
		t.Fatalf("decode trailing JSON in %s: %v", path, err)
	}
}
