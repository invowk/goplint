// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

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
	cfgMaxStates int,
	cfgWitnessMaxSteps int,
	refinement cfgProtocolRefinementOptions,
	calleeSummaryCache *sync.Map,
	ssaRes *ssaResult,
) error {
	pkgName := packageName(pass.Pkg)
	solver := newInterprocSolverWithSSA(pass, ssaRes, calleeSummaryCache)
	refiner := newCFGRefinementController(refinement)

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

			qualName := fmt.Sprintf("%s.%s", pkgName, name)
			excKey := qualName + ".constructor-validate"

			effectiveBudget := blockVisitBudget{maxStates: cfgMaxStates}
			if backendCFG := buildProtocolCFG(pass, fn.Body, ssaRes); backendCFG != nil {
				effectiveBudget = adaptiveBlockVisitBudget(backendCFG, effectiveBudget)
			}
			// Check whether constructor paths validate the returned type through
			// the canonical SSA/IFDS protocol analysis.
			pathInput := interprocConstructorPathInput{
				Decl:            fn,
				ReturnTypeKey:   returnTypeKey,
				ResultSlot:      retInfo.ResultSlot,
				Constructor:     qualName,
				MaxStates:       effectiveBudget.maxStates,
				CallChain:       []string{qualName},
				SSAAvailability: protocolSSAAvailabilityForDecl(pass, ssaRes, fn),
			}
			pathSolver := solver.withControl(refiner.newDeadline())
			pathResult := pathSolver.EvaluateConstructorPath(pathInput)
			pathResult = refiner.Refine(cfgRefinementRequest{
				Pass:      pass,
				Position:  fn.Name.Pos(),
				CFG:       buildProtocolCFG(pass, fn.Body, ssaRes),
				Result:    pathResult,
				Category:  CategoryMissingConstructorValidate,
				FindingID: PackageScopedFindingID(pass, CategoryMissingConstructorValidate, qualName, returnType),
				CallChain: []string{qualName},
				Control:   pathSolver.control,
				Rerun: func(override cfgRefinementOverride) interprocPathResult {
					next := pathInput
					if override.MaxStates > 0 {
						next.MaxStates = override.MaxStates
					}
					next.DischargedWitnesses = override.DischargedWitnesses
					return pathSolver.EvaluateConstructorPath(next)
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
				meta := cfgOutcomeMetaWithWitness(effectiveBudget.maxStates, pathReason, pathWitness, cfgWitnessMaxSteps)
				addCFGWitnessCallChainMeta(meta, []string{qualName}, cfgWitnessMaxSteps)
				meta = appendProtocolRefinementMeta(meta, pathResult)
				reportInconclusiveFindingWithMetaIfNotBaselined(
					pass,
					bl,
					fn.Name.Pos(),
					CategoryMissingConstructorValidateInc,
					findingID,
					msg,
					meta,
				)
				continue
			}
			if protocolPolicySuppressesDefiniteFinding(
				pathOutcome,
				func() bool { return hasIgnoreDirective(fn.Doc, nil) },
				func() bool { return cfg != nil && cfg.isExcepted(excKey) },
			) {
				continue
			}

			msg := fmt.Sprintf(
				"constructor %s returns %s.%s which has Validate() but never calls it",
				qualName, returnTypePkg, returnType)
			findingID := PackageScopedFindingID(pass, CategoryMissingConstructorValidate, qualName, returnType)
			var meta map[string]string
			if pathResult.Refinement.Enabled {
				meta = appendProtocolRefinementMeta(nil, pathResult)
				addCFGWitnessMeta(meta, pathWitness, cfgWitnessMaxSteps)
				addCFGWitnessCallChainMeta(meta, []string{qualName}, cfgWitnessMaxSteps)
			}
			reportFindingWithMetaIfNotBaselined(pass, bl, fn.Name.Pos(), CategoryMissingConstructorValidate, findingID, msg, meta)
		}
	}

	return nil
}

type returnTypeValidateInfo struct {
	HasValidate bool
	ResultSlot  int
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
	resultSlot := 0
	for slot := range sig.Results().Len() {
		candidate := sig.Results().At(slot).Type()
		if !isErrorType(candidate) {
			retType = candidate
			resultSlot = slot
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
		ResultSlot:  resultSlot,
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
