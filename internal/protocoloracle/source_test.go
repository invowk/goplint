// SPDX-License-Identifier: MPL-2.0

package protocoloracle

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

func TestGeneratedGoProjectionTraceCoversDeclaredIntegratedDimensions(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	dimensions := make(map[string]bool)
	if err := Enumerate(manifest, func(program Program) error {
		for _, identity := range program.Identities {
			_, trace, err := GoSourceForIdentityWithTrace(program, identity)
			if err != nil {
				return err
			}
			if trace.TrackedIdentity != identity || trace.Identities != len(program.Identities) ||
				trace.Procedures != program.Shape.Procedures || trace.Nodes != len(program.Nodes) ||
				trace.IntraEdges+trace.CallEdges+trace.ReturnEdges != len(program.Edges) ||
				trace.CallEdges != program.Shape.CallSites || trace.ReturnEdges != program.Shape.CallSites {
				return fmt.Errorf("projection trace for %s identity %d does not match its IR: %+v", program.CaseID, identity, trace)
			}
			for _, dimension := range trace.Dimensions() {
				dimensions[dimension] = true
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, dimension := range []string{
		"aliases", "call-sites", "cast-validation", "constraints", "facts", "return-edges",
	} {
		if !dimensions[dimension] {
			t.Errorf("generated Go projection never exercised declared dimension %q", dimension)
		}
	}
}

func TestGeneratedGoProjectionRejectsUnrepresentableIR(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	programs := make(map[string]Program)
	if err := Enumerate(manifest, func(program Program) error {
		programs[program.CaseID] = program
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		caseID string
		mutate func(*Program)
	}{
		{
			name:   "arbitrary intra edges",
			caseID: "baseline",
			mutate: func(program *Program) {
				program.Edges[0].To, program.Edges[1].To = program.Edges[1].To, program.Edges[0].To
			},
		},
		{
			name:   "callee mutation",
			caseID: "scenario/matched-return",
			mutate: func(program *Program) {
				for index := range program.Nodes {
					if program.Nodes[index].Ref.Procedure == 1 && program.Nodes[index].Operation == OperationValidate {
						program.Nodes[index].Operation = OperationMutate
						program.Nodes[index].Condition = ConditionalResultNone
					}
				}
			},
		},
		{
			name:   "multiple alias transitions",
			caseID: "scenario/alias-copy",
			mutate: func(program *Program) {
				program.Nodes[2].AliasAction = AliasActionKill
				program.Nodes[2].Identity = 1
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			program := programs[test.caseID]
			program.Nodes = append([]Node(nil), program.Nodes...)
			program.Edges = append([]Edge(nil), program.Edges...)
			test.mutate(&program)
			if err := program.Validate(); err != nil {
				t.Fatalf("test mutation must remain normalized IR: %v", err)
			}
			if _, err := GoSourceForIdentity(program, program.Identities[0]); err == nil {
				t.Fatal("GoSourceForIdentity() accepted an IR shape it cannot faithfully project")
			}
		})
	}
}

func TestGeneratedGoCorpusParsesAndTypeChecks(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	err := Enumerate(manifest, func(program Program) error {
		for _, identity := range program.Identities {
			source, sourceErr := GoSourceForIdentity(program, identity)
			if sourceErr != nil {
				return sourceErr
			}
			fileSet := token.NewFileSet()
			file, parseErr := parser.ParseFile(fileSet, program.CaseID+".go", source, parser.ParseComments)
			if parseErr != nil {
				return parseErr
			}
			info := &types.Info{
				Types:      make(map[ast.Expr]types.TypeAndValue),
				Defs:       make(map[*ast.Ident]types.Object),
				Uses:       make(map[*ast.Ident]types.Object),
				Selections: make(map[*ast.SelectorExpr]*types.Selection),
				Instances:  make(map[*ast.Ident]types.Instance),
			}
			configuration := &types.Config{Importer: importer.Default()}
			if _, checkErr := configuration.Check("generated", fileSet, []*ast.File{file}, info); checkErr != nil {
				return checkErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("generated Go corpus is invalid: %v", err)
	}
}

func TestGeneratedGoRendersStructuralAndIdentitySemantics(t *testing.T) {
	t.Parallel()

	manifest := loadTestManifest(t)
	programs := make(map[string]Program)
	if err := Enumerate(manifest, func(program Program) error {
		programs[program.CaseID] = program
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		caseID   string
		identity Identity
		contains []string
		omits    []string
	}{
		{
			name:     "branch join",
			caseID:   "dimension/branch-join/true",
			contains: []string{"if choose { consume(n0); return nil }"},
		},
		{
			name:     "nested call depth",
			caseID:   "dimension/call-depth/2",
			contains: []string{"func procedure2(", "err = procedure1(", "err = procedure2("},
		},
		{
			name:     "recursive call",
			caseID:   "dimension/recursion/true",
			contains: []string{"if choose { err = procedure1(n0, raw, false, effect)"},
		},
		{
			name:     "killed tracked identity",
			caseID:   "scenario/alias-kill",
			identity: 1,
			contains: []string{
				"n1 := Name(\"seed-identity-1\")",
				"rebound1p0n1 := Name(raw)",
				"if err := rebound1p0n1.Validate(); err != nil { return err }",
			},
		},
		{
			name:   "validated initial fact",
			caseID: "dimension/initial-fact/validated",
			contains: []string{
				"n0 := Name(raw)",
				"if err := n0.Validate(); err != nil { return err }",
			},
		},
		{
			name:     "concurrent unknown effect",
			caseID:   "dimension/unknown/concurrent-mutation",
			contains: []string{"go effect.Mutate(&n0)"},
		},
		{
			name:     "escaped heap unknown effect",
			caseID:   "dimension/unknown/escaped-heap-mutation",
			contains: []string{"escaped = &n0"},
		},
		{
			name:   "unsatisfiable witness",
			caseID: "dimension/constraint/unsat",
			contains: []string{
				"if raw == \"left\" {",
				"if raw != \"left\" {",
				"consume(n0)",
			},
			omits: []string{"if err := n0.Validate(); err != nil"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			program, ok := programs[test.caseID]
			if !ok {
				t.Fatalf("generated case %q is missing", test.caseID)
			}
			source, err := GoSourceForIdentity(program, test.identity)
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range test.contains {
				if !strings.Contains(source, fragment) {
					t.Errorf("generated source for %s omitted %q:\n%s", test.caseID, fragment, source)
				}
			}
			for _, fragment := range test.omits {
				if strings.Contains(source, fragment) {
					t.Errorf("generated source for %s unexpectedly contained %q:\n%s", test.caseID, fragment, source)
				}
			}
		})
	}
}
