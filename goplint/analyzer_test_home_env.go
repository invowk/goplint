// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

func inspectTestHomeEnvPlatform(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	if fn == nil || fn.Body == nil || shouldSkipFunc(fn) {
		return
	}
	if !isTestFile(pass, fn.Pos()) {
		return
	}
	if hasIgnoreDirective(fn.Doc, nil) {
		return
	}

	funcQualName := qualFuncName(pass, fn)
	if isPlatformCategoryExcepted(cfg, funcQualName+".test-home-env-platform", CategoryTestHomeEnvPlatform) {
		return
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isDirectHomeEnvSetter(pass, call) {
			return true
		}
		reportTestHomeEnvPlatform(pass, call, funcQualName, bl)
		return true
	})
}

func isDirectHomeEnvSetter(pass *analysis.Pass, call *ast.CallExpr) bool {
	if len(call.Args) < 2 {
		return false
	}
	if isTestingSetenvHomeCall(pass, call) {
		return true
	}
	if isPackageFuncMatch(pass, call, "os", func(name string) bool { return name == "Setenv" }) {
		return isStringConstant(pass, call.Args[0], "HOME")
	}
	if !isPackageFuncMatch(pass, call, "github.com/invowk/invowk/internal/testutil", func(name string) bool {
		return name == "MustSetenv"
	}) {
		return false
	}
	return len(call.Args) >= 3 && isStringConstant(pass, call.Args[1], "HOME")
}

func isTestingSetenvHomeCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	if !isStringConstant(pass, call.Args[0], "HOME") {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "Setenv" {
		return false
	}
	receiverType := pass.TypesInfo.TypeOf(sel.X)
	return isTestingHomeSetterReceiver(receiverType)
}

func isTestingHomeSetterReceiver(receiverType types.Type) bool {
	if receiverType == nil {
		return false
	}
	t := types.Unalias(receiverType)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "testing" {
		return false
	}
	switch obj.Name() {
	case "T", "B", "F", "TB":
		return true
	default:
		return false
	}
}

func reportTestHomeEnvPlatform(pass *analysis.Pass, call *ast.CallExpr, funcQualName string, bl *BaselineConfig) {
	msg := fmt.Sprintf(
		"test %s sets HOME directly; use internal/testutil.SetHomeDir(t, dir) so os.UserHomeDir sees USERPROFILE on Windows",
		funcQualName,
	)
	findingID := PackageScopedFindingID(pass, CategoryTestHomeEnvPlatform, funcQualName, semanticNodeKey(pass, call.Pos()))
	reportFindingIfNotBaselined(pass, bl, call.Pos(), CategoryTestHomeEnvPlatform, findingID, msg)
}
