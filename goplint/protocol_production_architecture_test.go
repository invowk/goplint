// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestProtocolProductionArchitecture(t *testing.T) {
	t.Parallel()
	config := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo,
		Dir: ".",
	}
	loaded, err := packages.Load(config, ".")
	if err != nil {
		t.Fatalf("packages.Load() error: %v", err)
	}
	if packages.PrintErrors(loaded) != 0 {
		t.Fatal("production package has load errors")
	}
	if len(loaded) != 1 {
		t.Fatalf("packages.Load() returned %d packages, want 1", len(loaded))
	}

	pkg := loaded[0]
	forbiddenSymbols := map[string]struct{}{
		"ValidatesTypeFact":                          {},
		"EvaluateCastPathLegacy":                     {},
		"EvaluateUBVCrossBlockLegacy":                {},
		"EvaluateUBVInBlockLegacy":                   {},
		"buildFuncCFG":                               {},
		"buildFuncCFGForBackend":                     {},
		"cfgTraversalModeConstructorPath":            {},
		"cfgTraversalModeLegacy":                     {},
		"cfgTraversalModeUBVOrder":                   {},
		"cfgVisitStateFromBlockVisited":              {},
		"dfsUnvalidatedBlocks":                       {},
		"newCastTargetFromName":                      {},
		"newInterprocCompatTracker":                  {},
		"collectProtocolCheckedValidationPositions":  {},
		"markCheckedValidationCalls":                 {},
		"protocolCheckedValidationForClosure":        {},
		"protocolCheckedValidationForDecl":           {},
		"protocolCheckedValidationsForClosure":       {},
		"protocolCheckedValidationsForDecl":          {},
		"protocolCheckedValidationsForPackage":       {},
		"protocolCheckedValidationsForDeclWithSSA":   {},
		"ideValidationState":                         {},
		"joinPathOutcomeReasons":                     {},
		"unresolvedReason":                           {},
		"callerReason":                               {},
		"callerUncertainty":                          {},
		"callerIdentity":                             {},
		"runIFDSPropagation":                         {},
		"runIFDSPropagationWithSink":                 {},
		"firstInterprocEdgeTransfer":                 {},
		"firstUseValidateOrderInNode":                {},
		"firstUseValidateOrderInNodeSeen":            {},
		"ubvOrderResult":                             {},
		"protocolAliasState":                         {},
		"protocolUncertaintyReason":                  {},
		"calleeSummaryEffectState":                   {},
		"protocolRecursiveValidationNode":            {},
		"boundaryRequestParamHasUseBeforeValidation": {},
		"boundaryRequestSafeDelegationStmt":          {},
		"boundaryRequestUse":                         {},
		"isExecutableClosureLiteral":                 {},
		"closureLiteralCall":                         {},
		"containsDeferredConstructorValidation":      {},
		"constructorNamedErrorResultNames":           {},
		"constructorDefersPreserveResultSlot":        {},
		"constructorDeferredExecutionState":          {},
		"firstCallExprInNode":                        {},
	}
	forbiddenFragments := [][]byte{
		[]byte(`"name:"`),
		[]byte(`"expr:"`),
		[]byte("staleExceptionPatternFromMessage"),
		[]byte("StableFindingID(d.Category, d.Posn, d.Message)"),
	}
	for index, file := range pkg.Syntax {
		filename := pkg.CompiledGoFiles[index]
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.Ident:
				if _, forbidden := forbiddenSymbols[typed.Name]; !forbidden {
					return true
				}
				if pkg.TypesInfo.Defs[typed] != nil || pkg.TypesInfo.Uses[typed] != nil {
					t.Errorf("%s: forbidden production symbol %q", pkg.Fset.Position(typed.Pos()), typed.Name)
				}
			case *ast.BinaryExpr:
				if typed.Op != token.EQL && typed.Op != token.NEQ {
					return true
				}
				if (isNilIdentifier(typed.X) && isTraversalContextType(pkg.TypesInfo.TypeOf(typed.Y))) ||
					(isNilIdentifier(typed.Y) && isTraversalContextType(pkg.TypesInfo.TypeOf(typed.X))) {
					t.Errorf("%s: nil traversal-context compatibility branch", pkg.Fset.Position(typed.Pos()))
				}
			case *ast.CallExpr:
				assertNoNilTraversalContextArgument(t, pkg, typed)
				assertNoNilProtocolKeyArgument(t, pkg, typed)
			}
			return true
		})

		content, readErr := os.ReadFile(filename)
		if readErr != nil {
			t.Fatalf("os.ReadFile(%s) error: %v", filename, readErr)
		}
		for symbol := range forbiddenSymbols {
			pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(symbol) + `\b`)
			if pattern.Match(content) {
				t.Errorf("%s: stale forbidden production reference %q", filename, symbol)
			}
		}
		for _, fragment := range forbiddenFragments {
			if bytes.Contains(content, fragment) {
				t.Errorf("%s: stale syntactic protocol identity fragment %q", filename, fragment)
			}
		}
	}
	assertCanonicalProtocolStateCarriers(t, pkg.Types.Scope())
}

func TestProtocolFindingPolicyIsConsultedOnlyAfterClassification(t *testing.T) {
	t.Parallel()

	config := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax,
		Dir: ".",
	}
	loaded, err := packages.Load(config, ".")
	if err != nil {
		t.Fatalf("packages.Load() error: %v", err)
	}
	if packages.PrintErrors(loaded) != 0 {
		t.Fatal("production package has load errors")
	}
	if len(loaded) != 1 {
		t.Fatalf("packages.Load() returned %d packages, want 1", len(loaded))
	}

	expectedHelperCalls := map[string]int{
		"inspectUnvalidatedCastsCFA":   2,
		"inspectClosureCastsCFA":       2,
		"inspectConstructorValidates":  1,
		"reportBoundaryRequestFinding": 1,
	}
	found := make(map[string]bool, len(expectedHelperCalls))
	pkg := loaded[0]
	for _, file := range pkg.Syntax {
		parents := buildParentMap(file)
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			wantHelperCalls, tracked := expectedHelperCalls[function.Name.Name]
			if !tracked {
				continue
			}
			found[function.Name.Name] = true
			helperCalls := 0
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				name := protocolArchitectureCallName(call.Fun)
				if name == "protocolPolicySuppressesDefiniteFinding" {
					helperCalls++
					return true
				}
				if name != "isExcepted" && name != "hasIgnoreAtPos" && name != "hasIgnoreDirective" {
					return true
				}
				if !protocolPolicyCallIsLazy(call, parents) {
					t.Errorf(
						"%s: %s calls suppression policy outside protocolPolicySuppressesDefiniteFinding callback",
						pkg.Fset.Position(call.Pos()),
						function.Name.Name,
					)
				}
				return true
			})
			if helperCalls != wantHelperCalls {
				t.Errorf(
					"%s has %d protocolPolicySuppressesDefiniteFinding calls, want %d",
					function.Name.Name,
					helperCalls,
					wantHelperCalls,
				)
			}
		}
	}
	for function := range expectedHelperCalls {
		if !found[function] {
			t.Errorf("protocol finding entry point %s is missing", function)
		}
	}
}

func protocolArchitectureCallName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	default:
		return ""
	}
}

func protocolPolicyCallIsLazy(call *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	for node := ast.Node(call); node != nil; node = parents[node] {
		ancestor, ok := node.(*ast.CallExpr)
		if ok && protocolArchitectureCallName(ancestor.Fun) == "protocolPolicySuppressesDefiniteFinding" {
			return true
		}
	}
	return false
}

func assertCanonicalProtocolStateCarriers(t *testing.T, scope *types.Scope) {
	t.Helper()
	for typeName, fieldNames := range map[string][]string{
		"interprocEntryFact":        {"node", "state"},
		"interprocPathEdge":         {"entry", "node", "state", "edgeFunction", "path", "witness"},
		"interprocProcedureSummary": {"entry", "exit", "state", "edgeFunction", "path", "witness", "exitStateBefore", "exitTransferTag"},
	} {
		object := scope.Lookup(typeName)
		if object == nil {
			t.Errorf("production type %s is missing", typeName)
			continue
		}
		named, ok := object.Type().(*types.Named)
		if !ok {
			t.Errorf("production object %s has type %T, want named type", typeName, object.Type())
			continue
		}
		structure, ok := named.Underlying().(*types.Struct)
		if !ok {
			t.Errorf("production type %s has underlying %T, want struct", typeName, named.Underlying())
			continue
		}
		if structure.NumFields() != len(fieldNames) {
			t.Errorf("production type %s has %d fields, want %d canonical fields", typeName, structure.NumFields(), len(fieldNames))
			continue
		}
		for index, wantName := range fieldNames {
			if gotName := structure.Field(index).Name(); gotName != wantName {
				t.Errorf("production type %s field %d = %s, want %s", typeName, index, gotName, wantName)
			}
		}
		assertCanonicalStateField(t, typeName, structure, "state")
		if typeName == "interprocProcedureSummary" {
			assertCanonicalStateField(t, typeName, structure, "exitStateBefore")
		}
	}

	object := scope.Lookup("interprocWitnessEdge")
	if object == nil {
		t.Fatal("production type interprocWitnessEdge is missing")
	}
	structure := object.Type().Underlying().(*types.Struct)
	assertCanonicalStateField(t, "interprocWitnessEdge", structure, "StateBefore")
	assertCanonicalStateField(t, "interprocWitnessEdge", structure, "StateAfter")
}

func assertCanonicalStateField(t *testing.T, owner string, structure *types.Struct, fieldName string) {
	t.Helper()
	for field := range structure.Fields() {
		if field.Name() != fieldName {
			continue
		}
		named, ok := field.Type().(*types.Named)
		if !ok || named.Obj().Name() != "protocolAbstractState" {
			t.Errorf("%s.%s has type %s, want protocolAbstractState", owner, fieldName, field.Type())
		}
		return
	}
	t.Errorf("%s is missing canonical state field %s", owner, fieldName)
}

func TestProtocolTestsUseCanonicalComponents(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("filepath.Glob() error: %v", err)
	}
	forbidden := map[string]struct{}{
		"EvaluateCastPathLegacy":                    {},
		"EvaluateUBVCrossBlockLegacy":               {},
		"EvaluateUBVInBlockLegacy":                  {},
		"buildFuncCFGForBackend":                    {},
		"cfgTraversalModeConstructorPath":           {},
		"cfgTraversalModeLegacy":                    {},
		"cfgTraversalModeUBVOrder":                  {},
		"dfsUnvalidatedBlocks":                      {},
		"newCastTargetFromName":                     {},
		"newInterprocCompatTracker":                 {},
		"collectProtocolCheckedValidationPositions": {},
		"markCheckedValidationCalls":                {},
		"protocolCheckedValidationForClosure":       {},
		"protocolCheckedValidationForDecl":          {},
	}
	for _, filename := range files {
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if parseErr != nil {
			t.Fatalf("parser.ParseFile(%s) error: %v", filename, parseErr)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			if _, stale := forbidden[identifier.Name]; stale {
				t.Errorf("%s: test code selects removed protocol component %q", filename, identifier.Name)
			}
			return true
		})
	}
}

func assertNoNilProtocolKeyArgument(t *testing.T, pkg *packages.Package, call *ast.CallExpr) {
	t.Helper()

	identifier, ok := call.Fun.(*ast.Ident)
	if !ok || identifier.Name != "targetKeyForExpr" || len(call.Args) == 0 || !isNilIdentifier(call.Args[0]) {
		return
	}
	if object := pkg.TypesInfo.Uses[identifier]; object != nil && object.Pkg() == pkg.Types {
		t.Errorf("%s: nil analysis pass selects a syntactic protocol identity fallback", pkg.Fset.Position(call.Pos()))
	}
}

func assertNoNilTraversalContextArgument(t *testing.T, pkg *packages.Package, call *ast.CallExpr) {
	t.Helper()

	signature, ok := pkg.TypesInfo.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return
	}
	for index, argument := range call.Args {
		if !isNilIdentifier(argument) || index >= signature.Params().Len() {
			continue
		}
		if isTraversalContextType(signature.Params().At(index).Type()) {
			t.Errorf("%s: nil traversal context passed to production call", pkg.Fset.Position(argument.Pos()))
		}
	}
}

func isNilIdentifier(expr ast.Expr) bool {
	identifier, ok := expr.(*ast.Ident)
	return ok && identifier.Name == "nil"
}

func isTraversalContextType(typ types.Type) bool {
	pointer, ok := typ.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := pointer.Elem().(*types.Named)
	return ok && named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == "github.com/invowk/invowk/tools/goplint/goplint" &&
		named.Obj().Name() == "cfgTraversalContext"
}
