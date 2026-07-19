// SPDX-License-Identifier: MPL-2.0

package protocoloracle

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestBoundedCorpusCardinalityCensusAndReferenceOutcomes(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	programs := collectPrograms(t, manifest)
	if len(programs) != manifest.ExpectedProgramCount {
		t.Fatalf("generated %d programs, want %d", len(programs), manifest.ExpectedProgramCount)
	}
	census := FeatureCensus(programs)
	for _, feature := range manifest.RequiredFeatures {
		if census[feature] == 0 {
			t.Errorf("derived census did not exercise required feature %q", feature)
		}
	}
	seen := make(map[Outcome]bool)
	for _, program := range programs {
		result := Interpret(program, manifest.Blocking.MaxStates)
		for _, identity := range program.Identities {
			seen[result.ByIdentity[identity]] = true
		}
	}
	for _, outcome := range []Outcome{OutcomeNone, OutcomeViolation, OutcomeInconclusive} {
		if !seen[outcome] {
			t.Errorf("generated corpus did not exercise %q", outcome)
		}
	}
}

func TestReferenceInterpreterReviewedScenarios(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	want := map[string]Outcome{
		"scenario/validated-nil":            OutcomeNone,
		"scenario/validation-non-nil":       OutcomeViolation,
		"scenario/validation-unknown":       OutcomeViolation,
		"scenario/alias-copy":               OutcomeNone,
		"scenario/alias-kill":               OutcomeViolation,
		"scenario/post-validation-mutation": OutcomeViolation,
		"scenario/post-validation-unknown":  OutcomeInconclusive,
		"scenario/post-validation-escape":   OutcomeInconclusive,
		"scenario/matched-return":           OutcomeNone,
		"scenario/refinement-unsat":         OutcomeNone,
	}
	found := make(map[string]bool, len(want))
	err := Enumerate(manifest, func(program Program) error {
		expected, ok := want[program.CaseID]
		if !ok {
			return nil
		}
		found[program.CaseID] = true
		if got := Interpret(program, manifest.Blocking.MaxStates).ByIdentity[0]; got != expected {
			return fmt.Errorf("%s outcome = %q, want %q", program.CaseID, got, expected)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name := range want {
		if !found[name] {
			t.Errorf("reviewed scenario %q was not admitted", name)
		}
	}
}

func TestReferenceInterpreterRetainsWitnessSummaryAndRefinementEvidence(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	programs := make(map[string]Program)
	if err := Enumerate(manifest, func(program Program) error {
		programs[program.CaseID] = program
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	unknown := Interpret(programs["scenario/post-validation-unknown"], manifest.Blocking.MaxStates).Evidence[0]
	if unknown.Outcome != OutcomeInconclusive || len(unknown.Witness) == 0 {
		t.Fatalf("unknown-effect evidence = %+v, want inconclusive with a realizable witness", unknown)
	}
	escaped := Interpret(programs["scenario/post-validation-escape"], manifest.Blocking.MaxStates).Evidence[0]
	if escaped.Outcome != OutcomeInconclusive || len(escaped.Witness) == 0 {
		t.Fatalf("escape evidence = %+v, want inconclusive with a realizable witness", escaped)
	}
	refined := Interpret(programs["scenario/refinement-unsat"], manifest.Blocking.MaxStates).Evidence[0]
	if refined.Outcome != OutcomeNone || len(refined.RefinedWitnesses) == 0 {
		t.Fatalf("refinement evidence = %+v, want an exact discharged witness", refined)
	}
	summarized := Interpret(programs["scenario/matched-return"], manifest.Blocking.MaxStates).Evidence[0]
	if summarized.Summaries == 0 || summarized.SummaryReuses == 0 || len(summarized.Witness) == 0 {
		t.Fatalf("summary evidence = %+v, want produced/reused summary and matching witness", summarized)
	}
}

func TestManifestDimensionsAreCorpusSensitive(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	basePrograms := collectPrograms(t, manifest)
	baseFingerprint := CorpusFingerprint(basePrograms)
	tests := []struct {
		name   string
		mutate func(*BoundsManifest)
	}{
		{name: "procedures", mutate: func(value *BoundsManifest) { value.Dimensions.ProcedureCounts = value.Dimensions.ProcedureCounts[:1] }},
		{name: "nodes", mutate: func(value *BoundsManifest) {
			value.Dimensions.NodesPerProcedure = value.Dimensions.NodesPerProcedure[:1]
		}},
		{name: "identities", mutate: func(value *BoundsManifest) { value.Dimensions.IdentityCounts = value.Dimensions.IdentityCounts[:1] }},
		{name: "call-sites", mutate: func(value *BoundsManifest) { value.Dimensions.CallSiteCounts = value.Dimensions.CallSiteCounts[:1] }},
		{name: "call-depth", mutate: func(value *BoundsManifest) { value.Dimensions.CallDepths = value.Dimensions.CallDepths[:1] }},
		{name: "topologies", mutate: func(value *BoundsManifest) { value.Dimensions.Topologies = value.Dimensions.Topologies[:1] }},
		{name: "branch-joins", mutate: func(value *BoundsManifest) { value.Dimensions.BranchJoins = value.Dimensions.BranchJoins[:1] }},
		{name: "recursion", mutate: func(value *BoundsManifest) { value.Dimensions.Recursion = value.Dimensions.Recursion[:1] }},
		{name: "operations", mutate: func(value *BoundsManifest) { value.Dimensions.Operations = value.Dimensions.Operations[:1] }},
		{name: "conditional-results", mutate: func(value *BoundsManifest) {
			value.Dimensions.ConditionalResults = value.Dimensions.ConditionalResults[:1]
		}},
		{name: "alias-actions", mutate: func(value *BoundsManifest) { value.Dimensions.AliasActions = value.Dimensions.AliasActions[:1] }},
		{name: "unknown-effects", mutate: func(value *BoundsManifest) { value.Dimensions.UnknownEffects = value.Dimensions.UnknownEffects[:1] }},
		{name: "constraints", mutate: func(value *BoundsManifest) { value.Dimensions.Constraints = value.Dimensions.Constraints[:1] }},
		{name: "initial-facts", mutate: func(value *BoundsManifest) { value.Dimensions.InitialFacts = value.Dimensions.InitialFacts[:1] }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			changed := cloneManifest(t, manifest)
			test.mutate(&changed)
			programs, err := generateAll(changed)
			if err != nil {
				t.Fatalf("generateAll() error: %v", err)
			}
			if got := CorpusFingerprint(programs); got == baseFingerprint {
				t.Errorf("dimension mutation left corpus fingerprint unchanged: %s", got)
			}
		})
	}
}

func TestManifestDeclaresTheMinimumExecutableNodeShape(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	manifest.Dimensions.NodesPerProcedure = []int{3}
	if err := validateManifest(manifest); err == nil {
		t.Fatal("validateManifest() accepted a shape too small for entry, operation, return, and terminal nodes")
	}
}

func TestManifestDimensionCasesChangeDerivedSemantics(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	programs := make(map[string]Program)
	if err := Enumerate(manifest, func(program Program) error {
		programs[program.CaseID] = program
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	base := programs["baseline"]
	baseMetrics := base.Metrics()
	tests := []struct {
		caseID  string
		changed func(Program, ProgramMetrics) bool
	}{
		{caseID: "dimension/procedures/2", changed: func(_ Program, metrics ProgramMetrics) bool { return metrics.Procedures > baseMetrics.Procedures }},
		{caseID: "dimension/nodes/5", changed: func(_ Program, metrics ProgramMetrics) bool {
			return metrics.NodesPerProcedure > baseMetrics.NodesPerProcedure
		}},
		{caseID: "dimension/identities/2", changed: func(_ Program, metrics ProgramMetrics) bool { return metrics.Identities > baseMetrics.Identities }},
		{caseID: "dimension/call-sites/2", changed: func(_ Program, metrics ProgramMetrics) bool { return metrics.CallSites > baseMetrics.CallSites }},
		{caseID: "dimension/call-depth/2", changed: func(_ Program, metrics ProgramMetrics) bool { return metrics.CallDepth > baseMetrics.CallDepth }},
		{caseID: "dimension/topology/recursive", changed: func(_ Program, metrics ProgramMetrics) bool { return metrics.Recursive }},
		{caseID: "dimension/branch-join/true", changed: func(_ Program, metrics ProgramMetrics) bool { return metrics.BranchJoin }},
		{caseID: "dimension/recursion/true", changed: func(_ Program, metrics ProgramMetrics) bool { return metrics.Recursive }},
		{caseID: "dimension/operation/unresolved", changed: func(program Program, _ ProgramMetrics) bool {
			return hasNodeFeature(program, func(node Node) bool { return node.Operation == OperationUnresolved })
		}},
		{caseID: "dimension/condition/unknown", changed: func(program Program, _ ProgramMetrics) bool {
			return hasNodeFeature(program, func(node Node) bool { return node.Condition == ConditionalResultUnknown })
		}},
		{caseID: "dimension/alias/kill", changed: func(program Program, _ ProgramMetrics) bool {
			return hasNodeFeature(program, func(node Node) bool { return node.AliasAction == AliasActionKill })
		}},
		{caseID: "dimension/unknown/escaped-heap-mutation", changed: func(program Program, _ ProgramMetrics) bool {
			return hasNodeFeature(program, func(node Node) bool { return node.UnknownEffect == UnknownEffectEscapedHeap })
		}},
		{caseID: "dimension/constraint/unsat", changed: func(program Program, _ ProgramMetrics) bool {
			return hasNodeFeature(program, func(node Node) bool { return node.Constraint == ConstraintUNSAT })
		}},
		{caseID: "dimension/initial-fact/validated", changed: func(program Program, _ ProgramMetrics) bool {
			return program.Metrics().ValidatedFacts > 0
		}},
	}
	for _, test := range tests {
		t.Run(test.caseID, func(t *testing.T) {
			t.Parallel()

			program, ok := programs[test.caseID]
			if !ok {
				t.Fatalf("dimension case %q is missing", test.caseID)
			}
			if !test.changed(program, program.Metrics()) {
				t.Fatalf("dimension case %q did not change derived semantics: %+v", test.caseID, program)
			}
		})
	}
}

func TestCorpusProfilesPartitionDeterministically(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	for _, profileName := range []string{"blocking", "scheduled"} {
		t.Run(profileName, func(t *testing.T) {
			t.Parallel()

			profile, err := manifest.Profile(profileName)
			if err != nil {
				t.Fatal(err)
			}
			first := collectShardFingerprints(t, manifest, profileName, profile.Shards)
			second := collectShardFingerprints(t, manifest, profileName, profile.Shards)
			if !reflect.DeepEqual(first, second) {
				t.Fatal("corpus partition changed across identical enumeration")
			}
			seen := make(map[string]bool, profile.ExpectedProgramCount)
			for _, shard := range first {
				for _, fingerprint := range shard {
					if seen[fingerprint] {
						t.Errorf("program %s occurred in multiple shards", fingerprint)
					}
					seen[fingerprint] = true
				}
			}
			if len(seen) != profile.ExpectedProgramCount || len(seen) < profile.MinimumPrograms {
				t.Errorf("partition union contains %d programs, want %d and at least %d", len(seen), profile.ExpectedProgramCount, profile.MinimumPrograms)
			}
		})
	}
}

func TestScheduledProfileIsStrictBlockingSuperset(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	blocking := make(map[string]bool, manifest.Blocking.ExpectedProgramCount)
	if err := Enumerate(manifest, func(program Program) error {
		blocking[program.Fingerprint()] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	scheduled := make(map[string]bool, manifest.Scheduled.ExpectedProgramCount)
	for shard := range manifest.Scheduled.Shards {
		if err := EnumerateShard(manifest, "scheduled", shard, func(program Program) error {
			scheduled[program.Fingerprint()] = true
			_ = Interpret(program, manifest.Scheduled.MaxStates)
			return nil
		}); err != nil {
			t.Fatalf("scheduled shard %d: %v", shard, err)
		}
	}
	if len(scheduled) != manifest.Scheduled.ExpectedProgramCount || len(scheduled) <= len(blocking) {
		t.Fatalf("scheduled corpus count = %d, want %d and greater than blocking %d", len(scheduled), manifest.Scheduled.ExpectedProgramCount, len(blocking))
	}
	for fingerprint := range blocking {
		if !scheduled[fingerprint] {
			t.Errorf("scheduled corpus omitted blocking program %s", fingerprint)
		}
	}
}

func TestScheduledProfileAdmitsJointIntegratedSemantics(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	programs, err := generateProfilePrograms(manifest, "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	integrated := make(map[string]Program)
	for _, program := range programs {
		if !strings.HasPrefix(program.CaseID, "scheduled/integrated/") {
			continue
		}
		metrics := program.Metrics()
		if metrics.Procedures < 2 || metrics.Identities < 2 || metrics.CallSites < 1 || metrics.CallDepth < 1 {
			t.Fatalf("integrated program %s omitted a procedure/call/identity dimension: %+v", program.CaseID, metrics)
		}
		calls, returns := 0, 0
		for _, edge := range program.Edges {
			switch edge.Kind {
			case EdgeIntra:
				// Intra-procedure edges do not contribute to call-site metrics.
			case EdgeCall:
				calls++
			case EdgeReturn:
				returns++
			}
		}
		if calls != metrics.CallSites || returns != metrics.CallSites || len(program.InitialFacts) != metrics.Identities {
			t.Fatalf("integrated program %s did not project matched returns and facts: calls=%d returns=%d facts=%d metrics=%+v", program.CaseID, calls, returns, len(program.InitialFacts), metrics)
		}
		integrated[program.CaseID] = program
	}
	if len(integrated) != 2016 {
		t.Fatalf("integrated scheduled corpus count = %d, want 2016", len(integrated))
	}

	assertOutcome := func(caseID string, identity Identity, want Outcome) IdentityResult {
		t.Helper()
		program, ok := integrated[caseID]
		if !ok {
			t.Fatalf("integrated case %q is missing", caseID)
		}
		result := Interpret(program, manifest.Scheduled.MaxStates).Evidence[identity]
		if result.Outcome != want {
			t.Fatalf("%s identity %d outcome = %q, want %q", caseID, identity, result.Outcome, want)
		}
		return result
	}
	validatedCopy := assertOutcome(
		"scheduled/integrated/noop/none/copy/none/none/validated",
		1,
		OutcomeNone,
	)
	if validatedCopy.Summaries == 0 || validatedCopy.SummaryReuses == 0 {
		t.Fatalf("integrated matched-return case omitted summary production/reuse: %+v", validatedCopy)
	}
	assertOutcome("scheduled/integrated/noop/none/copy/none/none/needs-validation", 1, OutcomeViolation)
	assertOutcome("scheduled/integrated/noop/none/kill/none/none/validated", 1, OutcomeViolation)
	assertOutcome("scheduled/integrated/noop/none/copy/none/unsat/needs-validation", 1, OutcomeNone)
}

func loadTestManifest(t *testing.T) BoundsManifest {
	t.Helper()

	manifest, err := LoadBoundsManifest(filepath.Join("..", "..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		t.Fatalf("LoadBoundsManifest() error: %v", err)
	}
	return manifest
}

func collectPrograms(t *testing.T, manifest BoundsManifest) []Program {
	t.Helper()

	programs := make([]Program, 0, manifest.ExpectedProgramCount)
	if err := Enumerate(manifest, func(program Program) error {
		programs = append(programs, program)
		return nil
	}); err != nil {
		t.Fatalf("Enumerate() error: %v", err)
	}
	return programs
}

func collectShardFingerprints(
	t *testing.T,
	manifest BoundsManifest,
	profileName string,
	shards int,
) [][]string {
	t.Helper()

	result := make([][]string, shards)
	for shard := range shards {
		if err := EnumerateShard(manifest, profileName, shard, func(program Program) error {
			result[shard] = append(result[shard], program.Fingerprint())
			return nil
		}); err != nil {
			t.Fatalf("EnumerateShard(%d) error: %v", shard, err)
		}
		sort.Strings(result[shard])
	}
	return result
}

func cloneManifest(t *testing.T, manifest BoundsManifest) BoundsManifest {
	t.Helper()

	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	var cloned BoundsManifest
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	return cloned
}

func hasNodeFeature(program Program, predicate func(Node) bool) bool {
	return slices.ContainsFunc(program.Nodes, predicate)
}
