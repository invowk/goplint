// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
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
	cfgMaxStates int,
	refinement cfgProtocolRefinementOptions,
	ssaRes *ssaResult,
	calleeSummaryCache *sync.Map,
) {
	if fn == nil || fn.Body == nil || fn.Type == nil || !fn.Name.IsExported() || !boundaryRequestFuncReturnsError(fn) {
		return
	}
	if shouldSkipFunc(fn) || hasTrustedBoundaryDirective(fn.Doc, nil) {
		return
	}
	params := collectBoundaryRequestParams(pass, fn)
	if len(params) == 0 {
		// Callee summaries are relevant only when this declaration actually
		// owns a boundary-request obligation. Building them for every exported
		// error-returning function in every dependency recursively summarizes
		// unrelated standard-library call graphs.
		return
	}

	funcCFG := buildProtocolCFG(pass, fn.Body, ssaRes)
	if funcCFG == nil || len(funcCFG.Blocks) == 0 {
		return
	}
	functionAvailability := protocolSSAAvailabilityForDecl(pass, ssaRes, fn)
	effectiveBudget := adaptiveBlockVisitBudget(funcCFG, blockVisitBudget{maxStates: cfgMaxStates})
	solver := newInterprocSolverWithSSA(pass, ssaRes, calleeSummaryCache)
	refiner := newCFGRefinementController(refinement)
	syncLits := collectSynchronousClosureLits(fn.Body)
	methodCalls := mergeMethodValueValidateCallSets(
		collectMethodValueValidateCalls(pass, fn.Body),
		collectCalleeValidatedCalls(
			pass,
			fn.Body,
			ssaRes,
			stackScopeFromMap(nil, ssaRes),
			calleeSummaryCache,
		),
	)
	callChain := []string{qualFuncName(pass, fn)}
	for _, param := range params {
		availability := functionAvailability
		paramAvailability := enrichBoundaryRequestParamWithSSA(pass, ssaRes, fn, &param)
		if availability.ready() && !paramAvailability.ready() {
			availability = paramAvailability
		}
		originKey := semanticNodeKey(pass, param.pos)
		input := interprocUBVCrossBlockInput{
			Target:                         param.target,
			DefBlock:                       funcCFG.Blocks[0],
			DefIdx:                         -1,
			OriginKey:                      originKey,
			TypeName:                       param.typeName,
			SyncLits:                       syncLits,
			MethodCalls:                    methodCalls,
			MaxStates:                      effectiveBudget.maxStates,
			CallChain:                      callChain,
			SSAAvailability:                availability,
			OriginAtEntry:                  true,
			IgnoredNodes:                   boundaryRequestIgnoredProtocolNodes(pass, fn.Body, param.target),
			TerminalUncertaintyIsBlocking:  true,
			ValidationDischargesObligation: true,
		}
		controlledSolver := solver.withControl(refiner.newDeadline())
		result := controlledSolver.EvaluateUBVCrossBlock(input)
		result = refineBoundaryRequestResult(pass, fn, param, funcCFG, refiner, controlledSolver, input, result)
		switch result.toPathOutcome() {
		case pathOutcomeUnsafe:
			reportBoundaryRequestFinding(pass, fn, param, cfg, bl, result)
		case pathOutcomeInconclusive:
			reportBoundaryRequestInconclusive(pass, fn, param, bl, result)
		case pathOutcomeSafe:
		}
	}
}

func reportBoundaryRequestInconclusive(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	param boundaryRequestParam,
	bl *BaselineConfig,
	result interprocPathResult,
) {
	qualName := qualFuncName(pass, fn)
	findingID := PackageScopedFindingID(
		pass,
		CategoryUnvalidatedBoundaryRequest,
		qualName,
		param.name,
		param.typeName,
		"inconclusive",
		string(result.Reason),
		semanticNodeKey(pass, param.pos),
	)
	msg := fmt.Sprintf(
		"parameter %q of %s has inconclusive checked Validate() analysis at exported boundary",
		param.name,
		param.typeName,
	)
	meta := map[string]string{
		"cfg_outcome_status":      cfgRefinementStatusInconclusive,
		"cfg_inconclusive_reason": string(result.Reason),
	}
	meta = appendProtocolRefinementMeta(meta, result)
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass,
		bl,
		param.pos,
		CategoryUnvalidatedBoundaryRequest,
		findingID,
		msg,
		meta,
	)
}

func enrichBoundaryRequestParamWithSSA(
	pass *analysis.Pass,
	ssaRes *ssaResult,
	fn *ast.FuncDecl,
	param *boundaryRequestParam,
) ssaAvailability {
	if pass == nil || pass.TypesInfo == nil || fn == nil || fn.Name == nil || param == nil {
		return ssaAvailability{Status: ssaAvailabilityMissingFunction}
	}
	typesFunc, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
	if !ok || typesFunc == nil {
		return ssaAvailability{Status: ssaAvailabilityMissingFunction}
	}
	resolution := resolveSSAFunction(ssaRes, typesFunc)
	if !resolution.Availability.ready() {
		return resolution.Availability
	}
	signature, ok := typesFunc.Type().(*types.Signature)
	if !ok {
		return ssaAvailability{Status: ssaAvailabilityMissingFunction}
	}
	parameterIdent, ok := param.target.originExpr.(*ast.Ident)
	if !ok {
		return ssaAvailability{Status: ssaAvailabilityMissingFunction}
	}
	parameterObject := objectForIdent(pass, parameterIdent)
	ssaIndex := 0
	if signature.Recv() != nil {
		ssaIndex++
	}
	parameterIndex := -1
	for index := range signature.Params().Len() {
		if signature.Params().At(index) == parameterObject {
			parameterIndex = index
			break
		}
	}
	if parameterIndex < 0 || ssaIndex+parameterIndex >= len(resolution.Function.Params) {
		return ssaAvailability{Status: ssaAvailabilityMissingFunction}
	}
	ssaParameter := resolution.Function.Params[ssaIndex+parameterIndex]
	interner := newProtocolIdentityInterner()
	origin := interner.internValue(ssaParameter)
	analysis := analyzeProtocolAliases(resolution.Function, interner)
	param.target.flowAliases = newSSAFlowAliasMatcherForIdentity(
		pass,
		resolution.Function,
		analysis,
		origin,
		ssaValueHasPointerType(ssaParameter),
		ssaParameter,
	)
	return resolution.Availability
}

func boundaryRequestIgnoredProtocolNodes(pass *analysis.Pass, body *ast.BlockStmt, target castTarget) map[ast.Node]bool {
	ignored := make(map[ast.Node]bool)
	if body == nil {
		return ignored
	}
	for _, stmt := range body.List {
		if !boundaryRequestDefaultingStmt(pass, stmt, target) &&
			!boundaryRequestNilGuardStmt(pass, stmt, target) &&
			!boundaryRequestLocalAliasStmt(pass, stmt, target) {
			continue
		}
		ast.Inspect(stmt, func(node ast.Node) bool {
			if node != nil {
				ignored[node] = true
			}
			return true
		})
	}
	return ignored
}

func boundaryRequestLocalAliasStmt(pass *analysis.Pass, stmt ast.Stmt, target castTarget) bool {
	if pass == nil || pass.Pkg == nil {
		return false
	}
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return false
	}
	identifier, ok := stripParens(assign.Lhs[0]).(*ast.Ident)
	if !ok {
		return false
	}
	variable, ok := objectForIdent(pass, identifier).(*types.Var)
	if !ok || variable.IsField() || variable.Parent() == nil || variable.Parent() == pass.Pkg.Scope() {
		return false
	}
	source := stripParens(assign.Rhs[0])
	if address, addressOK := source.(*ast.UnaryExpr); addressOK && address.Op == token.AND {
		source = stripParens(address.X)
	}
	return target.matchesExpr(pass, source)
}

func refineBoundaryRequestResult(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	param boundaryRequestParam,
	funcCFG *gocfg.CFG,
	refiner cfgRefinementController,
	solver interprocSolver,
	input interprocUBVCrossBlockInput,
	result interprocPathResult,
) interprocPathResult {
	findingID := boundaryRequestFindingID(pass, fn, param)
	return refiner.Refine(cfgRefinementRequest{
		Pass:          pass,
		Position:      param.pos,
		CFG:           funcCFG,
		Result:        result,
		Category:      CategoryUnvalidatedBoundaryRequest,
		FindingID:     findingID,
		CallChain:     input.CallChain,
		OriginAnchors: map[string]string{"boundary_parameter_pos": input.OriginKey},
		SyntheticPath: []int32{input.DefBlock.Index},
		Control:       solver.control,
		Rerun: func(override cfgRefinementOverride) interprocPathResult {
			next := input
			if override.MaxStates > 0 {
				next.MaxStates = override.MaxStates
			}
			next.DischargedWitnesses = override.DischargedWitnesses
			if override.ResolveTargets {
				next.ResolveCFGCalls = true
			}
			return solver.EvaluateUBVCrossBlock(next)
		},
	})
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
			target.typeKey = typeIdentityKey(paramType)
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
		if boundaryRequestExprReferencesTarget(pass, assign.Rhs[0], target) {
			return false
		}
	}
	return true
}

func boundaryRequestExprReferencesTarget(pass *analysis.Pass, expr ast.Expr, target castTarget) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		candidate, ok := node.(ast.Expr)
		if !ok || !target.matchesExpr(pass, candidate) {
			return true
		}
		found = true
		return false
	})
	return found
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
	result interprocPathResult,
) {
	qualName := qualFuncName(pass, fn)
	exceptionKey := qualName + "." + param.name + ".boundary-request-validation"
	funcExceptionKey := qualName + ".boundary-request-validation"
	if protocolPolicySuppressesDefiniteFinding(
		pathOutcomeUnsafe,
		func() bool {
			return cfg != nil && (cfg.isExcepted(exceptionKey) || cfg.isExcepted(funcExceptionKey))
		},
	) {
		return
	}
	findingID := boundaryRequestFindingID(pass, fn, param)
	msg := fmt.Sprintf(
		"parameter %q of %s is used before checked Validate() at exported boundary",
		param.name,
		param.typeName,
	)
	meta := appendProtocolRefinementMeta(map[string]string{
		"cfg_outcome_status": cfgRefinementStatusViolation,
	}, result)
	reportFindingWithMetaIfNotBaselined(pass, bl, param.pos, CategoryUnvalidatedBoundaryRequest, findingID, msg, meta)
}

func boundaryRequestFindingID(pass *analysis.Pass, fn *ast.FuncDecl, param boundaryRequestParam) string {
	return PackageScopedFindingID(
		pass,
		CategoryUnvalidatedBoundaryRequest,
		qualFuncName(pass, fn),
		param.name,
		param.typeName,
		semanticNodeKey(pass, param.pos),
	)
}
