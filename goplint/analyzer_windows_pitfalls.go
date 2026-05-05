// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var commandExecutionMethods = map[string]bool{
	"CombinedOutput": true,
	"Output":         true,
	"Run":            true,
	"Start":          true,
	"Wait":           true,
}

func inspectCommandWaitDelay(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	if fn == nil || fn.Body == nil || shouldSkipFunc(fn) {
		return
	}
	funcQualName := qualFuncName(pass, fn)
	if isPlatformCategoryExcepted(cfg, funcQualName+".command-waitdelay", "command-waitdelay") {
		return
	}

	tracked := make(map[*types.Var]token.Pos)
	hasWaitDelay := make(map[*types.Var]bool)
	reported := make(map[*types.Var]bool)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for idx, lhs := range node.Lhs {
				if recordWaitDelayAssignment(pass, lhs, tracked, hasWaitDelay) {
					continue
				}
				if idx >= len(node.Rhs) {
					continue
				}
				v := varFromIdentExpr(pass, lhs)
				if v == nil {
					continue
				}
				if isExecCommandContextExpr(pass, node.Rhs[idx]) {
					tracked[v] = node.Rhs[idx].Pos()
					hasWaitDelay[v] = false
					reported[v] = false
					continue
				}
				delete(tracked, v)
				delete(hasWaitDelay, v)
				delete(reported, v)
			}
		case *ast.CallExpr:
			if commandContextImmediateExecution(pass, node) {
				reportCommandWaitDelay(pass, node, funcQualName, bl)
				return true
			}
			v, method := commandMethodCall(pass, node)
			if v == nil || !commandExecutionMethods[method] {
				return true
			}
			if _, ok := tracked[v]; ok && !hasWaitDelay[v] && !reported[v] {
				reportCommandWaitDelay(pass, node, funcQualName, bl)
				reported[v] = true
			}
		case *ast.ReturnStmt:
			for _, result := range node.Results {
				for _, v := range commandVarsReturnedInPreparedCommand(pass, result) {
					if _, ok := tracked[v]; ok && !hasWaitDelay[v] && !reported[v] {
						reportCommandWaitDelay(pass, result, funcQualName, bl)
						reported[v] = true
					}
				}
			}
		}
		return true
	})
}

func inspectCueFedPathNativeClean(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	if fn == nil || fn.Body == nil || shouldSkipFunc(fn) {
		return
	}
	funcQualName := qualFuncName(pass, fn)
	if isPlatformCategoryExcepted(cfg, funcQualName+".cue-fed-path-native-clean", CategoryCueFedPathNativeClean) {
		return
	}
	if !isExportedPathValidatorName(fn.Name.Name) {
		return
	}

	pathVars := repoPathParameterVars(pass, fn)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for idx, lhs := range node.Lhs {
				if idx >= len(node.Rhs) {
					continue
				}
				v := varFromIdentExpr(pass, lhs)
				if v == nil {
					continue
				}
				if exprHasRepoPathProvenance(pass, node.Rhs[idx], pathVars) {
					pathVars[v] = true
					continue
				}
				delete(pathVars, v)
			}
		case *ast.CallExpr:
			if !isNativeFilepathClassifierCall(pass, node) {
				return true
			}
			for _, arg := range node.Args {
				if exprHasRepoPathProvenance(pass, arg, pathVars) {
					reportCueFedPathNativeClean(pass, node, funcQualName, packageCallFuncName(node), bl)
					return true
				}
			}
		}
		return true
	})
}

func inspectPathBoundaryPrefix(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	if fn == nil || fn.Body == nil || shouldSkipFunc(fn) {
		return
	}
	funcQualName := qualFuncName(pass, fn)
	if isPlatformCategoryExcepted(cfg, funcQualName+".path-boundary-prefix", CategoryPathBoundaryPrefix) {
		return
	}

	relVars := make(map[*types.Var]bool)
	cleanVars := make(map[*types.Var]bool)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for idx, lhs := range node.Lhs {
				if idx >= len(node.Rhs) {
					continue
				}
				v := varFromIdentExpr(pass, lhs)
				if v == nil {
					continue
				}
				if call, ok := stripParens(node.Rhs[idx]).(*ast.CallExpr); ok {
					switch {
					case isCallToPathFilepathFunc(pass, call, "Rel"):
						relVars[v] = true
					case isCallToPathFilepathFunc(pass, call, "Clean") || isCallToPathFilepathFunc(pass, call, "Abs") || isCallToPathFilepathFunc(pass, call, "EvalSymlinks"):
						cleanVars[v] = true
					default:
						delete(relVars, v)
						delete(cleanVars, v)
					}
					continue
				}
				delete(relVars, v)
				delete(cleanVars, v)
			}
		case *ast.CallExpr:
			if !isStringsHasPrefixCall(pass, node) || len(node.Args) != 2 {
				return true
			}
			firstVar := varFromIdentExpr(pass, node.Args[0])
			if firstVar == nil {
				return true
			}
			if relVars[firstVar] && isStringConstant(pass, node.Args[1], "..") {
				reportPathBoundaryPrefix(pass, node, funcQualName, "filepath.Rel result is checked with strings.HasPrefix(rel, \"..\"); require rel == \"..\" or \"..\"+separator boundary", bl)
				return true
			}
			if cleanVars[firstVar] {
				if secondVar := varFromIdentExpr(pass, node.Args[1]); secondVar != nil && cleanVars[secondVar] {
					reportPathBoundaryPrefix(pass, node, funcQualName, "cleaned path containment uses strings.HasPrefix(candidate, base) without exact match or separator boundary", bl)
				}
			}
		}
		return true
	})
}

func inspectVolumeMountHostToSlash(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	if fn == nil || fn.Body == nil || shouldSkipFunc(fn) {
		return
	}
	funcQualName := qualFuncName(pass, fn)
	if isPlatformCategoryExcepted(cfg, funcQualName+".volume-mount-host-toslash", CategoryVolumeMountHostToSlash) {
		return
	}

	parentMap := make(map[ast.Node]ast.Node)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		for _, child := range directChildren(n) {
			parentMap[child] = n
		}
		return true
	})

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if isVolumeMountSpecConversion(pass, node) && len(node.Args) == 1 {
				if volumeSpecExprNeedsToSlash(pass, node.Args[0]) {
					reportVolumeMountHostToSlash(pass, node, funcQualName, bl)
					return false
				}
				return true
			}
			if isVolumeMountFormatterFunc(fn) && len(node.Args) == 1 && isWriteStringHostPathCall(pass, node) {
				reportVolumeMountHostToSlash(pass, node, funcQualName, bl)
				return true
			}
		case *ast.BinaryExpr:
			if parent, ok := parentMap[node]; ok {
				if parentBin, ok := parent.(*ast.BinaryExpr); ok && parentBin.Op == token.ADD {
					return true
				}
			}
			if volumeSpecExprNeedsToSlash(pass, node) {
				reportVolumeMountHostToSlash(pass, node, funcQualName, bl)
				return true
			}
		}
		return true
	})
}

func inspectCobraCommandContext(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	if fn == nil || fn.Body == nil || shouldSkipFunc(fn) {
		return
	}
	funcQualName := qualFuncName(pass, fn)
	if isPlatformCategoryExcepted(cfg, funcQualName+".cobra-command-context", CategoryCobraCommandContext) {
		return
	}
	cobraVars := cobraCommandParameterVars(pass, fn)
	if len(cobraVars) == 0 {
		return
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isContextBackgroundCall(pass, call) {
			return true
		}
		reportCobraCommandContext(pass, call, funcQualName, bl)
		return true
	})
}

func directChildren(n ast.Node) []ast.Node {
	var children []ast.Node
	ast.Inspect(n, func(child ast.Node) bool {
		if child == nil || child == n {
			return true
		}
		children = append(children, child)
		return false
	})
	return children
}

func reportCommandWaitDelay(pass *analysis.Pass, node ast.Node, funcQualName string, bl *BaselineConfig) {
	msg := fmt.Sprintf("exec.CommandContext command in %s is used without setting Cmd.WaitDelay before execution; set WaitDelay to bound pipe waits after Windows process cancellation", funcQualName)
	findingID := PackageScopedFindingID(pass, CategoryMissingCommandWaitDelay, funcQualName, stablePosKey(pass, node.Pos()))
	reportFindingIfNotBaselined(pass, bl, node.Pos(), CategoryMissingCommandWaitDelay, findingID, msg)
}

func reportCueFedPathNativeClean(pass *analysis.Pass, node ast.Node, funcQualName, filepathFunc string, bl *BaselineConfig) {
	msg := fmt.Sprintf("CUE-fed or repo-relative path in %s flows into filepath.%s before slash-normalized validation; normalize backslashes and use path.Clean for repo-relative checks", funcQualName, filepathFunc)
	findingID := PackageScopedFindingID(pass, CategoryCueFedPathNativeClean, funcQualName, stablePosKey(pass, node.Pos()))
	reportFindingIfNotBaselined(pass, bl, node.Pos(), CategoryCueFedPathNativeClean, findingID, msg)
}

func reportPathBoundaryPrefix(pass *analysis.Pass, node ast.Node, funcQualName, reason string, bl *BaselineConfig) {
	msg := fmt.Sprintf("unsafe path boundary check in %s: %s", funcQualName, reason)
	findingID := PackageScopedFindingID(pass, CategoryPathBoundaryPrefix, funcQualName, stablePosKey(pass, node.Pos()))
	reportFindingIfNotBaselined(pass, bl, node.Pos(), CategoryPathBoundaryPrefix, findingID, msg)
}

func reportVolumeMountHostToSlash(pass *analysis.Pass, node ast.Node, funcQualName string, bl *BaselineConfig) {
	msg := fmt.Sprintf("container volume mount host path in %s is formatted before filepath.ToSlash; convert the host path before appending ':' to avoid invalid Windows specs", funcQualName)
	findingID := PackageScopedFindingID(pass, CategoryVolumeMountHostToSlash, funcQualName, stablePosKey(pass, node.Pos()))
	reportFindingIfNotBaselined(pass, bl, node.Pos(), CategoryVolumeMountHostToSlash, findingID, msg)
}

func reportCobraCommandContext(pass *analysis.Pass, node ast.Node, funcQualName string, bl *BaselineConfig) {
	msg := fmt.Sprintf("Cobra command handler %s calls context.Background(); use cmd.Context() so signal cancellation and caller deadlines reach execution", funcQualName)
	findingID := PackageScopedFindingID(pass, CategoryCobraCommandContext, funcQualName, stablePosKey(pass, node.Pos()))
	reportFindingIfNotBaselined(pass, bl, node.Pos(), CategoryCobraCommandContext, findingID, msg)
}

func isPlatformCategoryExcepted(cfg *ExceptionConfig, key, category string) bool {
	return cfg != nil && cfg.isCategoryExcepted(key, category)
}

func recordWaitDelayAssignment(
	pass *analysis.Pass,
	lhs ast.Expr,
	tracked map[*types.Var]token.Pos,
	hasWaitDelay map[*types.Var]bool,
) bool {
	sel, ok := stripParens(lhs).(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "WaitDelay" {
		return false
	}
	v := varFromIdentExpr(pass, sel.X)
	if v == nil {
		return false
	}
	if _, ok := tracked[v]; ok {
		hasWaitDelay[v] = true
	}
	return true
}

func isExecCommandContextExpr(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := stripParens(expr).(*ast.CallExpr)
	return ok && isPackageFuncMatch(pass, call, "os/exec", func(name string) bool {
		return name == "CommandContext"
	})
}

func commandMethodCall(pass *analysis.Pass, call *ast.CallExpr) (*types.Var, string) {
	sel, ok := stripParens(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return nil, ""
	}
	return varFromIdentExpr(pass, sel.X), sel.Sel.Name
}

func commandContextImmediateExecution(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := stripParens(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || !commandExecutionMethods[sel.Sel.Name] {
		return false
	}
	return isExecCommandContextExpr(pass, sel.X)
}

func commandVarsReturnedInPreparedCommand(pass *analysis.Pass, expr ast.Expr) []*types.Var {
	lit, ok := stripParens(expr).(*ast.UnaryExpr)
	if ok && lit.Op == token.AND {
		expr = lit.X
	}
	composite, ok := stripParens(expr).(*ast.CompositeLit)
	if !ok {
		return nil
	}
	typeName := typeNameString(pass.TypesInfo.TypeOf(composite))
	if !strings.HasSuffix(typeName, ".PreparedCommand") && typeName != "PreparedCommand" {
		return nil
	}
	var out []*types.Var
	for _, elt := range composite.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := stripParens(kv.Key).(*ast.Ident)
		if !ok || key.Name != "Cmd" {
			continue
		}
		if v := varFromIdentExpr(pass, kv.Value); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func repoPathParameterVars(pass *analysis.Pass, fn *ast.FuncDecl) map[*types.Var]bool {
	out := make(map[*types.Var]bool)
	if fn.Type == nil || fn.Type.Params == nil {
		return out
	}
	pathValidator := isExportedPathValidatorName(fn.Name.Name)
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			obj := pass.TypesInfo.ObjectOf(name)
			v, ok := obj.(*types.Var)
			if !ok {
				continue
			}
			if isStrictRepoRelativePathType(pass, v.Type()) || (pathValidator && isRepoPathParamName(name.Name)) {
				out[v] = true
			}
		}
	}
	return out
}

func isExportedPathValidatorName(name string) bool {
	return strings.HasPrefix(name, "Validate") && strings.Contains(name, "Path")
}

func isRepoPathParamName(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "base") || strings.Contains(lower, "root") || strings.Contains(lower, "dir") {
		return false
	}
	return strings.Contains(lower, "path") || strings.Contains(lower, "file") || strings.Contains(lower, "container")
}

func exprHasRepoPathProvenance(pass *analysis.Pass, expr ast.Expr, pathVars map[*types.Var]bool) bool {
	expr = stripParens(expr)
	if isStrictRepoRelativePathType(pass, pass.TypesInfo.TypeOf(expr)) {
		return true
	}
	if v := varFromIdentExpr(pass, expr); v != nil {
		return pathVars[v]
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		if len(call.Args) == 1 {
			if tv, ok := pass.TypesInfo.Types[call.Fun]; ok && tv.IsType() {
				if basic, ok := types.Unalias(tv.Type).(*types.Basic); ok && basic.Kind() == types.String {
					return exprHasRepoPathProvenance(pass, call.Args[0], pathVars)
				}
			}
		}
		if isRepoPathPreservingCall(pass, call) {
			for _, arg := range call.Args {
				if exprHasRepoPathProvenance(pass, arg, pathVars) {
					return true
				}
			}
		}
	}
	return false
}

func isStrictRepoRelativePathType(pass *analysis.Pass, t types.Type) bool {
	typeName := cueFedTypeNameForType(pass, t)
	if typeName == nil {
		return false
	}
	switch typeName.Name() {
	case "ContainerfilePath", "DotenvFilePath", "ScriptContent", "SubdirectoryPath", "WorkDir":
		return true
	default:
		return false
	}
}

func isRepoPathPreservingCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	if isCallToPathFilepathFunc(pass, call, "FromSlash") ||
		isCallToPathFilepathFunc(pass, call, "Clean") ||
		isCallToPathFilepathFunc(pass, call, "Join") ||
		isCallToPathFilepathFunc(pass, call, "Rel") ||
		isCallToPathFilepathFunc(pass, call, "Base") {
		return true
	}
	if isPackageFuncMatch(pass, call, "strings", func(name string) bool {
		switch name {
		case "TrimSpace", "TrimSuffix", "TrimPrefix", "Trim", "ReplaceAll":
			return true
		default:
			return false
		}
	}) {
		return true
	}
	return false
}

func isNativeFilepathClassifierCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isCallToPathFilepathFunc(pass, call, "Clean") ||
		isCallToPathFilepathFunc(pass, call, "Join") ||
		isCallToPathFilepathFunc(pass, call, "Rel") ||
		isCallToPathFilepathFunc(pass, call, "Base")
}

func isStringsHasPrefixCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isPackageFuncMatch(pass, call, "strings", func(name string) bool {
		return name == "HasPrefix"
	})
}

func isStringConstant(pass *analysis.Pass, expr ast.Expr, want string) bool {
	if pass == nil || pass.TypesInfo == nil || expr == nil {
		return false
	}
	tv, ok := pass.TypesInfo.Types[expr]
	return ok && tv.Value != nil && tv.Value.Kind() == constant.String && constant.StringVal(tv.Value) == want
}

func isVolumeMountSpecConversion(pass *analysis.Pass, call *ast.CallExpr) bool {
	tv, ok := pass.TypesInfo.Types[call.Fun]
	if !ok || !tv.IsType() {
		return false
	}
	return strings.HasSuffix(typeNameString(tv.Type), ".VolumeMountSpec") ||
		strings.HasSuffix(typeNameString(tv.Type), ".ContainerVolumeMountSpec")
}

func volumeSpecExprNeedsToSlash(pass *analysis.Pass, expr ast.Expr) bool {
	hostExpr, ok := hostPathOperandBeforeVolumeColon(pass, expr)
	return ok && !isFilepathToSlashCall(pass, hostExpr)
}

func hostPathOperandBeforeVolumeColon(pass *analysis.Pass, expr ast.Expr) (ast.Expr, bool) {
	parts := flattenStringConcat(expr)
	for idx, part := range parts {
		if !exprContainsVolumeColon(pass, part) {
			continue
		}
		for prev := idx - 1; prev >= 0; prev-- {
			if isLikelyHostPathExpr(pass, parts[prev]) {
				return parts[prev], true
			}
		}
	}
	return nil, false
}

func flattenStringConcat(expr ast.Expr) []ast.Expr {
	bin, ok := stripParens(expr).(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return []ast.Expr{expr}
	}
	out := flattenStringConcat(bin.X)
	out = append(out, flattenStringConcat(bin.Y)...)
	return out
}

func exprContainsVolumeColon(pass *analysis.Pass, expr ast.Expr) bool {
	if isStringConstant(pass, expr, ":") {
		return true
	}
	tv, ok := pass.TypesInfo.Types[expr]
	return ok && tv.Value != nil && tv.Value.Kind() == constant.String && strings.Contains(constant.StringVal(tv.Value), ":/")
}

func isLikelyHostPathExpr(pass *analysis.Pass, expr ast.Expr) bool {
	if isFilepathToSlashCall(pass, expr) {
		return true
	}
	expr = unwrapStringConversion(pass, expr)
	if cueFedTypeNameForType(pass, pass.TypesInfo.TypeOf(expr)) != nil {
		return true
	}
	if strings.Contains(typeNameString(pass.TypesInfo.TypeOf(expr)), "HostFilesystemPath") ||
		strings.Contains(typeNameString(pass.TypesInfo.TypeOf(expr)), "FilesystemPath") {
		return true
	}
	if sel, ok := stripParens(expr).(*ast.SelectorExpr); ok && sel.Sel != nil {
		return strings.Contains(sel.Sel.Name, "HostPath") || strings.Contains(sel.Sel.Name, "FilesystemPath")
	}
	return false
}

func isFilepathToSlashCall(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := stripParens(expr).(*ast.CallExpr)
	return ok && isCallToPathFilepathFunc(pass, call, "ToSlash")
}

func isVolumeMountFormatterFunc(fn *ast.FuncDecl) bool {
	if fn.Name.Name == "FormatVolumeMount" {
		return true
	}
	if fn.Name.Name != "String" || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	return strings.Contains(typeNameStringFromExpr(fn.Recv.List[0].Type), "VolumeMount")
}

func isWriteStringHostPathCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := stripParens(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "WriteString" || len(call.Args) != 1 {
		return false
	}
	return isLikelyHostPathExpr(pass, call.Args[0]) && !isFilepathToSlashCall(pass, call.Args[0])
}

func cobraCommandParameterVars(pass *analysis.Pass, fn *ast.FuncDecl) map[*types.Var]bool {
	out := make(map[*types.Var]bool)
	if pass == nil || pass.TypesInfo == nil || fn == nil || fn.Type == nil || fn.Type.Params == nil {
		return out
	}
	for _, field := range fn.Type.Params.List {
		if !isCobraCommandType(field.Type, pass.TypesInfo) {
			continue
		}
		for _, name := range field.Names {
			if v, ok := pass.TypesInfo.ObjectOf(name).(*types.Var); ok {
				out[v] = true
			}
		}
	}
	return out
}

func isCobraCommandType(expr ast.Expr, info *types.Info) bool {
	if expr == nil || info == nil {
		return false
	}
	return isNamedCobraCommand(info.TypeOf(expr))
}

func isNamedCobraCommand(t types.Type) bool {
	ptr, ok := types.Unalias(t).(*types.Pointer)
	if ok {
		t = ptr.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == "Command" && named.Obj().Pkg().Path() == "github.com/spf13/cobra"
}

func isContextBackgroundCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isPackageFuncMatch(pass, call, "context", func(name string) bool {
		return name == "Background"
	})
}

func unwrapStringConversion(pass *analysis.Pass, expr ast.Expr) ast.Expr {
	for {
		call, ok := stripParens(expr).(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return expr
		}
		tv, ok := pass.TypesInfo.Types[call.Fun]
		if !ok || !tv.IsType() {
			return expr
		}
		if basic, ok := types.Unalias(tv.Type).(*types.Basic); !ok || basic.Kind() != types.String {
			return expr
		}
		expr = call.Args[0]
	}
}

func varFromIdentExpr(pass *analysis.Pass, expr ast.Expr) *types.Var {
	ident, ok := stripParens(expr).(*ast.Ident)
	if !ok {
		return nil
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	v, ok := obj.(*types.Var)
	if !ok {
		return nil
	}
	return v
}

func typeNameString(t types.Type) string {
	if t == nil {
		return ""
	}
	return types.TypeString(types.Unalias(t), func(pkg *types.Package) string {
		if pkg == nil {
			return ""
		}
		return pkg.Name()
	})
}

func typeNameStringFromExpr(expr ast.Expr) string {
	switch e := stripParens(expr).(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return typeNameStringFromExpr(e.X)
	case *ast.SelectorExpr:
		return e.Sel.Name
	default:
		return ""
	}
}
