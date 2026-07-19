// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/protocoloracle"
)

func FuzzInterprocSupergraphConstruction(f *testing.F) {
	for _, seed := range [][]byte{{0}, {1, 2, 3}, {3, 2, 1, 0}, {7, 11, 13, 17}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		source, wantFunctions, wantRecursion := fuzzInterprocSource(data)
		pass, file := buildTypedPassFromSource(t, source)
		entry := findFuncDecl(t, file, "entry")
		ssaResult := buildSSAForPass(pass)
		graph := buildInterprocSupergraphForFunc(pass, entry, ssaResult)
		procedures := make(map[string]bool)
		for _, node := range graph.Nodes {
			procedures[node.FuncKey] = true
		}
		if len(procedures) < wantFunctions {
			t.Fatalf("supergraph procedures = %d, want at least %d for generated calls/recursion", len(procedures), wantFunctions)
		}
		for _, edge := range graph.Edges {
			if _, ok := graph.Nodes[edge.From.Key()]; !ok {
				t.Fatalf("edge source %s is missing", edge.From.Key())
			}
			if _, ok := graph.Nodes[edge.To.Key()]; !ok {
				t.Fatalf("edge target %s is missing", edge.To.Key())
			}
		}
		if !strings.Contains(source, ".Validate()") || !strings.Contains(source, "alias :=") {
			t.Fatal("generated source omitted validation or alias dimensions")
		}
		if wantRecursion && !fuzzGraphHasRecursiveCallAndReturn(graph) {
			t.Fatal("generated recursive source omitted a matched recursive call/return topology")
		}
	})
}

func FuzzIFDSTabulation(f *testing.F) {
	for _, seed := range [][]byte{{0}, {1, 2, 3}, {4, 3, 2, 1}, {9, 7, 5, 3, 1}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		program, err := protocoloracle.DecodeFuzzProgram(data)
		if err != nil {
			t.Fatalf("DecodeFuzzProgram() error: %v", err)
		}
		if _, err := compareGeneratedGoProgram(t, program, 512, ""); err != nil {
			t.Fatalf("integrated analyzer/reference comparison: %v", err)
		}
		reordered := program
		reordered.Nodes = slices.Clone(program.Nodes)
		reordered.Edges = slices.Clone(program.Edges)
		slices.Reverse(reordered.Nodes)
		slices.Reverse(reordered.Edges)
		if reordered.Fingerprint() != program.Fingerprint() {
			t.Fatal("node and edge insertion order changed the normalized program fingerprint")
		}
		if _, err := compareGeneratedGoProgram(t, reordered, 512, ""); err != nil {
			t.Fatalf("reordered integrated analyzer/reference comparison: %v", err)
		}
	})
}

func fuzzInterprocSource(data []byte) (string, int, bool) {
	helperCount := int(fuzzByte(data, 0)%4) + 1
	recursive := fuzzByte(data, helperCount*2+2)&1 != 0
	var source strings.Builder
	source.WriteString("package p\ntype Value string\nfunc (v Value) Validate() error { return nil }\n")
	for index := range helperCount {
		fmt.Fprintf(&source, "func helper%d(v Value, ok bool) Value {\n", index)
		source.WriteString("alias := v\nif err := alias.Validate(); err != nil { return v }\n")
		if index > 0 && fuzzByte(data, index+1)&1 != 0 {
			fmt.Fprintf(&source, "if ok { alias = helper%d(alias, false) }\n", index-1)
		}
		if fuzzByte(data, helperCount+index+1)&1 != 0 {
			source.WriteString("if !ok { return alias }\n")
		}
		if recursive && index == helperCount-1 {
			fmt.Fprintf(&source, "if ok { alias = helper%d(alias, false) }\n", index)
		}
		source.WriteString("return alias\n}\n")
	}
	source.WriteString("func entry(v Value, ok bool) Value {\n")
	for index := range helperCount {
		fmt.Fprintf(&source, "v = helper%d(v, ok)\n", index)
	}
	source.WriteString("return v\n}\n")
	return source.String(), helperCount + 1, recursive
}

func fuzzGraphHasRecursiveCallAndReturn(graph interprocSupergraph) bool {
	callSites := make(map[string]string)
	for _, edge := range graph.Edges {
		if edge.Kind == interprocEdgeCall && edge.From.FuncKey == edge.To.FuncKey {
			callSites[edge.CallSite] = edge.To.FuncKey
		}
	}
	for _, edge := range graph.Edges {
		if callee, ok := callSites[edge.CallSite]; ok && edge.Kind == interprocEdgeReturn && edge.From.FuncKey == callee {
			return true
		}
	}
	return false
}

func FuzzProtocolSummaryFactSerialization(f *testing.F) {
	for _, seed := range [][]byte{{1, 0, 0}, {2, 1, 1}, {255, 2, 2}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
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
		var first bytes.Buffer
		if err := gob.NewEncoder(&first).Encode(fact); err != nil {
			t.Fatalf("Encode() error: %v", err)
		}
		var decoded ProtocolSummaryFact
		if err := gob.NewDecoder(bytes.NewReader(first.Bytes())).Decode(&decoded); err != nil {
			t.Fatalf("Decode() error: %v", err)
		}
		var second bytes.Buffer
		if err := gob.NewEncoder(&second).Encode(decoded); err != nil {
			t.Fatalf("re-Encode() error: %v", err)
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) || fact.String() != decoded.String() ||
			validateProtocolSummaryFactShape(&fact, fact.PackagePath) != validateProtocolSummaryFactShape(&decoded, decoded.PackagePath) {
			t.Fatal("protocol summary fact round trip changed bytes or semantics")
		}
	})
}

func FuzzSSAConstraintNormalizationEvidence(f *testing.F) {
	for _, seed := range [][]byte{{0, 0, 0}, {1, 1, 1}, {2, 3, 4}, {255}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
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
		reordered := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{append([]cfgPredicateConstraint(nil), alternative...)}}
		slices.Reverse(reordered.alternatives[0])
		reordered.normalize()
		if normalizedConstraintFormula(formula) != normalizedConstraintFormula(reordered) {
			t.Fatal("constraint normalization depends on atom order")
		}
		evidence, unsat := buildSSAConstraintEvidence(formula)
		if !unsat {
			return
		}
		evidence.WitnessPath = []int32{0, 1}
		evidence.Subjects = ssaConstraintSubjects(formula)
		if !checkSSAConstraintFormulaEvidence(formula, []int32{0, 1}, evidence) {
			t.Fatal("generated UNSAT evidence was rejected")
		}
		corrupt := evidence
		corrupt.FormatVersion++
		if checkSSAConstraintFormulaEvidence(formula, []int32{0, 1}, corrupt) {
			t.Fatal("corrupt evidence was accepted")
		}
	})
}

func FuzzSemanticCatalogDecoding(f *testing.F) {
	live, err := os.ReadFile(semanticRulesCatalogPath())
	if err != nil {
		f.Fatalf("ReadFile() error: %v", err)
	}
	f.Add(live)
	f.Add([]byte(`{"version":1,"rules":[]}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			return
		}
		var catalog semanticRuleCatalog
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&catalog); err != nil {
			return
		}
		if err := validateSemanticRuleCatalog(catalog); err != nil {
			return
		}
		encoded, err := json.Marshal(catalog)
		if err != nil {
			t.Fatalf("Marshal() error: %v", err)
		}
		var roundTrip semanticRuleCatalog
		if err := json.Unmarshal(encoded, &roundTrip); err != nil || validateSemanticRuleCatalog(roundTrip) != nil {
			t.Fatalf("accepted catalog failed round trip: %v", err)
		}
	})
}

func FuzzFindingDeterminism(f *testing.F) {
	for _, seed := range [][]byte{[]byte("plain"), []byte("unicode-ç"), []byte("reserved/a?b=c")} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		part := string(data)
		firstID := StableFindingID(CategoryUnvalidatedCast, part, "tail")
		secondID := StableFindingID(CategoryUnvalidatedCast, part, "tail")
		if firstID != secondID {
			t.Fatal("stable finding ID changed across identical calls")
		}
		first := mustMarshalFuzzValue(t, FindingStreamRecord{Category: CategoryUnvalidatedCast, ID: firstID, Meta: map[string]string{"b": part, "a": "first"}})
		second := mustMarshalFuzzValue(t, FindingStreamRecord{Category: CategoryUnvalidatedCast, ID: secondID, Meta: map[string]string{"a": "first", "b": part}})
		if !bytes.Equal(first, second) {
			t.Fatalf("finding JSON depends on map insertion order: %s != %s", first, second)
		}
	})
}

func FuzzSemanticCategoryEvidence(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		first, firstErr := classifySemanticCategorySeed(data)
		second, secondErr := classifySemanticCategorySeed(data)
		if (firstErr == nil) != (secondErr == nil) || !reflect.DeepEqual(first, second) {
			t.Fatalf("semantic category seed classification is nondeterministic: first=%+v/%v second=%+v/%v", first, firstErr, second, secondErr)
		}
		if firstErr != nil {
			return
		}

		seed, wantOutcome, wantFeatures, err := decodeSemanticCategorySeed(data)
		if err != nil {
			t.Fatalf("accepted semantic category seed no longer decodes: %v", err)
		}
		reordered := seed.Program
		reordered.Identities = slices.Clone(seed.Program.Identities)
		reordered.Aliases = slices.Clone(seed.Program.Aliases)
		reordered.Constraints = slices.Clone(seed.Program.Constraints)
		reordered.Procedures = slices.Clone(seed.Program.Procedures)
		reordered.Blocks = slices.Clone(seed.Program.Blocks)
		slices.Reverse(reordered.Identities)
		slices.Reverse(reordered.Aliases)
		slices.Reverse(reordered.Constraints)
		slices.Reverse(reordered.Procedures)
		slices.Reverse(reordered.Blocks)
		gotOutcome, gotFeatures, err := evaluateSemanticCategoryProgram(seed.FeatureID, reordered)
		if err != nil {
			t.Fatalf("semantic category relation rejected reordered program: %v", err)
		}
		if gotOutcome != wantOutcome || !reflect.DeepEqual(canonicalFuzzFeatures(gotFeatures), canonicalFuzzFeatures(wantFeatures)) {
			t.Fatalf(
				"semantic category relation changed after declaration reordering: outcome=%q/%q features=%v/%v",
				gotOutcome,
				wantOutcome,
				gotFeatures,
				wantFeatures,
			)
		}
	})
}

func fuzzByte(data []byte, index int) byte {
	if len(data) == 0 {
		return 0
	}
	return data[index%len(data)]
}

func normalizedConstraintFormula(formula cfgSSAConstraintFormula) string {
	normalized := make([][]string, len(formula.alternatives))
	for alternativeIndex, alternative := range formula.alternatives {
		for _, constraint := range alternative {
			normalized[alternativeIndex] = append(normalized[alternativeIndex], cfgConstraintKey(constraint))
		}
	}
	encoded := mustMarshalTestValue(normalized)
	return string(encoded)
}

func mustMarshalFuzzValue(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fuzz value: %v", err)
	}
	return encoded
}

func mustMarshalTestValue(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic("marshal deterministic test value: " + err.Error())
	}
	return encoded
}
