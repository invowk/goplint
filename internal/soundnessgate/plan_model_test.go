// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"bytes"
	"slices"
	"testing"
)

func TestExecutionPlanValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*ExecutionPlan)
		wantError string
	}{
		{
			name:      "valid",
			mutate:    func(*ExecutionPlan) {},
			wantError: "",
		},
		{
			name: "stale plan identity",
			mutate: func(plan *ExecutionPlan) {
				plan.Workspace.Digest = runnerChangedDigest
			},
			wantError: "from canonical content",
		},
		{
			name: "unsafe command",
			mutate: func(plan *ExecutionPlan) {
				plan.Commands[0].Command = []string{"bash", "-c", "echo unsafe\n"}
				assignPlanID(plan)
			},
			wantError: "is unsafe",
		},
		{
			name: "dependency cycle",
			mutate: func(plan *ExecutionPlan) {
				plan.Dependencies[0].Requires = []string{"work-b"}
				plan.Dependencies[1].Requires = []string{"work-a"}
				assignPlanID(plan)
			},
			wantError: "dependency cycle",
		},
		{
			name: "overlapping shard census",
			mutate: func(plan *ExecutionPlan) {
				plan.Shards[1].MemberIDs = []string{"test-a"}
				assignPlanID(plan)
			},
			wantError: "overlaps shards",
		},
		{
			name: "incomplete shard census",
			mutate: func(plan *ExecutionPlan) {
				plan.Shards = plan.Shards[:1]
				assignPlanID(plan)
			},
			wantError: "incompletely cover census",
		},
		{
			name: "binary identity mutation",
			mutate: func(plan *ExecutionPlan) {
				plan.Commands[0].BinaryDigest = runnerChangedDigest
			},
			wantError: "from canonical content",
		},
		{
			name: "duplicate work unit identity",
			mutate: func(plan *ExecutionPlan) {
				plan.Commands[1].WorkUnitID = plan.Commands[0].WorkUnitID
				assignPlanID(plan)
			},
			wantError: "duplicate work unit",
		},
		{
			name: "incomplete expected reports",
			mutate: func(plan *ExecutionPlan) {
				plan.ExpectedReports = plan.ExpectedReports[:1]
				assignPlanID(plan)
			},
			wantError: "expected report count",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			plan := validExecutionPlan()
			test.mutate(&plan)
			assertGateErrorContains(t, plan.Validate(), test.wantError)
		})
	}
}

func TestExecutionPlanBindingsInvalidateIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*ExecutionPlan)
	}{
		{name: "workspace", mutate: func(plan *ExecutionPlan) { plan.Workspace.Digest = runnerChangedDigest }},
		{name: "manifest", mutate: func(plan *ExecutionPlan) { plan.Manifest.Digest = runnerChangedDigest }},
		{name: "registry", mutate: func(plan *ExecutionPlan) { plan.Registry.Digest = runnerChangedDigest }},
		{name: "toolchain", mutate: func(plan *ExecutionPlan) { plan.Toolchain.GoVersion = "go1.26.6" }},
		{name: "command", mutate: func(plan *ExecutionPlan) { plan.Commands[0].Command[1] = "vet" }},
		{name: "binary", mutate: func(plan *ExecutionPlan) { plan.Commands[0].BinaryDigest = runnerChangedDigest }},
		{name: "census", mutate: func(plan *ExecutionPlan) { plan.Censuses[0].MemberIDs[0] = "test-changed" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			plan := validExecutionPlan()
			test.mutate(&plan)
			assertGateErrorContains(t, plan.Validate(), "from canonical content")
		})
	}
}

func TestValidatePlanShardsScopesCoverageByModeAndIteration(t *testing.T) {
	t.Parallel()

	plan := validExecutionPlan()
	commands, err := validatePlanCommands(plan.Commands, plan.Resources)
	if err != nil {
		t.Fatal(err)
	}
	censuses, err := validatePlanCensuses(plan.Censuses)
	if err != nil {
		t.Fatal(err)
	}
	secondIteration := make([]PlanShardBinding, 0, len(plan.Shards))
	ids := []string{"shard-repeat-a", "shard-repeat-b"}
	for index, shard := range plan.Shards {
		shard.ID = ids[index]
		shard.Iteration = 2
		secondIteration = append(secondIteration, shard)
	}
	shards := append(slices.Clone(plan.Shards), secondIteration...)
	slices.SortFunc(shards, comparePlanShards)
	if err := validatePlanShards(shards, commands, censuses); err != nil {
		t.Fatalf("validatePlanShards() error = %v", err)
	}
}

func TestExecutionPlanCalculateIDIsDeterministic(t *testing.T) {
	t.Parallel()

	left := validExecutionPlan()
	right := validExecutionPlan()
	left.PlanID = ""
	right.PlanID = ""
	leftID, err := left.CalculateID()
	if err != nil {
		t.Fatalf("left.CalculateID() error = %v", err)
	}
	rightID, err := right.CalculateID()
	if err != nil {
		t.Fatalf("right.CalculateID() error = %v", err)
	}
	if !bytes.Equal([]byte(leftID), []byte(rightID)) {
		t.Fatalf("plan ids differ: %q != %q", leftID, rightID)
	}
}

func validExecutionPlan() ExecutionPlan {
	commandDigestA, err := CommandDigest(Subgate{
		ID: "subgate-a", WorkingDirectory: ".", Command: []string{"go", "test", "./..."},
	})
	if err != nil {
		panic(err)
	}
	commandDigestB, err := CommandDigest(Subgate{
		ID: "subgate-b", WorkingDirectory: "tools/goplint", Command: []string{"./scripts/check.sh"},
	})
	if err != nil {
		panic(err)
	}
	toolchainDigest := mustBindingDigest(struct {
		GoVersion string `json:"go_version"`
		GOOS      string `json:"goos"`
		GOARCH    string `json:"goarch"`
	}{GoVersion: "go1.26.5", GOOS: "linux", GOARCH: "amd64"})
	censusDigest := mustBindingDigest(struct {
		ID        string   `json:"id"`
		Kind      string   `json:"kind"`
		MemberIDs []string `json:"member_ids"`
	}{ID: "analyzer-tests", Kind: "go-tests", MemberIDs: []string{"test-a", "test-b"}})
	plan := ExecutionPlan{
		FormatVersion: ExecutionPlanFormatVersion,
		Profile:       ProfileCore,
		Workspace: WorkspaceBinding{
			Root:   ".",
			Digest: runnerTestWorkspaceDigest,
		},
		Manifest: ArtifactBinding{
			Path:   "tools/goplint/spec/soundness-gate.v1.json",
			Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		Registry: ArtifactBinding{
			Path:   "tools/goplint/spec/semantic-evidence.v2.json",
			Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		},
		Toolchain: ToolchainBinding{
			GoVersion: "go1.26.5",
			GOOS:      "linux",
			GOARCH:    "amd64",
			Digest:    toolchainDigest,
		},
		Resources: ResourceBudget{
			CPUUnits:        4,
			MemoryBytes:     16 * 1024 * 1024 * 1024,
			MaximumWorkers:  2,
			RunnerClass:     "test-runner",
			SerialReference: false,
		},
		Commands: []PlanCommandBinding{
			{
				WorkUnitID:        "work-a",
				SubgateID:         "subgate-a",
				WorkingDirectory:  ".",
				Command:           []string{"go", "test", "./..."},
				CommandDigest:     commandDigestA,
				BinaryDigest:      "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				ReservedResources: ResourceReservation{CPUUnits: 2, MemoryBytes: 1024, WorkerSlots: 1},
				ExclusivityGroups: []string{},
				Distributable:     true,
				TimeoutSeconds:    30,
			},
			{
				WorkUnitID:        "work-b",
				SubgateID:         "subgate-b",
				WorkingDirectory:  "tools/goplint",
				Command:           []string{"./scripts/check.sh"},
				CommandDigest:     commandDigestB,
				BinaryDigest:      "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				ReservedResources: ResourceReservation{CPUUnits: 2, MemoryBytes: 1024, WorkerSlots: 1},
				ExclusivityGroups: []string{},
				Distributable:     true,
				TimeoutSeconds:    30,
			},
		},
		Dependencies: []PlanDependencyBinding{
			{WorkUnitID: "work-a", Requires: []string{}},
			{WorkUnitID: "work-b", Requires: []string{"work-a"}},
		},
		Censuses: []PlanCensusBinding{
			{
				ID:        "analyzer-tests",
				Kind:      "go-tests",
				MemberIDs: []string{"test-a", "test-b"},
				Digest:    censusDigest,
			},
		},
		Shards: []PlanShardBinding{
			{
				ID:             "shard-a",
				WorkUnitID:     "work-a",
				CensusID:       "analyzer-tests",
				Mode:           "normal",
				Iteration:      1,
				MemberIDs:      []string{"test-a"},
				TotalWeight:    10,
				TimeoutSeconds: 30,
			},
			{
				ID:             "shard-b",
				WorkUnitID:     "work-b",
				CensusID:       "analyzer-tests",
				Mode:           "normal",
				Iteration:      1,
				MemberIDs:      []string{"test-b"},
				TotalWeight:    20,
				TimeoutSeconds: 30,
			},
		},
		ExpectedReports: []PlanExpectedReportBinding{
			{
				WorkUnitID:              "work-a",
				SubgateID:               "subgate-a",
				ReportFile:              "report.json",
				CommandDigest:           commandDigestA,
				RequiredRegistrationIDs: []string{},
				RequiredPopulations:     []PopulationRequirement{{ID: "cases", Minimum: 1}},
			},
			{
				WorkUnitID:              "work-b",
				SubgateID:               "subgate-b",
				ReportFile:              "report.json",
				CommandDigest:           commandDigestB,
				RequiredRegistrationIDs: []string{},
				RequiredPopulations:     []PopulationRequirement{{ID: "cases", Minimum: 1}},
			},
		},
	}
	assignPlanID(&plan)
	return plan
}

func assignPlanID(plan *ExecutionPlan) {
	id, err := plan.CalculateID()
	if err != nil {
		panic(err)
	}
	plan.PlanID = id
}

func mustBindingDigest(value any) string {
	digest, err := calculateBindingDigest(value)
	if err != nil {
		panic(err)
	}
	return digest
}
