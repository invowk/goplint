// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"

	gocfg "golang.org/x/tools/go/cfg"
)

func TestCFGSMTFeasibilityBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		src        string
		choices    []bool
		timeout    time.Duration
		wantResult string
		wantReason string
	}{
		{
			name: "feasible witness is sat",
			src: `package p
func sample(raw string) {
	if raw == "" {
		return
	}
}
`,
			choices:    []bool{true},
			timeout:    50 * time.Millisecond,
			wantResult: cfgFeasibilityResultSAT,
		},
		{
			name: "contradictory guards are unsat",
			src: `package p
func sample(raw string) {
	if raw == "" {
		if raw != "" {
			return
		}
	}
}
`,
			choices:    []bool{true, true},
			timeout:    50 * time.Millisecond,
			wantResult: cfgFeasibilityResultUNSAT,
		},
		{
			name: "unsupported predicate degrades to unknown",
			src: `package p
import "strings"
func sample(raw string) {
	if strings.HasPrefix(raw, "prod") {
		return
	}
}
`,
			choices:    []bool{true},
			timeout:    50 * time.Millisecond,
			wantResult: cfgFeasibilityResultUnknown,
			wantReason: cfgFeasibilityReasonUnsupportedPredicate,
		},
		{
			name: "timeout degrades to unknown",
			src: `package p
func sample(raw string) {
	if raw == "" {
		return
	}
}
`,
			choices:    []bool{true},
			timeout:    0,
			wantResult: cfgFeasibilityResultUnknown,
			wantReason: cfgFeasibilityReasonTimeout,
		},
	}

	backend := cfgSMTFeasibilityBackend{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := mustBuildTestCFG(t, tt.src)
			path := witnessPathFromChoices(t, cfg, tt.choices...)
			gotResult, gotReason := backend.Check(cfgFeasibilityQuery{
				CFG:     cfg,
				Witness: cfgWitnessRecord{CFGPath: path},
				Timeout: tt.timeout,
			})
			if gotResult != tt.wantResult {
				t.Fatalf("Check() result = %q, want %q", gotResult, tt.wantResult)
			}
			if gotReason != tt.wantReason {
				t.Fatalf("Check() reason = %q, want %q", gotReason, tt.wantReason)
			}
		})
	}
}

func TestCFGWitnessHashDeterministic(t *testing.T) {
	t.Parallel()

	record := cfgWitnessRecord{
		OriginAnchors: map[string]string{
			"witness_cast_pos": "sample.go:10:5",
		},
		FactFamily:    "cast-needs-validate",
		FactKey:       "origin|target|type",
		EdgeTag:       string(ideEdgeFuncIdentity),
		CFGPath:       []int32{0, 1, 3},
		CallChain:     []string{"pkg.Func"},
		TriggerReason: cfgRefinementTriggerUnsafeCandidate,
	}
	got1 := computeCFGWitnessHash(record)
	got2 := computeCFGWitnessHash(record)
	if got1 == "" {
		t.Fatal("computeCFGWitnessHash() returned empty hash")
	}
	if got1 != got2 {
		t.Fatalf("computeCFGWitnessHash() = %q, want deterministic %q", got1, got2)
	}
}

func mustBuildTestCFG(t *testing.T, src string) *gocfg.CFG {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, 0)
	if err != nil {
		t.Fatalf("ParseFile() error: %v", err)
	}
	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "sample" {
			body = fn.Body
			break
		}
	}
	if body == nil {
		t.Fatal("sample function body not found")
	}
	return gocfg.New(body, func(*ast.CallExpr) bool { return true })
}

func witnessPathFromChoices(t *testing.T, cfg *gocfg.CFG, choices ...bool) []int32 {
	t.Helper()
	if cfg == nil || len(cfg.Blocks) == 0 {
		t.Fatal("cfg is empty")
	}
	block := cfg.Blocks[0]
	path := []int32{block.Index}
	for _, choice := range choices {
		if block == nil || len(block.Succs) != 2 {
			t.Fatalf("block %d does not have a conditional successor set", block.Index)
		}
		if choice {
			block = block.Succs[0]
		} else {
			block = block.Succs[1]
		}
		path = appendWitnessBlock(path, block.Index)
	}
	return path
}
