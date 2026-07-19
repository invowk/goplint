// SPDX-License-Identifier: MPL-2.0

package protocoloracle

import "testing"

func TestProgramValidateRejectsUnsupportedEnums(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	programs := collectPrograms(t, manifest)
	base := programs[0]
	tests := []struct {
		name   string
		mutate func(*Program)
	}{
		{name: "operation", mutate: func(program *Program) { program.Nodes[0].Operation = "future-operation" }},
		{name: "conditional result", mutate: func(program *Program) { program.Nodes[0].Condition = "future-result" }},
		{name: "alias action", mutate: func(program *Program) { program.Nodes[0].AliasAction = "future-alias" }},
		{name: "unknown effect", mutate: func(program *Program) { program.Nodes[0].UnknownEffect = "future-effect" }},
		{name: "constraint", mutate: func(program *Program) { program.Nodes[0].Constraint = "future-constraint" }},
		{name: "edge kind", mutate: func(program *Program) { program.Edges[0].Kind = "future-edge" }},
		{name: "topology", mutate: func(program *Program) { program.Shape.Topology = "future-topology" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			program := base
			program.Nodes = append([]Node(nil), base.Nodes...)
			program.Edges = append([]Edge(nil), base.Edges...)
			test.mutate(&program)
			if err := program.Validate(); err == nil {
				t.Fatal("Program.Validate() accepted an unsupported enum value")
			}
		})
	}
}

func TestReferenceInterpreterRemembersUnsafeConsumption(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	programs := collectPrograms(t, manifest)
	program := programs[0]
	program.CaseID = "consume-before-validation"
	program.Nodes[0].Operation = OperationConsume
	program.Nodes[1].Operation = OperationValidate
	program.Nodes[1].Condition = ConditionalResultNil
	program.Nodes[len(program.Nodes)-1].Terminal = true
	if err := program.Validate(); err != nil {
		t.Fatalf("Program.Validate() error: %v", err)
	}
	if got := Interpret(program, manifest.Blocking.MaxStates).ByIdentity[0]; got != OutcomeViolation {
		t.Fatalf("Interpret() outcome = %q, want %q after consumption before validation", got, OutcomeViolation)
	}
}

func TestReferenceInterpreterPreservesValidatedFactAcrossIgnoredRecheck(t *testing.T) {
	t.Parallel()

	state := referenceState{}
	state.aliases[0] = 0
	state.validated[0] = true
	got := applyReferenceNode(state, Node{
		Operation: OperationValidate,
		Identity:  0,
		Condition: ConditionalResultUnknown,
	}, 0)
	if !got.validated[0] || got.uncertainty != 0 {
		t.Fatalf("ignored recheck changed an established validation fact: %+v", got)
	}
}
