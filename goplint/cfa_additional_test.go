// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestBuildFuncCFGForPass(t *testing.T) {
	t.Parallel()

	t.Run("nil body", func(t *testing.T) {
		t.Parallel()

		if got := buildFuncCFGForPass(nil, nil); got != nil {
			t.Fatalf("buildFuncCFGForPass(nil, nil) = %v, want nil", got)
		}
	})

	t.Run("fallback without pass", func(t *testing.T) {
		t.Parallel()

		src := `package p
func f() {
	x := 1
	_ = x
}`
		body, _ := parseFuncBody(t, src)
		cfg := buildFuncCFGForPass(nil, body)
		if cfg == nil || len(cfg.Blocks) == 0 {
			t.Fatalf("fallback CFG = %v, want non-nil with blocks", cfg)
		}
	})

	t.Run("fallback without types info", func(t *testing.T) {
		t.Parallel()

		src := `package p
func f() {
	x := 1
	_ = x
}`
		body, _ := parseFuncBody(t, src)
		cfg := buildFuncCFGForPass(&analysis.Pass{}, body)
		if cfg == nil || len(cfg.Blocks) == 0 {
			t.Fatalf("fallback CFG with nil TypesInfo = %v, want non-nil with blocks", cfg)
		}
	})

	t.Run("typed pass", func(t *testing.T) {
		t.Parallel()

		src := `package p
func f() {
	panic("boom")
}`
		pass, file := buildTypedPassFromSource(t, src)
		fn := findFuncDecl(t, file, "f")
		cfg := buildFuncCFGForPass(pass, fn.Body)
		if cfg == nil || len(cfg.Blocks) == 0 {
			t.Fatalf("typed CFG = %v, want non-nil with blocks", cfg)
		}
	})
}
