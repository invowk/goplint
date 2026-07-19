// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/protocoloracle"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

const protocolMetamorphicPrefix = `package probe
type Value string
func (value Value) Validate() error { return nil }
`

type protocolMetamorphicCase struct {
	name        string
	base        string
	transformed string
	wantBase    interprocOutcomeClass
	wantChanged bool
}

func TestProtocolMetamorphicRelations(t *testing.T) {
	t.Parallel()

	for _, test := range protocolMetamorphicCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			checkProtocolMetamorphicCase(t, test)
		})
	}
}

func protocolMetamorphicCases() []protocolMetamorphicCase {
	return []protocolMetamorphicCase{
		{
			name: "alpha renaming",
			base: `func Probe(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	return nil
}`,
			transformed: `func Probe(raw string) error {
	renamed := Value(raw)
	if validationErr := renamed.Validate(); validationErr != nil { return validationErr }
	return nil
}`,
			wantBase: interprocOutcomeSafe,
		},
		{
			name: "nil check equivalence",
			base: `func Probe(raw string) error {
	value := Value(raw)
	err := value.Validate()
	if err != nil { return err }
	return nil
}`,
			transformed: `func Probe(raw string) error {
	value := Value(raw)
	err := value.Validate()
	if err == nil { return nil }
	return err
}`,
			wantBase: interprocOutcomeSafe,
		},
		{
			name: "branch inversion",
			base: `func Probe(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err == nil { return nil } else { return err }
}`,
			transformed: `func Probe(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err } else { return nil }
}`,
			wantBase: interprocOutcomeSafe,
		},
		{
			name: "selector and method value equivalence",
			base: `func Probe(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	return nil
}`,
			transformed: `func Probe(raw string) error {
	value := Value(raw)
	validate := value.Validate
	if err := validate(); err != nil { return err }
	return nil
}`,
			wantBase: interprocOutcomeSafe,
		},
		{
			name: "harmless statement insertion",
			base: `func Probe(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	return nil
}`,
			transformed: `func Probe(raw string) error {
	value := Value(raw)
	_ = len(raw)
	if err := value.Validate(); err != nil { return err }
	return nil
}`,
			wantBase: interprocOutcomeSafe,
		},
		{
			name: "must alias copy",
			base: `func Probe(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	return nil
}`,
			transformed: `func Probe(raw string) error {
	value := Value(raw)
	alias := value
	if err := alias.Validate(); err != nil { return err }
	return nil
}`,
			wantBase: interprocOutcomeSafe,
		},
		{
			name: "alias rebinding changes outcome",
			base: `func Probe(raw string) error {
	value := Value(raw)
	alias := value
	if err := alias.Validate(); err != nil { return err }
	return nil
}`,
			transformed: `func Probe(raw string) error {
	value := Value(raw)
	alias := value
	alias = Value("other")
	if err := alias.Validate(); err != nil { return err }
	return nil
}`,
			wantBase:    interprocOutcomeSafe,
			wantChanged: true,
		},
		{
			name: "changed error continuation changes outcome",
			base: `func Probe(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	return nil
}`,
			transformed: `func Probe(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { _ = err }
	return nil
}`,
			wantBase:    interprocOutcomeSafe,
			wantChanged: true,
		},
	}
}

func checkProtocolMetamorphicCase(t testing.TB, test protocolMetamorphicCase) {
	t.Helper()

	base := evaluateMetamorphicCast(t, protocolMetamorphicPrefix+test.base)
	transformed := evaluateMetamorphicCast(t, protocolMetamorphicPrefix+test.transformed)
	if base != test.wantBase {
		t.Fatalf("base outcome = %s, want %s", base, test.wantBase)
	}
	if test.wantChanged {
		if transformed == base || transformed != interprocOutcomeUnsafe {
			t.Fatalf("transformed outcome = %s, want unsafe change from %s", transformed, base)
		}
		return
	}
	if transformed != base {
		t.Fatalf("transformed outcome = %s, want preserved %s", transformed, base)
	}
}

func TestProtocolOracleEvidence(t *testing.T) {
	t.Parallel()

	manifest, err := protocoloracle.LoadBoundsManifest(filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		t.Fatalf("LoadBoundsManifest() error: %v", err)
	}
	observations := make([]soundnessgate.ObservedMember, 0)
	if err := protocoloracle.Enumerate(manifest, func(program protocoloracle.Program) error {
		result := protocoloracle.Interpret(program, manifest.Blocking.MaxStates)
		if len(result.ByIdentity) != len(program.Identities) {
			return fmt.Errorf("program %s interpreted %d identities, want %d", program.CaseID, len(result.ByIdentity), len(program.Identities))
		}
		observations = append(observations, soundnessgate.ObservedMember{
			PopulationID: "independent-cases",
			MemberID:     program.CaseID,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	relations := protocolMetamorphicCases()
	for _, relation := range relations {
		checkProtocolMetamorphicCase(t, relation)
		observations = append(observations, soundnessgate.ObservedMember{
			PopulationID: "metamorphic-relations",
			MemberID:     relation.name,
		})
	}
	emitSoundnessSubgateReport(t, observedPopulations(t, observations))
}

func evaluateMetamorphicCast(t testing.TB, source string) interprocOutcomeClass {
	t.Helper()

	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "Probe")
	ssaResult := buildSSAForPass(pass)
	parentMap := buildParentMap(declaration.Body)
	assigned, _, closureCalls, methodValueCalls := collectCFACasts(
		pass,
		declaration.Body,
		parentMap,
		func(*ast.FuncLit, int) {},
	)
	if len(assigned) != 1 {
		t.Fatalf("assigned casts = %d, want 1", len(assigned))
	}
	enrichAssignedCastsWithSSA(pass, ssaResult, declaration, assigned)
	cfg := buildProtocolCFG(pass, declaration.Body, ssaResult)
	definitionBlock, definitionIndex := findDefiningBlock(cfg, assigned[0].assign)
	return newInterprocSolverWithSSA(pass, ssaResult).EvaluateCastPath(interprocCastPathInput{
		Decl:            declaration,
		CFG:             cfg,
		DefBlock:        definitionBlock,
		DefIdx:          definitionIndex,
		Target:          assigned[0].target,
		TypeName:        assigned[0].typeName,
		SyncCalls:       collectSynchronousClosureVarCalls(closureCalls),
		MethodCalls:     collectMethodValueValidateCallSet(methodValueCalls),
		MaxStates:       defaultCFGMaxStates,
		SSAAvailability: ssaResult.Availability,
	}).Class
}
