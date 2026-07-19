// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/types"
	"maps"
	"os"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestProtocolRedBaselineMetaContracts(t *testing.T) {
	t.Parallel()

	config := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo,
		Dir: ".",
	}
	loaded, err := packages.Load(config, ".")
	if err != nil {
		t.Fatalf("load production package: %v", err)
	}
	if packages.PrintErrors(loaded) != 0 || len(loaded) != 1 {
		t.Fatalf("load production package: got %d packages with errors", len(loaded))
	}
	scope := loaded[0].Types.Scope()

	t.Run("joined-state-witness-mismatch", func(t *testing.T) {
		t.Parallel()

		assertRedBaselineStructFields(t, scope, "joined-state-witness-mismatch", "interprocPathEdge", map[string]string{
			"state":   "protocolAbstractState",
			"witness": "[]github.com/invowk/invowk/tools/goplint/goplint.interprocWitnessEdge",
		})
		assertRedBaselineStructFields(t, scope, "joined-state-witness-mismatch", "interprocProcedureSummary", map[string]string{
			"state":           "protocolAbstractState",
			"witness":         "[]github.com/invowk/invowk/tools/goplint/goplint.interprocWitnessEdge",
			"exitStateBefore": "protocolAbstractState",
		})
	})

	t.Run("missing-ssa-fallback", func(t *testing.T) {
		t.Parallel()

		assertRedBaselineStructFields(t, scope, "missing-ssa-fallback", "ssaAvailability", map[string]string{
			"Status": "ssaAvailabilityStatus",
			"Detail": "string",
		})
		for _, name := range []string{
			"ssaAvailabilityBuildFailure",
			"ssaAvailabilityIncompleteDependencies",
			"ssaAvailabilityMissingFunction",
			"ssaAvailabilityMissingClosure",
			"ssaAvailabilityUnsupportedInstruction",
		} {
			if scope.Lookup(name) == nil {
				t.Errorf("red-baseline[missing-ssa-fallback]: production constant %s is missing", name)
			}
		}
	})

	t.Run("late-timeout-discharge", func(t *testing.T) {
		t.Parallel()

		control := scope.Lookup("protocolAnalysisControl")
		if control == nil {
			t.Fatal("red-baseline[late-timeout-discharge]: production type protocolAnalysisControl is missing")
		}
		iface, ok := control.Type().Underlying().(*types.Interface)
		if !ok || iface.NumExplicitMethods() != 1 || iface.ExplicitMethod(0).Name() != "Expired" {
			t.Errorf("red-baseline[late-timeout-discharge]: protocolAnalysisControl does not expose one Expired checkpoint")
		}
		assertRedBaselineStructFields(t, scope, "late-timeout-discharge", "cfgRefinementRequest", map[string]string{
			"Control": "protocolAnalysisControl",
		})
	})

	artifactContracts := []struct {
		name      string
		path      string
		fragments []string
	}{
		{
			name: "concrete-stack-recursion",
			path: "cfa_ifds_tabulation.go",
			fragments: []string{
				"type interprocEntryFact struct",
				"type interprocProcedureSummary struct",
				"SummaryReuses",
			},
		},
		{
			name: "no-return-alias-rebinding",
			path: "cfa_no_return_ssa.go",
			fragments: []string{
				"ssaCallIsDefinitelyNoReturn",
				"ssaFunctionValueIsNoReturn",
				"case *ssa.Phi:",
			},
		},
		{
			name: "catalog-coverage-not-total",
			path: "semantic_catalog_registry.go",
			fragments: []string{
				"registry := diagnosticCategoryRegistry()",
				"func requiredOracleLayersForKind",
				"Route                 semanticProductionRoute",
				"Traversal             semanticTraversalHandler",
			},
		},
		{
			name: "bounded-oracle-fixed-topology",
			path: "../spec/protocol-oracle-bounds.v1.json",
			fragments: []string{
				`"procedure_counts"`,
				`"topologies"`,
				`"expected_program_count"`,
			},
		},
		{
			name: "oracle-bypasses-production",
			path: "protocol_oracle_e2e_test.go",
			fragments: []string{
				"TestProtocolOracleGeneratedGoEndToEnd",
				"runGeneratedGoAnalyzer",
			},
		},
		{
			name: "synthetic-determinism-and-fuzz/determinism",
			path: "protocol_real_determinism_test.go",
			fragments: []string{
				"TestRealAnalyzerDeterminismAcrossPackageFileAndScheduleOrder",
			},
		},
		{
			name: "synthetic-determinism-and-fuzz/fuzz",
			path: "soundness_fuzz_test.go",
			fragments: []string{
				"fuzzInterprocSource(data)",
				"protocoloracle.DecodeFuzzProgram(data)",
				"compareGeneratedGoProgram(t, program, 512",
				"reordered.Fingerprint()",
			},
		},
		{
			name: "mutation-kill-not-causal",
			path: "../cmd/targeted-mutation/runner.go",
			fragments: []string{
				`runControlGuards(workdir, []targetedMutation{mutation}, count, runtime.RunGuard, "preflight")`,
				`"post-mutation control",`,
				"attributedGoTestFailures",
			},
		},
		{
			name: "aggregate-gate-vacuity",
			path: "../testdata/gates/soundness-v1.json",
			fragments: []string{
				`"checks"`,
				`"non_vacuity_markers"`,
				`"required_ci_triggers"`,
			},
		},
		{
			name: "completion-proof-not-reproducible",
			path: "../internal/cleantreeevidence/materialize.go",
			fragments: []string{
				`"GIT_INDEX_FILE"`,
				`"write-tree"`,
				`"commit-tree"`,
			},
		},
	}
	for _, contract := range artifactContracts {
		t.Run(contract.name, func(t *testing.T) {
			t.Parallel()

			assertRedBaselineArtifactFragments(t, contract.name, contract.path, contract.fragments)
		})
	}
}

func assertRedBaselineStructFields(
	t *testing.T,
	scope *types.Scope,
	contract string,
	typeName string,
	wantFields map[string]string,
) {
	t.Helper()

	object := scope.Lookup(typeName)
	if object == nil {
		t.Errorf("red-baseline[%s]: production type %s is missing", contract, typeName)
		return
	}
	structure, ok := object.Type().Underlying().(*types.Struct)
	if !ok {
		t.Errorf("red-baseline[%s]: production type %s is not a struct", contract, typeName)
		return
	}
	remaining := make(map[string]string, len(wantFields))
	maps.Copy(remaining, wantFields)
	for field := range structure.Fields() {
		wantType, required := remaining[field.Name()]
		if !required {
			continue
		}
		gotType := types.TypeString(field.Type(), func(pkg *types.Package) string { return pkg.Path() })
		if gotType != wantType && gotType != "github.com/invowk/invowk/tools/goplint/goplint."+wantType {
			t.Errorf(
				"red-baseline[%s]: production field %s.%s has type %s, want %s",
				contract,
				typeName,
				field.Name(),
				gotType,
				wantType,
			)
		}
		delete(remaining, field.Name())
	}
	for fieldName := range remaining {
		t.Errorf("red-baseline[%s]: production type %s is missing field %s", contract, typeName, fieldName)
	}
}

func assertRedBaselineArtifactFragments(
	t *testing.T,
	contract string,
	path string,
	fragments []string,
) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("red-baseline[%s]: read required artifact %s: %v", contract, path, err)
		return
	}
	for _, fragment := range fragments {
		if !strings.Contains(string(data), fragment) {
			t.Errorf("red-baseline[%s]: artifact %s is missing contract fragment %q", contract, path, fragment)
		}
	}
}
