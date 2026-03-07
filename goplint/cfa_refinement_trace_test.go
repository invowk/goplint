// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
)

func TestWriteRefinementTraceToSinkWritesTraceRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "findings.jsonl")
	analyzer := NewAnalyzer()
	if err := analyzer.Flags.Set("emit-findings-jsonl", path); err != nil {
		t.Fatalf("set emit-findings-jsonl flag: %v", err)
	}

	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 64)
	pos := file.Pos(12)
	pass := &analysis.Pass{
		Analyzer: analyzer,
		Fset:     fset,
	}

	result := interprocPathResult{
		WitnessRecord: cfgWitnessRecord{
			Category:    CategoryMissingConstructorValidate,
			FindingID:   "id-trace",
			CFGPath:     []int32{0, 2},
			WitnessHash: "cfgw1_deadbeef",
		},
		PhaseC: cfgPhaseCResult{
			Enabled:              true,
			FeasibilityEngine:    cfgFeasibilityEngineSMT,
			FeasibilityResult:    cfgFeasibilityResultSAT,
			RefinementStatus:     cfgRefinementStatusUnsafe,
			RefinementIterations: 1,
			RefinementTrigger:    cfgRefinementTriggerUnsafeCandidate,
			WitnessHash:          "cfgw1_deadbeef",
		},
	}

	writeRefinementTraceToSink(pass, pos, result)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read findings stream: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSONL record, got %d", len(lines))
	}

	var record FindingStreamRecord
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("decode JSONL record: %v", err)
	}
	if record.Kind != findingStreamKindRefinementTrace {
		t.Fatalf("record.Kind = %q, want %q", record.Kind, findingStreamKindRefinementTrace)
	}
	if record.Category != CategoryMissingConstructorValidate || record.ID != "id-trace" {
		t.Fatalf("unexpected trace identity: %+v", record)
	}
	if record.Message != cfgRefinementStatusUnsafe {
		t.Fatalf("record.Message = %q, want %q", record.Message, cfgRefinementStatusUnsafe)
	}
	if got := record.Meta["cfg_refinement_status"]; got != cfgRefinementStatusUnsafe {
		t.Fatalf("record.Meta[cfg_refinement_status] = %q, want %q", got, cfgRefinementStatusUnsafe)
	}
	if got := record.Meta["cfg_refinement_witness_hash"]; got != "cfgw1_deadbeef" {
		t.Fatalf("record.Meta[cfg_refinement_witness_hash] = %q, want cfgw1_deadbeef", got)
	}
	if got := record.Meta["cfg_witness_blocks"]; got != "0,2" {
		t.Fatalf("record.Meta[cfg_witness_blocks] = %q, want 0,2", got)
	}
}

func TestWriteRefinementTraceToSinkSkipsWhenPhaseCDisabled(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "findings.jsonl")
	analyzer := NewAnalyzer()
	if err := analyzer.Flags.Set("emit-findings-jsonl", path); err != nil {
		t.Fatalf("set emit-findings-jsonl flag: %v", err)
	}

	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 8)
	pass := &analysis.Pass{
		Analyzer: analyzer,
		Fset:     fset,
	}

	writeRefinementTraceToSink(pass, file.Pos(1), interprocPathResult{})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no trace file when Phase C is disabled, stat err=%v", err)
	}
}

func TestPhaseCRefinementGate(t *testing.T) {
	t.Parallel()

	t.Run("curated inconclusive corpus shrinks under refinement", func(t *testing.T) {
		t.Parallel()

		castBase := runPhaseCPackage(t, "cfa_cast_inconclusive", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-cast-validation", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-max-states", "1")
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineOff)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOff)
		})
		castRefined := runPhaseCPackage(t, "cfa_cast_inconclusive", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-cast-validation", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-max-states", "1")
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineSMT)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOnce)
		})
		constructorBase := runPhaseCPackage(t, "constructorvalidates_inconclusive", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-constructor-validates", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-max-states", "1")
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineOff)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOff)
		})
		constructorRefined := runPhaseCPackage(t, "constructorvalidates_inconclusive", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-constructor-validates", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-max-states", "1")
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineSMT)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOnce)
		})

		baseInconclusive := countDiagnosticCategory(castBase.Diagnostics, CategoryUnvalidatedCastInconclusive) +
			countDiagnosticCategory(constructorBase.Diagnostics, CategoryMissingConstructorValidateInc)
		refinedInconclusive := countDiagnosticCategory(castRefined.Diagnostics, CategoryUnvalidatedCastInconclusive) +
			countDiagnosticCategory(constructorRefined.Diagnostics, CategoryMissingConstructorValidateInc)
		if refinedInconclusive >= baseInconclusive {
			t.Fatalf(
				"expected refined inconclusive count to shrink, base=%d refined=%d",
				baseInconclusive,
				refinedInconclusive,
			)
		}
		if got := countDiagnosticCategory(constructorRefined.Diagnostics, CategoryMissingConstructorValidate); got != 1 {
			t.Fatalf("expected refined constructor corpus to preserve unsafe finding, got %d", got)
		}
	})

	t.Run("unknown feasibility never becomes safe", func(t *testing.T) {
		t.Parallel()

		cfg := mustBuildTestCFG(t, `package p
import "strings"
func sample(raw string) {
	if strings.HasPrefix(raw, "prod") {
		return
	}
}
`)
		path := witnessPathFromChoices(t, cfg, true)
		controller := newCFGRefinementController(cfgPhaseCOptions{
			FeasibilityEngine:     cfgFeasibilityEngineSMT,
			RefinementMode:        cfgRefinementModeOnce,
			FeasibilityMaxQueries: 4,
			FeasibilityTimeout:    50 * time.Millisecond,
		})

		called := false
		result := controller.Refine(cfgRefinementRequest{
			CFG:       cfg,
			Result:    interprocPathResult{Class: interprocOutcomeUnsafe, Witness: path},
			Category:  CategoryUnvalidatedCast,
			FindingID: "id-unknown",
			CallChain: []string{"pkg.sample"},
			Rerun: func(cfgRefinementOverride) interprocPathResult {
				called = true
				return interprocPathResult{Class: interprocOutcomeSafe}
			},
		})

		if called {
			t.Fatal("expected unknown feasibility result to skip refinement rerun")
		}
		if result.Class != interprocOutcomeUnsafe {
			t.Fatalf("result.Class = %q, want %q", result.Class, interprocOutcomeUnsafe)
		}
		if result.PhaseC.FeasibilityResult != cfgFeasibilityResultUnknown {
			t.Fatalf("result.PhaseC.FeasibilityResult = %q, want %q", result.PhaseC.FeasibilityResult, cfgFeasibilityResultUnknown)
		}
		if result.PhaseC.FeasibilityReason != cfgFeasibilityReasonUnsupportedPredicate {
			t.Fatalf("result.PhaseC.FeasibilityReason = %q, want %q", result.PhaseC.FeasibilityReason, cfgFeasibilityReasonUnsupportedPredicate)
		}
	})

	t.Run("unknown feasibility keeps inconclusive witnesses non-safe", func(t *testing.T) {
		t.Parallel()

		cfg := mustBuildTestCFG(t, `package p
import "strings"
func sample(raw string) {
	if strings.HasPrefix(raw, "prod") {
		return
	}
}
`)
		path := witnessPathFromChoices(t, cfg, true)
		controller := newCFGRefinementController(cfgPhaseCOptions{
			FeasibilityEngine:     cfgFeasibilityEngineSMT,
			RefinementMode:        cfgRefinementModeOnce,
			FeasibilityMaxQueries: 4,
			FeasibilityTimeout:    50 * time.Millisecond,
		})

		called := false
		result := controller.Refine(cfgRefinementRequest{
			CFG: cfg,
			Result: interprocPathResult{
				Class:   interprocOutcomeInconclusive,
				Reason:  pathOutcomeReasonStateBudget,
				Witness: path,
			},
			Category:  CategoryUnvalidatedCastInconclusive,
			FindingID: "id-inconclusive-unknown",
			CallChain: []string{"pkg.sample"},
			Rerun: func(cfgRefinementOverride) interprocPathResult {
				called = true
				return interprocPathResult{Class: interprocOutcomeSafe}
			},
		})

		if !called {
			t.Fatal("expected unknown feasibility result to attempt a bounded inconclusive rerun")
		}
		if result.Class != interprocOutcomeInconclusive {
			t.Fatalf("result.Class = %q, want %q", result.Class, interprocOutcomeInconclusive)
		}
		if result.PhaseC.FeasibilityResult != cfgFeasibilityResultUnknown {
			t.Fatalf("result.PhaseC.FeasibilityResult = %q, want %q", result.PhaseC.FeasibilityResult, cfgFeasibilityResultUnknown)
		}
		if result.PhaseC.RefinementStatus != cfgRefinementStatusInconclusiveRefined {
			t.Fatalf("result.PhaseC.RefinementStatus = %q, want %q", result.PhaseC.RefinementStatus, cfgRefinementStatusInconclusiveRefined)
		}
	})

	t.Run("unresolved targets can use bounded target-resolution rerun", func(t *testing.T) {
		t.Parallel()

		cfg := mustBuildTestCFG(t, `package p
func sample(raw string) {
	if raw == "" {
		return
	}
}
`)
		path := witnessPathFromChoices(t, cfg, true)
		controller := newCFGRefinementController(cfgPhaseCOptions{
			FeasibilityEngine:     cfgFeasibilityEngineSMT,
			RefinementMode:        cfgRefinementModeOnce,
			FeasibilityMaxQueries: 4,
			FeasibilityTimeout:    50 * time.Millisecond,
		})

		called := false
		result := controller.Refine(cfgRefinementRequest{
			CFG: cfg,
			Result: interprocPathResult{
				Class:   interprocOutcomeInconclusive,
				Reason:  pathOutcomeReasonUnresolvedTarget,
				Witness: path,
			},
			Category:  CategoryUnvalidatedCastInconclusive,
			FindingID: "id-unresolved",
			CallChain: []string{"pkg.sample"},
			Rerun: func(cfgRefinementOverride) interprocPathResult {
				called = true
				return interprocPathResult{Class: interprocOutcomeSafe}
			},
		})

		if !called {
			t.Fatal("expected unresolved-target result to trigger a bounded rerun")
		}
		if result.Class != interprocOutcomeSafe {
			t.Fatalf("result.Class = %q, want %q", result.Class, interprocOutcomeSafe)
		}
		if result.PhaseC.RefinementStatus != cfgRefinementStatusProvenSafe {
			t.Fatalf("result.PhaseC.RefinementStatus = %q, want %q", result.PhaseC.RefinementStatus, cfgRefinementStatusProvenSafe)
		}
		if result.PhaseC.RefinementIterations != 1 {
			t.Fatalf("result.PhaseC.RefinementIterations = %d, want 1", result.PhaseC.RefinementIterations)
		}
	})

	t.Run("cfg-only helper calls stay safe without phase c reruns", func(t *testing.T) {
		t.Parallel()

		base := runPhaseCPackage(t, "cfa_phasec_cfg_resolution", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-cast-validation", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-inconclusive-policy", cfgInconclusivePolicyError)
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineOff)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOff)
		})
		refined := runPhaseCPackage(t, "cfa_phasec_cfg_resolution", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-cast-validation", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-inconclusive-policy", cfgInconclusivePolicyError)
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineSMT)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOnce)
		})

		if got := countDiagnosticCategory(base.Diagnostics, CategoryUnvalidatedCast); got != 0 {
			t.Fatalf("expected cfg helper fixture to stay safe without cast findings, got %d", got)
		}
		if got := countDiagnosticCategory(base.Diagnostics, CategoryUnvalidatedCastInconclusive); got != 0 {
			t.Fatalf("expected cfg helper fixture to avoid inconclusive cast findings, got %d", got)
		}
		if got := countDiagnosticCategory(refined.Diagnostics, CategoryUnvalidatedCast); got != 0 {
			t.Fatalf("expected refined cfg helper fixture to remain safe, got %d", got)
		}
		if got := countDiagnosticCategory(refined.Diagnostics, CategoryUnvalidatedCastInconclusive); got != 0 {
			t.Fatalf("expected refined cfg helper fixture to remain free of inconclusive cast findings, got %d", got)
		}
		if trace := findFirstTraceRecord(refined.Records); trace != nil {
			t.Fatalf("expected no refinement trace for already-safe cfg helper fixture, got %+v", trace)
		}
	})

	t.Run("recursion-cycle ubv fixture stays conservative without cast inconclusives", func(t *testing.T) {
		t.Parallel()

		base := runPhaseCPackage(t, "cfa_phasec_recursion_cycle", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-cast-validation", "true")
			setFlag(t, analyzer, "check-use-before-validate", "true")
			setFlag(t, analyzer, "ubv-mode", ubvModeEscape)
			setFlag(t, analyzer, "cfg-backend", cfgBackendSSA)
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-inconclusive-policy", cfgInconclusivePolicyError)
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineOff)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOff)
		})
		refined := runPhaseCPackage(t, "cfa_phasec_recursion_cycle", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-cast-validation", "true")
			setFlag(t, analyzer, "check-use-before-validate", "true")
			setFlag(t, analyzer, "ubv-mode", ubvModeEscape)
			setFlag(t, analyzer, "cfg-backend", cfgBackendSSA)
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-inconclusive-policy", cfgInconclusivePolicyError)
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineSMT)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOnce)
		})

		if got := countDiagnosticCategory(base.Diagnostics, CategoryUnvalidatedCast); got != 1 {
			t.Fatalf("expected recursion-cycle fixture to stay conservatively unsafe, got %d", got)
		}
		if got := countDiagnosticCategory(base.Diagnostics, CategoryUnvalidatedCastInconclusive); got != 0 {
			t.Fatalf("expected recursion-cycle fixture to avoid cast inconclusive findings, got %d", got)
		}
		if got := countDiagnosticCategory(refined.Diagnostics, CategoryUnvalidatedCast); got != 1 {
			t.Fatalf("expected recursion-cycle fixture to remain conservatively unsafe under refinement, got %d", got)
		}
		if got := countDiagnosticCategory(refined.Diagnostics, CategoryUnvalidatedCastInconclusive); got != 0 {
			t.Fatalf("expected recursion-cycle fixture to remain free of cast inconclusive findings, got %d", got)
		}
		trace := findFirstTraceRecord(refined.Records)
		if trace == nil || trace.Message != cfgRefinementStatusUnsafe {
			t.Fatalf("expected unsafe refinement trace for already-conservative recursion-cycle fixture, got %+v", trace)
		}
	})

	t.Run("recursion-cycle refinement upgrades constructor fixtures", func(t *testing.T) {
		t.Parallel()

		base := runPhaseCPackage(t, "cfa_phasec_constructor_cycle", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-constructor-validates", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-inconclusive-policy", cfgInconclusivePolicyError)
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineOff)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOff)
		})
		refined := runPhaseCPackage(t, "cfa_phasec_constructor_cycle", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-constructor-validates", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-inconclusive-policy", cfgInconclusivePolicyError)
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineSMT)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOnce)
		})

		if got := countDiagnosticCategory(base.Diagnostics, CategoryMissingConstructorValidateInc); got != 1 {
			t.Fatalf("expected constructor recursion fixture to start inconclusive, got %d", got)
		}
		if got := countDiagnosticCategory(refined.Diagnostics, CategoryMissingConstructorValidate); got != 1 {
			t.Fatalf("expected constructor recursion fixture to refine to unsafe finding, got %d", got)
		}
		if got := countDiagnosticCategory(refined.Diagnostics, CategoryMissingConstructorValidateInc); got != 0 {
			t.Fatalf("expected constructor recursion inconclusive findings to clear under refinement, got %d", got)
		}
	})

	t.Run("unsat witness can discharge to proven-safe", func(t *testing.T) {
		t.Parallel()

		cfg := mustBuildTestCFG(t, `package p
func sample(raw string) {
	if raw == "" {
		if raw != "" {
			return
		}
	}
}
`)
		path := witnessPathFromChoices(t, cfg, true, true)
		controller := newCFGRefinementController(cfgPhaseCOptions{
			FeasibilityEngine:     cfgFeasibilityEngineSMT,
			RefinementMode:        cfgRefinementModeOnce,
			FeasibilityMaxQueries: 4,
			FeasibilityTimeout:    50 * time.Millisecond,
		})

		called := false
		result := controller.Refine(cfgRefinementRequest{
			CFG:       cfg,
			Result:    interprocPathResult{Class: interprocOutcomeUnsafe, Witness: path},
			Category:  CategoryUnvalidatedCast,
			FindingID: "id-unsat",
			CallChain: []string{"pkg.sample"},
			Rerun: func(override cfgRefinementOverride) interprocPathResult {
				called = true
				if len(override.DischargedWitnesses) != 1 {
					t.Fatalf("expected one discharged witness hash, got %d", len(override.DischargedWitnesses))
				}
				return interprocPathResult{Class: interprocOutcomeSafe}
			},
		})

		if !called {
			t.Fatal("expected UNSAT feasibility result to trigger a refinement rerun")
		}
		if result.Class != interprocOutcomeSafe {
			t.Fatalf("result.Class = %q, want %q", result.Class, interprocOutcomeSafe)
		}
		if result.PhaseC.FeasibilityResult != cfgFeasibilityResultUNSAT {
			t.Fatalf("result.PhaseC.FeasibilityResult = %q, want %q", result.PhaseC.FeasibilityResult, cfgFeasibilityResultUNSAT)
		}
		if result.PhaseC.RefinementStatus != cfgRefinementStatusProvenSafe {
			t.Fatalf("result.PhaseC.RefinementStatus = %q, want %q", result.PhaseC.RefinementStatus, cfgRefinementStatusProvenSafe)
		}
	})

	t.Run("already safe results skip phase c provenance", func(t *testing.T) {
		t.Parallel()

		cfg := mustBuildTestCFG(t, `package p
func sample(raw string) {
	if raw == "" {
		return
	}
}
`)
		controller := newCFGRefinementController(cfgPhaseCOptions{
			FeasibilityEngine:     cfgFeasibilityEngineSMT,
			RefinementMode:        cfgRefinementModeOnce,
			FeasibilityMaxQueries: 4,
			FeasibilityTimeout:    50 * time.Millisecond,
		})

		called := false
		result := controller.Refine(cfgRefinementRequest{
			CFG:       cfg,
			Result:    interprocPathResult{Class: interprocOutcomeSafe, Witness: []int32{0}},
			Category:  CategoryUnvalidatedCast,
			FindingID: "id-safe",
			CallChain: []string{"pkg.sample"},
			Rerun: func(cfgRefinementOverride) interprocPathResult {
				called = true
				return interprocPathResult{Class: interprocOutcomeUnsafe}
			},
		})

		if called {
			t.Fatal("expected safe result to skip phase c rerun")
		}
		if result.Class != interprocOutcomeSafe {
			t.Fatalf("result.Class = %q, want %q", result.Class, interprocOutcomeSafe)
		}
		if result.PhaseC.Enabled {
			t.Fatal("expected safe result to avoid phase c provenance when no witness was refined")
		}
	})

	t.Run("discharged solver hashes are honored for cast and ubv propagation", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			factFamily ifdsFactFamily
			factKey    string
			ubvMode    string
		}{
			{name: "cast", factFamily: ifdsFactFamilyCastNeedsValidate, factKey: "cast|origin|target|type", ubvMode: ubvModeOrder},
			{name: "ubv-order", factFamily: ifdsFactFamilyUBVNeedsValidateBefore, factKey: "ubv|origin|target|type|order", ubvMode: ubvModeOrder},
			{name: "ubv-escape", factFamily: ifdsFactFamilyUBVNeedsValidateBefore, factKey: "ubv|origin|target|type|escape", ubvMode: ubvModeEscape},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				graph, start, terminal := newUnsafeInterprocTestGraph()
				callChain := []string{"pkg.sample"}
				hashFunc := newInterprocWitnessHashFunc(callChain, tt.factFamily, tt.factKey, tt.ubvMode)

				unsafe := runIFDSPropagation(
					graph,
					start,
					4,
					4,
					callChain,
					nil,
					hashFunc,
					func(interprocNodeID, ast.Node, ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
						return ideEdgeFuncIdentity, pathOutcomeReasonNone
					},
					func(nodeID interprocNodeID, _ ast.Node, state ideValidationState) bool {
						return nodeID.Key() == terminal.Key() && state == ideStateNeedsValidate
					},
					nil,
				)
				if unsafe.Class != interprocOutcomeUnsafe {
					t.Fatalf("unsafe.Class = %q, want %q", unsafe.Class, interprocOutcomeUnsafe)
				}

				dischargeHash := hashFunc(unsafe.Witness, cfgRefinementTriggerUnsafeCandidate)
				if dischargeHash == "" {
					t.Fatal("expected discharge hash for unsafe witness")
				}

				safe := runIFDSPropagation(
					graph,
					start,
					4,
					4,
					callChain,
					map[string]bool{dischargeHash: true},
					hashFunc,
					func(interprocNodeID, ast.Node, ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
						return ideEdgeFuncIdentity, pathOutcomeReasonNone
					},
					func(nodeID interprocNodeID, _ ast.Node, state ideValidationState) bool {
						return nodeID.Key() == terminal.Key() && state == ideStateNeedsValidate
					},
					nil,
				)
				if safe.Class != interprocOutcomeSafe {
					t.Fatalf("safe.Class = %q, want %q", safe.Class, interprocOutcomeSafe)
				}
			})
		}
	})

	t.Run("explanation artifacts stay complete", func(t *testing.T) {
		t.Parallel()

		result := runPhaseCPackage(t, "constructorvalidates_inconclusive", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-constructor-validates", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-max-states", "1")
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineSMT)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOnce)
		})

		if len(result.Diagnostics) != 1 {
			t.Fatalf("expected 1 diagnostic, got %d", len(result.Diagnostics))
		}
		diag := result.Diagnostics[0]
		for _, key := range []string{
			"cfg_feasibility_engine",
			"cfg_feasibility_result",
			"cfg_refinement_status",
			"cfg_refinement_iterations",
			"cfg_refinement_trigger",
			"cfg_refinement_witness_hash",
		} {
			if got := FindingMetaFromDiagnosticURL(diag.URL, key); got == "" {
				t.Fatalf("diagnostic missing refinement URL metadata %q", key)
			}
		}
		trace := findFirstTraceRecord(result.Records)
		if trace == nil {
			t.Fatal("expected refinement trace record in findings stream")
		}
		if trace.Kind != findingStreamKindRefinementTrace {
			t.Fatalf("trace.Kind = %q, want %q", trace.Kind, findingStreamKindRefinementTrace)
		}
		if trace.Meta["cfg_refinement_witness_hash"] == "" {
			t.Fatal("trace record is missing cfg_refinement_witness_hash")
		}
	})

	t.Run("witness hashes and refined outcomes stay deterministic", func(t *testing.T) {
		t.Parallel()

		first := runPhaseCPackage(t, "constructorvalidates_inconclusive", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-constructor-validates", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-max-states", "1")
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineSMT)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOnce)
		})
		second := runPhaseCPackage(t, "constructorvalidates_inconclusive", func(analyzer *analysis.Analyzer) {
			setFlag(t, analyzer, "check-constructor-validates", "true")
			setFlag(t, analyzer, "cfg-interproc-engine", cfgInterprocEngineIFDS)
			setFlag(t, analyzer, "cfg-max-states", "1")
			setFlag(t, analyzer, "cfg-feasibility-engine", cfgFeasibilityEngineSMT)
			setFlag(t, analyzer, "cfg-refinement-mode", cfgRefinementModeOnce)
		})

		if len(first.Diagnostics) != 1 || len(second.Diagnostics) != 1 {
			t.Fatalf("expected 1 diagnostic per run, got first=%d second=%d", len(first.Diagnostics), len(second.Diagnostics))
		}
		firstHash := FindingMetaFromDiagnosticURL(first.Diagnostics[0].URL, "cfg_refinement_witness_hash")
		secondHash := FindingMetaFromDiagnosticURL(second.Diagnostics[0].URL, "cfg_refinement_witness_hash")
		if firstHash == "" || secondHash == "" {
			t.Fatal("expected diagnostic witness hashes on both runs")
		}
		if firstHash != secondHash {
			t.Fatalf("witness hash changed across identical runs: first=%q second=%q", firstHash, secondHash)
		}
		if first.Diagnostics[0].Category != second.Diagnostics[0].Category {
			t.Fatalf("diagnostic category changed across identical runs: first=%q second=%q", first.Diagnostics[0].Category, second.Diagnostics[0].Category)
		}
	})
}

type phaseCPackageResult struct {
	Diagnostics []analysis.Diagnostic
	Records     []FindingStreamRecord
}

func runPhaseCPackage(
	t *testing.T,
	pkg string,
	configure func(analyzer *analysis.Analyzer),
) phaseCPackageResult {
	t.Helper()

	h := newAnalyzerHarness()
	resetFlags(t, h)

	path := filepath.Join(t.TempDir(), "findings.jsonl")
	setFlag(t, h.Analyzer, "emit-findings-jsonl", path)
	if configure != nil {
		configure(h.Analyzer)
	}

	diagnostics, _, results := collectDiagnosticsForPackagesRespectCurrentEngine(t, h.Analyzer, pkg)
	for _, result := range results {
		if result != nil && result.Err != nil {
			t.Fatalf("analysis result error: %v", result.Err)
		}
	}
	return phaseCPackageResult{
		Diagnostics: diagnostics,
		Records:     mustReadFindingStreamRecords(t, path),
	}
}

func mustReadFindingStreamRecords(t *testing.T, path string) []FindingStreamRecord {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read findings stream %q: %v", path, err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	records := make([]FindingStreamRecord, 0, len(lines))
	for _, line := range lines {
		var record FindingStreamRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode findings stream record: %v", err)
		}
		records = append(records, record)
	}
	return records
}

func countDiagnosticCategory(diags []analysis.Diagnostic, category string) int {
	count := 0
	for _, diag := range diags {
		if diag.Category == category {
			count++
		}
	}
	return count
}

func findFirstTraceRecord(records []FindingStreamRecord) *FindingStreamRecord {
	for idx := range records {
		if records[idx].Kind == findingStreamKindRefinementTrace {
			return &records[idx]
		}
	}
	return nil
}

func newUnsafeInterprocTestGraph() (interprocSupergraph, interprocNodeID, interprocNodeID) {
	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "pkg.sample", BlockIndex: 0, NodeIndex: 0, Kind: interprocNodeKindCFG}
	terminal := interprocNodeID{FuncKey: "pkg.sample", BlockIndex: 1, NodeIndex: 0, Kind: interprocNodeKindCFG}
	graph.addNode(start, nil)
	graph.addNode(terminal, nil)
	graph.addEdge(interprocEdge{From: start, To: terminal, Kind: interprocEdgeIntra})
	graph.terminalCFGNodes[terminal.Key()] = true
	return graph, start, terminal
}
