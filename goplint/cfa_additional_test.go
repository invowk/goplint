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

		if got := buildFuncCFGForPass(nil, nil, nil); got != nil {
			t.Fatalf("buildFuncCFGForPass(nil, nil) = %v, want nil", got)
		}
	})

	t.Run("rejects missing pass", func(t *testing.T) {
		t.Parallel()

		src := `package p
func f() {
	x := 1
	_ = x
}`
		body, _ := parseFuncBody(t, src)
		cfg := buildFuncCFGForPass(nil, body, nil)
		if cfg != nil {
			t.Fatalf("buildFuncCFGForPass(nil, body) = %v, want nil", cfg)
		}
	})

	t.Run("rejects missing types info", func(t *testing.T) {
		t.Parallel()

		src := `package p
func f() {
	x := 1
	_ = x
}`
		body, _ := parseFuncBody(t, src)
		cfg := buildFuncCFGForPass(&analysis.Pass{}, body, nil)
		if cfg != nil {
			t.Fatalf("buildFuncCFGForPass(pass without TypesInfo, body) = %v, want nil", cfg)
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
		cfg := buildFuncCFGForPass(pass, fn.Body, buildSSAForPass(pass))
		if cfg == nil || len(cfg.Blocks) == 0 {
			t.Fatalf("typed CFG = %v, want non-nil with blocks", cfg)
		}
	})
}
