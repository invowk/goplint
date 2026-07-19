// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

// buildProtocolCFG constructs the mandatory type-aware CFG. Missing type
// information cannot select an AST-only fallback.
func buildProtocolCFG(pass *analysis.Pass, body *ast.BlockStmt, ssaResult *ssaResult) *gocfg.CFG {
	return buildFuncCFGForPass(pass, body, ssaResult)
}
