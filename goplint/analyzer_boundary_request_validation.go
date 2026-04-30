// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

type boundaryRequestParam struct {
	name     string
	typeName string
	target   castTarget
	pos      token.Pos
}

func inspectBoundaryRequestValidation(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	if fn == nil || fn.Body == nil || fn.Type == nil || !fn.Name.IsExported() || !boundaryRequestFuncReturnsError(fn) {
		return
	}
	if shouldSkipFunc(fn) || hasTrustedBoundaryDirective(fn.Doc, nil) {
		return
	}

	params := collectBoundaryRequestParams(pass, fn)
	for _, param := range params {
		if boundaryRequestParamHasUseBeforeValidation(pass, fn.Body, param.target) {
			reportBoundaryRequestFinding(pass, fn, param, cfg, bl)
		}
	}
}

func collectBoundaryRequestParams(pass *analysis.Pass, fn *ast.FuncDecl) []boundaryRequestParam {
	if pass == nil || pass.TypesInfo == nil || fn == nil || fn.Type == nil || fn.Type.Params == nil {
		return nil
	}
	var params []boundaryRequestParam
	for _, field := range fn.Type.Params.List {
		if field == nil {
			continue
		}
		paramType := pass.TypesInfo.TypeOf(field.Type)
		typeName, ok := boundaryRequestTypeName(paramType)
		if !ok {
			continue
		}
		if !hasValidateMethod(paramType) {
			continue
		}
		for _, name := range field.Names {
			if name == nil || name.Name == "_" {
				continue
			}
			target, ok := castTargetFromExpr(pass, name)
			if !ok {
				continue
			}
			params = append(params, boundaryRequestParam{
				name:     name.Name,
				typeName: typeName,
				target:   target,
				pos:      name.Pos(),
			})
		}
	}
	return params
}

func boundaryRequestTypeName(t types.Type) (string, bool) {
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok {
		return "", false
	}
	if _, ok := named.Underlying().(*types.Struct); !ok {
		return "", false
	}
	name := named.Obj().Name()
	if !strings.HasSuffix(name, "Request") && !strings.HasSuffix(name, "Options") {
		return "", false
	}
	return typeIdentityKey(named), true
}

func boundaryRequestParamHasUseBeforeValidation(pass *analysis.Pass, body *ast.BlockStmt, target castTarget) bool {
	if body == nil {
		return false
	}
	syncLits := collectSynchronousClosureLits(body)
	var syncCalls closureVarCallSet
	methodCalls := collectMethodValueValidateCalls(pass, body)
	for _, stmt := range body.List {
		if boundaryRequestDefaultingStmt(pass, stmt, target) ||
			boundaryRequestNilGuardStmt(pass, stmt, target) ||
			boundaryRequestSafeDelegationStmt(pass, stmt, target) {
			continue
		}
		if boundaryRequestValidateGuard(pass, stmt, target, syncLits, syncCalls, methodCalls) {
			return false
		}
		if boundaryRequestUse(pass, stmt, target, syncLits, syncCalls, methodCalls) {
			return true
		}
	}
	return false
}

func boundaryRequestFuncReturnsError(fn *ast.FuncDecl) bool {
	if fn == nil || fn.Type == nil || fn.Type.Results == nil {
		return false
	}
	for _, field := range fn.Type.Results.List {
		if field == nil {
			continue
		}
		ident, ok := stripParens(field.Type).(*ast.Ident)
		if ok && ident.Name == "error" {
			return true
		}
	}
	return false
}

func boundaryRequestValidateGuard(
	pass *analysis.Pass,
	stmt ast.Stmt,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	ifstmt, ok := stmt.(*ast.IfStmt)
	if !ok || ifstmt.Init == nil || ifstmt.Body == nil {
		return false
	}
	if !nodeSliceContainsValidateCall(
		pass,
		[]ast.Node{ifstmt.Init},
		target,
		syncLits,
		syncCalls,
		methodCalls,
	) {
		return false
	}
	errName, ok := boundaryRequestAssignedErrName(ifstmt.Init)
	if !ok || !boundaryRequestErrCondition(ifstmt.Cond, errName) {
		return false
	}
	return boundaryRequestBlockTerminates(ifstmt.Body)
}

func boundaryRequestAssignedErrName(stmt ast.Stmt) (string, bool) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != len(assign.Rhs) {
		return "", false
	}
	for i, rhs := range assign.Rhs {
		call, ok := stripParens(rhs).(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := stripParens(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != validateMethodName {
			continue
		}
		ident, ok := stripParens(assign.Lhs[i]).(*ast.Ident)
		if !ok || ident.Name == "_" {
			return "", false
		}
		return ident.Name, true
	}
	return "", false
}

func boundaryRequestErrCondition(expr ast.Expr, errName string) bool {
	bin, ok := stripParens(expr).(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return false
	}
	return boundaryRequestIdentName(bin.X, errName) && boundaryRequestIsNil(bin.Y) ||
		boundaryRequestIsNil(bin.X) && boundaryRequestIdentName(bin.Y, errName)
}

func boundaryRequestIdentName(expr ast.Expr, name string) bool {
	ident, ok := stripParens(expr).(*ast.Ident)
	return ok && ident.Name == name
}

func boundaryRequestIsNil(expr ast.Expr) bool {
	ident, ok := stripParens(expr).(*ast.Ident)
	return ok && ident.Name == "nil"
}

func boundaryRequestBlockTerminates(block *ast.BlockStmt) bool {
	if block == nil || len(block.List) == 0 {
		return false
	}
	switch block.List[len(block.List)-1].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	default:
		return false
	}
}

func boundaryRequestNilGuardStmt(pass *analysis.Pass, stmt ast.Stmt, target castTarget) bool {
	ifstmt, ok := stmt.(*ast.IfStmt)
	if !ok || ifstmt.Init != nil || ifstmt.Else != nil || ifstmt.Body == nil {
		return false
	}
	if !boundaryRequestNilCheckSelector(pass, ifstmt.Cond, target) {
		return false
	}
	return boundaryRequestBlockTerminates(ifstmt.Body)
}

func boundaryRequestNilCheckSelector(pass *analysis.Pass, expr ast.Expr, target castTarget) bool {
	bin, ok := stripParens(expr).(*ast.BinaryExpr)
	if !ok || bin.Op != token.EQL {
		return false
	}
	return boundaryRequestSelectorNilSide(pass, bin.X, bin.Y, target) ||
		boundaryRequestSelectorNilSide(pass, bin.Y, bin.X, target)
}

func boundaryRequestSelectorNilSide(pass *analysis.Pass, selectorExpr, nilExpr ast.Expr, target castTarget) bool {
	sel, ok := stripParens(selectorExpr).(*ast.SelectorExpr)
	return ok && target.matchesExpr(pass, sel.X) && boundaryRequestIsNil(nilExpr)
}

func boundaryRequestUse(
	pass *analysis.Pass,
	stmt ast.Stmt,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	if isVarUseTarget(pass, stmt, target, syncLits, syncCalls, methodCalls) {
		return true
	}
	found := false
	parentMap := buildParentMap(stmt)
	ast.Inspect(stmt, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			return syncLits[lit]
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !target.matchesExpr(pass, sel.X) {
			return true
		}
		if boundaryRequestSelectorIsValidateReceiver(sel, parentMap) ||
			boundaryRequestSelectorIsAssignmentLHS(sel, parentMap) {
			return true
		}
		found = true
		return false
	})
	return found
}

func boundaryRequestSafeDelegationStmt(pass *analysis.Pass, stmt ast.Stmt, target castTarget) bool {
	switch node := stmt.(type) {
	case *ast.ReturnStmt:
		return boundaryRequestSafeDelegationExprs(pass, node.Results, target)
	case *ast.AssignStmt:
		return boundaryRequestSafeDelegationExprs(pass, node.Rhs, target)
	case *ast.ExprStmt:
		return boundaryRequestSafeDelegationExpr(pass, node.X, target)
	default:
		return false
	}
}

func boundaryRequestSafeDelegationExprs(pass *analysis.Pass, exprs []ast.Expr, target castTarget) bool {
	if len(exprs) == 0 {
		return false
	}
	foundDelegation := false
	for _, expr := range exprs {
		if boundaryRequestSafeDelegationExpr(pass, expr, target) {
			foundDelegation = true
			continue
		}
		if boundaryRequestUse(pass, &ast.ExprStmt{X: expr}, target, nil, nil, nil) {
			return false
		}
	}
	return foundDelegation
}

func boundaryRequestSafeDelegationExpr(pass *analysis.Pass, expr ast.Expr, target castTarget) bool {
	call, ok := stripParens(expr).(*ast.CallExpr)
	if !ok || !boundaryRequestExportedCallee(call) {
		return false
	}
	foundTargetArg := false
	for _, arg := range call.Args {
		if target.matchesExpr(pass, arg) {
			foundTargetArg = true
			continue
		}
		if boundaryRequestUse(pass, &ast.ExprStmt{X: arg}, target, nil, nil, nil) {
			return false
		}
	}
	return foundTargetArg
}

func boundaryRequestExportedCallee(call *ast.CallExpr) bool {
	switch fun := stripParens(call.Fun).(type) {
	case *ast.Ident:
		return fun.IsExported()
	case *ast.SelectorExpr:
		return fun.Sel != nil && fun.Sel.IsExported()
	default:
		return false
	}
}

func boundaryRequestSelectorIsValidateReceiver(sel *ast.SelectorExpr, parentMap map[ast.Node]ast.Node) bool {
	if sel == nil || sel.Sel.Name != validateMethodName {
		return false
	}
	_, ok := parentMap[sel].(*ast.CallExpr)
	return ok
}

func boundaryRequestSelectorIsAssignmentLHS(sel *ast.SelectorExpr, parentMap map[ast.Node]ast.Node) bool {
	parent, ok := parentMap[sel]
	if !ok {
		return false
	}
	assign, ok := parent.(*ast.AssignStmt)
	if !ok {
		return false
	}
	for _, lhs := range assign.Lhs {
		if lhs == sel {
			return true
		}
	}
	return false
}

func boundaryRequestDefaultingStmt(pass *analysis.Pass, stmt ast.Stmt, target castTarget) bool {
	ifstmt, ok := stmt.(*ast.IfStmt)
	if !ok || ifstmt.Init != nil || ifstmt.Else != nil || ifstmt.Body == nil {
		return false
	}
	selector, ok := boundaryRequestZeroCheckSelector(pass, ifstmt.Cond, target)
	if !ok {
		return false
	}
	for _, bodyStmt := range ifstmt.Body.List {
		assign, ok := bodyStmt.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return false
		}
		lhs, ok := stripParens(assign.Lhs[0]).(*ast.SelectorExpr)
		if !ok || targetKeyForExpr(pass, lhs) != selector {
			return false
		}
		if boundaryRequestUse(pass, &ast.ExprStmt{X: assign.Rhs[0]}, target, nil, nil, nil) {
			return false
		}
	}
	return true
}

func boundaryRequestZeroCheckSelector(pass *analysis.Pass, expr ast.Expr, target castTarget) (string, bool) {
	bin, ok := stripParens(expr).(*ast.BinaryExpr)
	if !ok || bin.Op != token.EQL {
		return "", false
	}
	if key, ok := boundaryRequestSelectorZeroSide(pass, bin.X, bin.Y, target); ok {
		return key, true
	}
	return boundaryRequestSelectorZeroSide(pass, bin.Y, bin.X, target)
}

func boundaryRequestSelectorZeroSide(pass *analysis.Pass, selectorExpr, zeroExpr ast.Expr, target castTarget) (string, bool) {
	sel, ok := stripParens(selectorExpr).(*ast.SelectorExpr)
	if !ok || !target.matchesExpr(pass, sel.X) || !boundaryRequestZeroLiteral(zeroExpr) {
		return "", false
	}
	key := targetKeyForExpr(pass, sel)
	return key, key != ""
}

func boundaryRequestZeroLiteral(expr ast.Expr) bool {
	switch e := stripParens(expr).(type) {
	case *ast.BasicLit:
		return e.Value == `""` || e.Value == "0"
	case *ast.Ident:
		return e.Name == "nil" || e.Name == "false"
	default:
		return false
	}
}

func reportBoundaryRequestFinding(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	param boundaryRequestParam,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	qualName := qualFuncName(pass, fn)
	exceptionKey := qualName + "." + param.name + ".boundary-request-validation"
	funcExceptionKey := qualName + ".boundary-request-validation"
	if cfg != nil && (cfg.isExcepted(exceptionKey) || cfg.isExcepted(funcExceptionKey)) {
		return
	}
	findingID := PackageScopedFindingID(
		pass,
		CategoryUnvalidatedBoundaryRequest,
		qualName,
		param.name,
		param.typeName,
		stablePosKey(pass, param.pos),
	)
	msg := fmt.Sprintf(
		"parameter %q of %s is used before checked Validate() at exported boundary",
		param.name,
		param.typeName,
	)
	reportFindingIfNotBaselined(pass, bl, param.pos, CategoryUnvalidatedBoundaryRequest, findingID, msg)
}
