// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// maxTransitiveDepth is the maximum call chain depth for transitive
// factory tracking in --check-constructor-validates. This bounds
// recursion in bodyCallsValidateTransitive() to prevent pathological
// cases while allowing realistic delegation chains (e.g.,
// NewFoo → buildBar → initBaz → baz.Validate()).
const maxTransitiveDepth = 5

// ValidatesTypeFact is an analysis.Fact exported for functions annotated
// with //goplint:validates-type=TypeName. The directive indicates that
// the function validates the named type on behalf of a constructor,
// enabling cross-package constructor-validates tracking.
//
// When goplint processes a package containing a helper function annotated
// with this directive, it exports the fact. Consuming packages can then
// import the fact when checking whether a constructor's call to the helper
// satisfies the Validate() requirement.
type ValidatesTypeFact struct {
	TypeName    string // unqualified type name (e.g., "Server")
	TypePkgPath string // package path for the type name (optional for legacy facts)
}

// AFact implements the analysis.Fact interface marker method.
func (*ValidatesTypeFact) AFact() {}

// String returns a human-readable representation for analysistest fact matching.
func (f *ValidatesTypeFact) String() string {
	return fmt.Sprintf("validates-type(%s)", f.TypeName)
}

// exportValidatesTypeFacts scans a function declaration for the
// //goplint:validates-type=TypeName directive and exports a
// ValidatesTypeFact for the function. This enables cross-package
// tracking in bodyCallsValidateTransitive.
//
// Only free functions (not methods) are supported — the directive is
// intended for standalone helper functions like util.ValidateServer().
func exportValidatesTypeFacts(pass *analysis.Pass, fn *ast.FuncDecl) {
	if fn.Recv != nil {
		return
	}
	directiveType, ok := directiveValue([]*ast.CommentGroup{fn.Doc}, "validates-type")
	if !ok || directiveType == "" {
		return
	}
	typeName, typePkgPath := resolveDirectiveTypeIdentity(pass, fn, directiveType)
	if typeName == "" {
		return
	}
	obj := pass.TypesInfo.Defs[fn.Name]
	if obj == nil {
		return
	}
	pass.ExportObjectFact(obj, &ValidatesTypeFact{
		TypeName:    typeName,
		TypePkgPath: typePkgPath,
	})
}

func resolveDirectiveTypeIdentity(pass *analysis.Pass, fn *ast.FuncDecl, raw string) (typeName, typePkgPath string) {
	directive := strings.TrimSpace(raw)
	if directive == "" {
		return "", ""
	}

	pkgAlias := ""
	if dot := strings.LastIndex(directive, "."); dot > 0 && dot < len(directive)-1 {
		left := directive[:dot]
		right := directive[dot+1:]
		directive = right
		if strings.Contains(left, "/") {
			typePkgPath = left
		} else {
			pkgAlias = left
		}
	}

	typeName = directive
	if typePkgPath == "" {
		typePkgPath = inferDirectiveTypePkgPath(pass, fn, typeName, pkgAlias)
	}
	if typePkgPath == "" && pass != nil && pass.Pkg != nil {
		typePkgPath = pass.Pkg.Path()
	}
	return typeName, typePkgPath
}

func inferDirectiveTypePkgPath(pass *analysis.Pass, fn *ast.FuncDecl, typeName, pkgAlias string) string {
	if pass == nil || pass.TypesInfo == nil || fn == nil {
		return ""
	}
	obj := pass.TypesInfo.Defs[fn.Name]
	if obj == nil {
		return ""
	}
	fnObj, ok := obj.(*types.Func)
	if !ok {
		return ""
	}
	sig, ok := fnObj.Type().(*types.Signature)
	if !ok {
		return ""
	}

	if path := matchNamedTypePkgPath(sig.Params(), typeName, pkgAlias); path != "" {
		return path
	}
	if path := matchNamedTypePkgPath(sig.Results(), typeName, pkgAlias); path != "" {
		return path
	}
	return ""
}

func matchNamedTypePkgPath(tuple *types.Tuple, typeName, pkgAlias string) string {
	if tuple == nil || typeName == "" {
		return ""
	}
	for variable := range tuple.Variables() {
		t := variable.Type()
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		}
		t = types.Unalias(t)
		named, ok := t.(*types.Named)
		if !ok || named.Obj() == nil || named.Obj().Name() != typeName {
			continue
		}
		pkg := named.Obj().Pkg()
		if pkg == nil {
			continue
		}
		if pkgAlias != "" && pkg.Name() != pkgAlias {
			continue
		}
		return pkg.Path()
	}
	return ""
}

// inspectConstructorValidates checks whether NewXxx() constructors call
// Validate() on the type they construct. Constructors returning types with
// a Validate() method should call it before returning to enforce invariants.
//
// Types annotated with //goplint:constant-only are exempt — their values
// only come from compile-time constants, so runtime validation is unnecessary.
//
// This is a post-traversal check: it receives the constructorDetails map
// already populated by trackConstructorDetails, then walks the function
// bodies looking for .Validate() calls.
func inspectConstructorValidates(
	pass *analysis.Pass,
	ctors map[string]*constructorFuncInfo,
	constantOnlyTypes map[string]bool,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
	cfgBackend string,
	cfgInterprocEngine string,
	cfgMaxStates int,
	cfgMaxDepth int,
	cfgInconclusivePolicy string,
	cfgWitnessMaxSteps int,
	phaseC cfgPhaseCOptions,
) error {
	pkgName := packageName(pass.Pkg)
	solver := newInterprocSolver(pass, cfgBackend, cfgInterprocEngine)
	compatTracker := newInterprocCompatTracker(cfgInterprocEngine)
	refiner := newCFGRefinementController(phaseC)
	constructorUnsafeByIdentity := make(map[string]bool)

	// Build a set of struct names that have Validate() methods.
	validatableStructs := buildValidatableStructs(pass)

	// Walk all files to find constructor function bodies.
	for _, file := range pass.Files {
		if isTestFile(pass, file.Pos()) {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}

			name := fn.Name.Name
			if !strings.HasPrefix(name, "New") || len(name) <= 3 {
				continue
			}

			ctorInfo, exists := ctors[name]
			if !exists {
				continue
			}

			// Skip constructors returning interfaces — they may delegate
			// validation to the concrete implementation.
			if ctorInfo.returnsInterface {
				continue
			}

			retInfo := resolveReturnTypeValidateInfo(pass, fn)
			returnType := retInfo.TypeName
			if returnType == "" {
				returnType = ctorInfo.returnTypeName
			}
			if returnType == "" {
				continue
			}
			returnTypePkg := retInfo.TypePkgName
			if returnTypePkg == "" {
				returnTypePkg = pkgName
			}
			returnTypePkgPath := retInfo.TypePkgPath
			if returnTypePkgPath == "" {
				returnTypePkgPath = pass.Pkg.Path()
			}
			returnTypeKey := retInfo.TypeKey
			if returnTypeKey == "" {
				returnTypeKey = returnTypePkgPath + "." + returnType
			}

			// Check if the return type has Validate(). Try same-package
			// fast path first; fall back to type-checker resolution for
			// cross-package and alias-heavy cases.
			if !retInfo.HasValidate && !(returnTypePkgPath == pass.Pkg.Path() && validatableStructs[returnType]) {
				continue
			}

			// Skip types annotated with //goplint:constant-only — their
			// Validate() is intentionally unwired because all values come
			// from compile-time constants.
			if constantOnlyTypes[returnTypeKey] {
				continue
			}

			// Check for ignore directive on the function.
			if hasIgnoreDirective(fn.Doc, nil) {
				continue
			}

			qualName := fmt.Sprintf("%s.%s", pkgName, name)
			excKey := qualName + ".constructor-validate"
			if cfg.isExcepted(excKey) {
				continue
			}

			effectiveBudget := blockVisitBudget{
				maxStates: cfgMaxStates,
				maxDepth:  cfgMaxDepth,
			}
			if backendCFG := buildFuncCFGForBackend(pass, fn.Body, cfgBackend); backendCFG != nil {
				effectiveBudget = adaptiveBlockVisitBudget(backendCFG, effectiveBudget)
			}

			// Check whether constructor paths validate the returned type.
			// CFA mode is required for constructor-validates.
			pathInput := interprocConstructorPathInput{
				Decl:              fn,
				ReturnTypeKey:     returnTypeKey,
				ReturnTypePkgPath: returnTypePkgPath,
				Constructor:       qualName,
				ReturnType:        returnType,
				MaxStates:         effectiveBudget.maxStates,
				MaxDepth:          effectiveBudget.maxDepth,
				CallChain:         []string{qualName},
			}
			pathLegacy := solver.EvaluateConstructorPathLegacy(pathInput)
			pathResult := solver.EvaluateConstructorPath(pathInput)
			identity := qualName + "|" + returnTypeKey
			if pathResult.Class == interprocOutcomeUnsafe {
				constructorUnsafeByIdentity[identity] = true
			}
			compatTracker.Check(
				CategoryMissingConstructorValidate,
				PackageScopedFindingID(pass, CategoryMissingConstructorValidate, qualName, returnType),
				pathLegacy,
				pathResult,
				constructorUnsafeByIdentity[identity],
			)
			pathResult = refiner.Refine(cfgRefinementRequest{
				Pass:      pass,
				Position:  fn.Name.Pos(),
				CFG:       buildFuncCFGForBackend(pass, fn.Body, cfgBackend),
				Result:    pathResult,
				Category:  CategoryMissingConstructorValidate,
				FindingID: PackageScopedFindingID(pass, CategoryMissingConstructorValidate, qualName, returnType),
				CallChain: []string{qualName},
				Rerun: func(override cfgRefinementOverride) interprocPathResult {
					next := pathInput
					if override.MaxStates > 0 {
						next.MaxStates = override.MaxStates
					}
					if override.MaxDepth > 0 {
						next.MaxDepth = override.MaxDepth
					}
					next.DischargedWitnesses = override.DischargedWitnesses
					if override.RefineRecursion {
						next.SummaryStack = summaryStackWithRecursionFallback(next.SummaryStack)
					}
					refined := solver.EvaluateConstructorPath(next)
					if override.ResolveTargets &&
						refined.Class == interprocOutcomeInconclusive &&
						refined.Reason == pathOutcomeReasonUnresolvedTarget {
						refined = mergeResolvedTargetRefinement(refined, solver.EvaluateConstructorPathLegacy(next))
					}
					return refined
				},
			})
			writeRefinementTraceToSink(pass, fn.Name.Pos(), pathResult)
			pathOutcome := pathResult.toPathOutcome()
			pathReason := pathResult.Reason
			pathWitness := pathResult.Witness
			if pathOutcome == pathOutcomeSafe {
				continue
			}

			if pathOutcome == pathOutcomeInconclusive {
				msg := constructorValidateInconclusiveMessage(qualName, returnTypePkg, returnType)
				findingID := PackageScopedFindingID(
					pass,
					CategoryMissingConstructorValidateInc,
					qualName,
					returnType,
					"inconclusive",
					string(pathReason),
				)
				meta := cfgOutcomeMetaWithWitness(cfgBackend, effectiveBudget.maxStates, effectiveBudget.maxDepth, pathReason, pathWitness, cfgWitnessMaxSteps)
				addCFGWitnessCallChainMeta(meta, []string{qualName}, cfgWitnessMaxSteps)
				meta = appendPhaseCMeta(meta, pathResult)
				reportInconclusiveFindingWithMetaIfNotBaselined(
					pass,
					bl,
					cfgInconclusivePolicy,
					fn.Name.Pos(),
					CategoryMissingConstructorValidateInc,
					findingID,
					msg,
					meta,
				)
				continue
			}

			msg := fmt.Sprintf(
				"constructor %s returns %s.%s which has Validate() but never calls it",
				qualName, returnTypePkg, returnType)
			findingID := PackageScopedFindingID(pass, CategoryMissingConstructorValidate, qualName, returnType)
			var meta map[string]string
			if pathResult.PhaseC.Enabled {
				meta = appendPhaseCMeta(nil, pathResult)
				addCFGWitnessMeta(meta, pathWitness, cfgWitnessMaxSteps)
				addCFGWitnessCallChainMeta(meta, []string{qualName}, cfgWitnessMaxSteps)
			}
			reportFindingWithMetaIfNotBaselined(pass, bl, fn.Name.Pos(), CategoryMissingConstructorValidate, findingID, msg, meta)
		}
	}

	return compatTracker.Err()
}

// methodCallTarget identifies a method call on the constructor's return type
// for transitive validation tracking.
type methodCallTarget struct {
	typeName   string
	typeKey    string
	methodName string
}

// bodyCallsValidateTransitive checks if any private function or method called
// from body transitively calls Validate() on the given return type. Uses
// pass.TypesInfo to resolve callee identities. Bounds recursion depth
// to maxTransitiveDepth to prevent pathological cases. The visited map
// prevents cycles (re-visiting the same function/method); depth tracks the
// actual call chain depth independently.
//
// This function follows two kinds of callees:
//  1. Same-package bare function calls (e.g., helper()) — via *ast.Ident
//  2. Method calls on variables whose type matches returnTypeName
//     (e.g., s.Setup() where s is *Server) — via *ast.SelectorExpr
func bodyCallsValidateTransitive(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	returnTypeName string,
	returnTypePkgPath string,
	returnTypeKey string,
	visited map[string]bool,
	depth int,
) bool {
	if visited == nil {
		visited = make(map[string]bool)
	}

	// Bound recursion by call chain depth, not visit count.
	if depth >= maxTransitiveDepth {
		return false
	}

	// Collect bare function call identifiers AND method calls on the return type.
	// Calls in conditionally-evaluated contexts are ignored to avoid treating
	// partial/dead-branch delegation as whole-function coverage.
	var bareFuncCallees []string
	var methodCallees []methodCallTarget

	crossPkgValidates := false
	parentMap := buildParentMap(body)

	ast.Inspect(body, func(n ast.Node) bool {
		if crossPkgValidates {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isConditionallyEvaluated(call, parentMap) {
			return true
		}

		switch fun := call.Fun.(type) {
		case *ast.Ident:
			// Bare function calls — same-package or cross-package with fact.
			obj := pass.TypesInfo.Uses[fun]
			if obj == nil {
				return true
			}
			fn, ok := obj.(*types.Func)
			if !ok {
				return true
			}
			if fn.Pkg() != pass.Pkg {
				// Cross-package: check for validates-type fact.
				var fact ValidatesTypeFact
				if pass.ImportObjectFact(fn, &fact) && factMatchesReturnType(fact, returnTypeName, returnTypePkgPath) {
					crossPkgValidates = true
					return false
				}
				return true
			}
			bareFuncCallees = append(bareFuncCallees, fun.Name)

		case *ast.SelectorExpr:
			// Skip direct Validate() calls in this transitive helper walk.
			if fun.Sel.Name == "Validate" {
				return true
			}

			// Check if this is a cross-package function call (pkg.Func pattern)
			// with a validates-type fact.
			if ident, ok := fun.X.(*ast.Ident); ok {
				obj := pass.TypesInfo.Uses[ident]
				if _, isPkgName := obj.(*types.PkgName); isPkgName {
					// This is a qualified call: pkg.Func(...)
					selObj := pass.TypesInfo.Uses[fun.Sel]
					if selObj != nil {
						if callee, ok := selObj.(*types.Func); ok {
							var fact ValidatesTypeFact
							if pass.ImportObjectFact(callee, &fact) && factMatchesReturnType(fact, returnTypeName, returnTypePkgPath) {
								crossPkgValidates = true
								return false
							}
						}
					}
					return true
				}
			}

			// Method calls on variables whose type matches the return type.
			receiverType := pass.TypesInfo.TypeOf(fun.X)
			if receiverType == nil {
				return true
			}
			// Dereference pointers: *Server → Server.
			if ptr, ok := receiverType.(*types.Pointer); ok {
				receiverType = ptr.Elem()
			}
			receiverType = types.Unalias(receiverType)
			named, ok := receiverType.(*types.Named)
			if !ok {
				return true
			}
			if typeIdentityKey(receiverType) == returnTypeKey && named.Obj().Pkg() == pass.Pkg {
				methodCallees = append(methodCallees, methodCallTarget{
					typeName:   named.Obj().Name(),
					typeKey:    returnTypeKey,
					methodName: fun.Sel.Name,
				})
			}
		}
		return true
	})

	// Cross-package fact match found — the callee validates the return type.
	if crossPkgValidates {
		return true
	}

	// Follow bare function callees.
	for _, calleeName := range bareFuncCallees {
		if visited[calleeName] {
			continue
		}
		visited[calleeName] = true

		calleeBody := findFuncBody(pass, calleeName)
		if calleeBody == nil {
			continue
		}
		if helperBodyAlwaysValidatesType(pass, calleeBody, returnTypeKey) {
			return true
		}
		// Recurse into the callee's body.
		if bodyCallsValidateTransitive(pass, calleeBody, returnTypeName, returnTypePkgPath, returnTypeKey, visited, depth+1) {
			return true
		}
	}

	// Follow method callees on the return type.
	for _, mc := range methodCallees {
		visitKey := mc.typeKey + "." + mc.methodName
		if visited[visitKey] {
			continue
		}
		visited[visitKey] = true

		methodBody, _ := findMethodBody(pass, mc.typeName, mc.methodName)
		if methodBody == nil {
			continue
		}
		if helperBodyAlwaysValidatesType(pass, methodBody, returnTypeKey) {
			return true
		}
		if bodyCallsValidateTransitive(pass, methodBody, returnTypeName, returnTypePkgPath, returnTypeKey, visited, depth+1) {
			return true
		}
	}

	return false
}

type returnTypeValidateInfo struct {
	HasValidate bool
	TypeName    string
	TypePkgName string
	TypePkgPath string
	TypeKey     string
}

// resolveReturnTypeValidateInfo resolves the constructor's first non-error
// return type via the type checker and checks if it has a Validate() method.
func resolveReturnTypeValidateInfo(pass *analysis.Pass, fn *ast.FuncDecl) returnTypeValidateInfo {
	obj := pass.TypesInfo.Defs[fn.Name]
	if obj == nil {
		return returnTypeValidateInfo{}
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Results().Len() == 0 {
		return returnTypeValidateInfo{}
	}

	var retType types.Type
	for resultVar := range sig.Results().Variables() {
		candidate := resultVar.Type()
		if !isErrorType(candidate) {
			retType = candidate
			break
		}
	}
	if retType == nil {
		retType = sig.Results().At(0).Type()
	}
	if ptr, ok := retType.(*types.Pointer); ok {
		retType = ptr.Elem()
	}
	retType = types.Unalias(retType)

	info := returnTypeValidateInfo{
		HasValidate: hasValidateMethod(retType),
		TypeKey:     typeIdentityKey(retType),
	}
	if named, ok := retType.(*types.Named); ok {
		info.TypeName = named.Obj().Name()
		if pkg := named.Obj().Pkg(); pkg != nil {
			info.TypePkgName = packageName(pkg)
			info.TypePkgPath = pkg.Path()
		}
	}
	return info
}

func factMatchesReturnType(fact ValidatesTypeFact, returnTypeName, returnTypePkgPath string) bool {
	if fact.TypeName != returnTypeName {
		return false
	}
	// Legacy facts only had TypeName. Accept them for compatibility.
	if fact.TypePkgPath == "" {
		return true
	}
	return fact.TypePkgPath == returnTypePkgPath
}

func typeIdentityKey(t types.Type) string {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	t = types.Unalias(t)
	return types.TypeString(t, func(pkg *types.Package) string {
		if pkg == nil {
			return ""
		}
		return pkg.Path()
	})
}

// findFuncBody searches the package for a non-method function with the given
// name and returns its body. Returns nil if not found.
func findFuncBody(pass *analysis.Pass, funcName string) *ast.BlockStmt {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			if fn.Name.Name == funcName {
				return fn.Body
			}
		}
	}
	return nil
}
