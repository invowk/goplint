// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

// buildFuncCFGForBackend selects the requested path-analysis backend.
// - "ssa": type-aware mayReturn analysis (no-return calls prune CFG paths).
// - "ast": conservative AST-only CFG where all calls are treated as may-return.
func buildFuncCFGForBackend(pass *analysis.Pass, body *ast.BlockStmt, backend string) *gocfg.CFG {
	switch backend {
	case cfgBackendSSA:
		return buildFuncCFGForPass(pass, body)
	case cfgBackendAST:
		return buildFuncCFG(body)
	default:
		return buildFuncCFGForPass(pass, body)
	}
}
