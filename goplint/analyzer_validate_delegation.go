// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// validateAllStruct records a struct annotated with //goplint:validate-all
// along with its fields that have Validate() methods.
type validateAllStruct struct {
	name            string    // type name (e.g., "Config")
	pos             token.Pos // position of the type declaration
	validatableKeys []string  // field names whose types have Validate()
}

// inspectValidateDelegation checks structs annotated with //goplint:validate-all
// for delegation completeness: every field whose type has Validate() should
// be called in the struct's own Validate() method.
func inspectValidateDelegation(pass *analysis.Pass, cfg *ExceptionConfig, bl *BaselineConfig) {
	// Phase 1: Collect structs with //goplint:validate-all directive and
	// their validatable fields.
	var targets []validateAllStruct
	for _, file := range pass.Files {
		if isTestFile(pass, file.Pos()) {
			continue
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				if !hasValidateAllDirective(gd.Doc, ts.Doc) {
					continue
				}

				// Collect fields whose types have Validate().
				validatableKeys := collectValidatableFieldKeys(pass, st)

				if len(validatableKeys) > 0 {
					targets = append(targets, validateAllStruct{
						name:            ts.Name.Name,
						pos:             ts.Name.Pos(),
						validatableKeys: validatableKeys,
					})
				}
			}
		}
	}

	if len(targets) == 0 {
		return
	}

	// Phase 2: For each target, find its Validate() method and check
	// which fields are delegated.
	for _, target := range targets {
		reportIncompleteDelegation(pass, target.name, target.pos, target.validatableKeys, cfg, bl)
	}
}

// findDelegatedFields searches the package for a Validate() method on the
// given type and returns a set of field names that appear in
// `receiver.Field.Validate()` call patterns. Also handles intermediate
// variable assignment: `field := receiver.Field; field.Validate()`.
func findDelegatedFields(pass *analysis.Pass, typeName string) map[string]bool {
	called := make(map[string]bool)

	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			recvName := receiverTypeName(fn.Recv.List[0].Type)
			if recvName != typeName || fn.Name.Name != "Validate" {
				continue
			}

			// Get the receiver variable name (e.g., "c" in "func (c *Config)")
			recvVarName := ""
			if len(fn.Recv.List[0].Names) > 0 {
				recvVarName = fn.Recv.List[0].Names[0].Name
			}
			parentMap := buildParentMap(fn.Body)

			// Pass 1: Direct receiver.Field.Validate() calls.
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if isConditionallyEvaluated(call, parentMap) &&
					!isWithinIfInit(call, parentMap) &&
					!isGuardedValidateCall(pass, call, parentMap) {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Validate" {
					return true
				}
				innerSel, ok := sel.X.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if recvVarName != "" {
					if ident, ok := innerSel.X.(*ast.Ident); ok && ident.Name == recvVarName {
						called[innerSel.Sel.Name] = true
					}
				}
				return true
			})

			// Pass 2: Intermediate variable pattern with rebinding awareness:
			//   field := receiver.Field
			//   var field = receiver.Field
			//   field.Validate()
			// Reassignments clear the alias unless they still point to receiver.Field.
			aliasBindings := collectDelegationAliasBindings(pass, fn.Body, recvVarName)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				callExpr, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if isConditionallyEvaluated(callExpr, parentMap) &&
					!isWithinIfInit(callExpr, parentMap) &&
					!isGuardedValidateCall(pass, callExpr, parentMap) {
					return true
				}
				selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
				if !ok || selExpr.Sel.Name != "Validate" {
					return true
				}
				ident, ok := selExpr.X.(*ast.Ident)
				if !ok {
					return true
				}
				aliasKey := targetKeyForExpr(pass, ident)
				if aliasKey == "" {
					return true
				}
				fieldName, ok := latestDelegationAliasFieldBefore(aliasBindings[aliasKey], callExpr.Pos())
				if !ok {
					return true
				}
				called[fieldName] = true
				return true
			})

			// Pass 3: Range loop delegation pattern:
			//   for _, r := range receiver.Field { r.Validate() }
			//   for i := range receiver.Field { receiver.Field[i].Validate() }
			// Recognizes iteration over slice/array fields with
			// validatable element types. Supports both value-variable
			// and index-variable delegation patterns.
			ast.Inspect(fn.Body, func(n ast.Node) bool { //nolint:dupl // distinct AST pattern
				rangeStmt, ok := n.(*ast.RangeStmt)
				if !ok {
					return true
				}
				sel, ok := rangeStmt.X.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if recvVarName == "" {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok || ident.Name != recvVarName {
					return true
				}
				fieldName := sel.Sel.Name

				// Get the range value variable name.
				valueVar := ""
				if rangeStmt.Value != nil {
					if vi, ok := rangeStmt.Value.(*ast.Ident); ok {
						valueVar = vi.Name
					}
				}

				// Get the range key (index) variable name.
				keyVar := ""
				if rangeStmt.Key != nil {
					if ki, ok := rangeStmt.Key.(*ast.Ident); ok {
						keyVar = ki.Name
					}
				}

				// Check if the loop body calls valueVar.Validate()
				// or receiver.Field[keyVar].Validate().
				ast.Inspect(rangeStmt.Body, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					callSel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || callSel.Sel.Name != "Validate" {
						return true
					}

					// Pattern 1: valueVar.Validate()
					if valueVar != "" {
						if vi, ok := callSel.X.(*ast.Ident); ok && vi.Name == valueVar {
							called[fieldName] = true
							return true
						}
					}

					// Pattern 2: receiver.Field[keyVar].Validate()
					if keyVar != "" {
						if indexExpr, ok := callSel.X.(*ast.IndexExpr); ok {
							if innerSel, ok := indexExpr.X.(*ast.SelectorExpr); ok {
								if innerIdent, ok := innerSel.X.(*ast.Ident); ok &&
									innerIdent.Name == recvVarName &&
									innerSel.Sel.Name == fieldName {
									if ki, ok := indexExpr.Index.(*ast.Ident); ok && ki.Name == keyVar {
										called[fieldName] = true
									}
								}
							}
						}
					}

					return true
				})
				return true
			})
			// Pass 4: Helper method delegation pattern:
			//   func (c *Config) Validate() error { return c.validateFields() }
			// When Validate() calls a method on the same receiver, walk that
			// method's body for direct field delegations.
			if recvVarName != "" {
				findHelperMethodDelegations(pass, fn.Body, typeName, recvVarName, nil, 0, called)
				findHelperFunctionDelegations(pass, fn.Body, recvVarName, aliasBindings, parentMap, called)
			}
		}
	}

	return called
}

type delegationAliasBindingEvent struct {
	pos       token.Pos
	fieldName string
}

func collectDelegationAliasBindings(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	recvVarName string,
) map[string][]delegationAliasBindingEvent {
	if pass == nil || pass.TypesInfo == nil || body == nil || recvVarName == "" {
		return nil
	}
	bindings := make(map[string][]delegationAliasBindingEvent)

	resolve := func(lhs ast.Expr, rhs ast.Expr, atPos token.Pos) (string, delegationAliasBindingEvent, bool) {
		lhsIdent, ok := stripParens(lhs).(*ast.Ident)
		if !ok || lhsIdent.Name == "_" {
			return "", delegationAliasBindingEvent{}, false
		}
		obj := objectForIdent(pass, lhsIdent)
		if obj == nil {
			return "", delegationAliasBindingEvent{}, false
		}
		if _, isVar := obj.(*types.Var); !isVar {
			return "", delegationAliasBindingEvent{}, false
		}
		lhsKey := objectKey(obj)
		if lhsKey == "" {
			return "", delegationAliasBindingEvent{}, false
		}

		event := delegationAliasBindingEvent{pos: atPos}
		if rhsSel, ok := stripParens(rhs).(*ast.SelectorExpr); ok {
			if rhsIdent, idOK := rhsSel.X.(*ast.Ident); idOK && rhsIdent.Name == recvVarName {
				event.fieldName = rhsSel.Sel.Name
				return lhsKey, event, true
			}
		}

		if rhsKey := targetKeyForExpr(pass, stripParens(rhs)); rhsKey != "" {
			if fieldName, aliasOK := latestDelegationAliasFieldBefore(bindings[rhsKey], atPos); aliasOK {
				event.fieldName = fieldName
			}
		}
		return lhsKey, event, true
	}

	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			type pendingBinding struct {
				key   string
				event delegationAliasBindingEvent
			}
			pending := make([]pendingBinding, 0, len(node.Rhs))
			for i, rhs := range node.Rhs {
				if i >= len(node.Lhs) {
					break
				}
				key, event, ok := resolve(node.Lhs[i], rhs, node.Lhs[i].Pos())
				if !ok {
					continue
				}
				pending = append(pending, pendingBinding{key: key, event: event})
			}
			for _, entry := range pending {
				bindings[entry.key] = append(bindings[entry.key], entry.event)
			}
		case *ast.ValueSpec:
			type pendingBinding struct {
				key   string
				event delegationAliasBindingEvent
			}
			pending := make([]pendingBinding, 0, len(node.Values))
			for i, rhs := range node.Values {
				if i >= len(node.Names) {
					break
				}
				key, event, ok := resolve(node.Names[i], rhs, node.Names[i].Pos())
				if !ok {
					continue
				}
				pending = append(pending, pendingBinding{key: key, event: event})
			}
			for _, entry := range pending {
				bindings[entry.key] = append(bindings[entry.key], entry.event)
			}
		case *ast.RangeStmt:
			// Track range value aliases: for _, v := range recv.Field { v.Validate() }.
			sel, ok := stripParens(node.X).(*ast.SelectorExpr)
			if !ok {
				break
			}
			recvIdent, ok := stripParens(sel.X).(*ast.Ident)
			if !ok || recvIdent.Name != recvVarName {
				break
			}
			valueIdent, ok := node.Value.(*ast.Ident)
			if !ok || valueIdent.Name == "_" {
				break
			}
			obj := objectForIdent(pass, valueIdent)
			if obj == nil {
				break
			}
			if _, isVar := obj.(*types.Var); !isVar {
				break
			}
			key := objectKey(obj)
			if key == "" {
				break
			}
			bindings[key] = append(bindings[key], delegationAliasBindingEvent{
				pos:       valueIdent.Pos(),
				fieldName: sel.Sel.Name,
			})
		}
		return true
	})

	return bindings
}

func latestDelegationAliasFieldBefore(events []delegationAliasBindingEvent, atPos token.Pos) (string, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].pos > atPos {
			continue
		}
		if events[i].fieldName == "" {
			return "", false
		}
		return events[i].fieldName, true
	}
	return "", false
}

func isGuardedValidateCall(
	pass *analysis.Pass,
	call *ast.CallExpr,
	parentMap map[ast.Node]ast.Node,
) bool {
	if pass == nil || call == nil || parentMap == nil {
		return false
	}
	sel, ok := stripParens(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Validate" {
		return false
	}
	receiverKey := targetKeyForExpr(pass, sel.X)
	if receiverKey == "" {
		return false
	}

	child := ast.Node(call)
	for {
		parent := parentMap[child]
		if parent == nil {
			return false
		}
		if ifStmt, ok := parent.(*ast.IfStmt); ok {
			bodyNonZero, elseNonZero, hasGuard := branchValueGuardForTarget(pass, ifStmt.Cond, receiverKey)
			if hasGuard {
				if bodyNonZero && isDescendantOrSelf(child, ifStmt.Body, parentMap) {
					return true
				}
				if elseNonZero && ifStmt.Else != nil && isDescendantOrSelf(child, ifStmt.Else, parentMap) {
					return true
				}
			}
		}
		child = parent
	}
}

func branchValueGuardForTarget(pass *analysis.Pass, cond ast.Expr, receiverKey string) (bodyNonZero bool, elseNonZero bool, ok bool) {
	bin, ok := stripParens(cond).(*ast.BinaryExpr)
	if !ok || (bin.Op != token.NEQ && bin.Op != token.EQL) {
		return false, false, false
	}
	leftZero := isZeroValueExpr(bin.X)
	rightZero := isZeroValueExpr(bin.Y)
	if leftZero == rightZero {
		return false, false, false
	}
	targetExpr := bin.X
	if leftZero {
		targetExpr = bin.Y
	}
	if targetKeyForExpr(pass, targetExpr) != receiverKey {
		return false, false, false
	}
	if bin.Op == token.NEQ {
		return true, false, true
	}
	return false, true, true
}

func isZeroValueExpr(expr ast.Expr) bool {
	expr = stripParens(expr)
	if isNilIdent(expr) {
		return true
	}
	if lit, ok := expr.(*ast.BasicLit); ok {
		switch lit.Kind {
		case token.STRING:
			return lit.Value == `""`
		case token.INT, token.FLOAT:
			return lit.Value == "0" || lit.Value == "0.0"
		default:
			return false
		}
	}
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "false"
}

func isNilIdent(expr ast.Expr) bool {
	ident, ok := stripParens(expr).(*ast.Ident)
	return ok && ident.Name == "nil"
}

func isDescendantOrSelf(node ast.Node, ancestor ast.Node, parentMap map[ast.Node]ast.Node) bool {
	if node == nil || ancestor == nil || parentMap == nil {
		return false
	}
	current := node
	for current != nil {
		if current == ancestor {
			return true
		}
		current = parentMap[current]
	}
	return false
}

func isWithinIfInit(node ast.Node, parentMap map[ast.Node]ast.Node) bool {
	if node == nil || parentMap == nil {
		return false
	}
	child := node
	for {
		parent := parentMap[child]
		if parent == nil {
			return false
		}
		if ifStmt, ok := parent.(*ast.IfStmt); ok && ifStmt.Init != nil {
			if isDescendantOrSelf(child, ifStmt.Init, parentMap) {
				return true
			}
		}
		child = parent
	}
}

// maxHelperMethodDepth bounds recursion in multi-level helper method
// delegation tracking to prevent pathological cases. Aligned with
// maxTransitiveDepth in constructor-validates for consistency.
const maxHelperMethodDepth = 5

// findHelperMethodDelegations finds receiver.helperMethod() calls in the
// given body, then recursively walks each helper method's body for direct
// field delegation patterns. Writes delegated field names directly into
// the out accumulator. Bounds recursion with a visited set and depth limit.
func findHelperMethodDelegations(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	typeName, recvVarName string,
	visited map[string]bool,
	depth int,
	out map[string]bool,
) {
	if depth >= maxHelperMethodDepth {
		return
	}
	if visited == nil {
		visited = make(map[string]bool)
	}

	// Collect receiver.helperMethod() calls.
	var helperNames []string
	parentMap := buildParentMap(body)
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != recvVarName {
			return true
		}
		if isConditionallyEvaluated(call, parentMap) {
			return true
		}
		// Skip "Validate" itself to avoid infinite recursion.
		if sel.Sel.Name != "Validate" {
			helperNames = append(helperNames, sel.Sel.Name)
		}
		return true
	})

	// For each helper, find its method body and check for delegations.
	for _, helperName := range helperNames {
		if visited[helperName] {
			continue
		}
		visited[helperName] = true

		helperBody, helperRecvVar := findMethodBody(pass, typeName, helperName)
		if helperBody == nil {
			continue
		}

		// Walk the helper for direct receiver.Field.Validate() patterns.
		helperParentMap := buildParentMap(helperBody)
		ast.Inspect(helperBody, func(n ast.Node) bool {
			rangeStmt, ok := n.(*ast.RangeStmt)
			if ok {
				sel, ok := rangeStmt.X.(*ast.SelectorExpr)
				if !ok || helperRecvVar == "" {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok || ident.Name != helperRecvVar {
					return true
				}

				fieldName := sel.Sel.Name
				valueVar := ""
				if vi, ok := rangeStmt.Value.(*ast.Ident); ok {
					valueVar = vi.Name
				}
				keyVar := ""
				if ki, ok := rangeStmt.Key.(*ast.Ident); ok {
					keyVar = ki.Name
				}

				ast.Inspect(rangeStmt.Body, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Validate" {
						return true
					}

					if valueVar != "" {
						if vi, ok := sel.X.(*ast.Ident); ok && vi.Name == valueVar {
							out[fieldName] = true
							return true
						}
					}
					if keyVar != "" {
						if indexExpr, ok := sel.X.(*ast.IndexExpr); ok {
							if innerSel, ok := indexExpr.X.(*ast.SelectorExpr); ok {
								if innerIdent, ok := innerSel.X.(*ast.Ident); ok &&
									innerIdent.Name == helperRecvVar &&
									innerSel.Sel.Name == fieldName {
									if ki, ok := indexExpr.Index.(*ast.Ident); ok && ki.Name == keyVar {
										out[fieldName] = true
									}
								}
							}
						}
					}
					return true
				})
				return true
			}

			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isConditionallyEvaluated(call, helperParentMap) &&
				!isWithinIfInit(call, helperParentMap) &&
				!isGuardedValidateCall(pass, call, helperParentMap) {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Validate" {
				return true
			}
			innerSel, ok := sel.X.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if helperRecvVar != "" {
				if id, ok := innerSel.X.(*ast.Ident); ok && id.Name == helperRecvVar {
					out[innerSel.Sel.Name] = true
				}
			}
			return true
		})

		// Recurse: check if this helper calls further helpers that
		// contain field delegations.
		findHelperMethodDelegations(pass, helperBody, typeName, helperRecvVar, visited, depth+1, out)
	}
}

func findHelperFunctionDelegations(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	recvVarName string,
	aliasBindings map[string][]delegationAliasBindingEvent,
	parentMap map[ast.Node]ast.Node,
	out map[string]bool,
) {
	if pass == nil || pass.TypesInfo == nil || body == nil || recvVarName == "" {
		return
	}

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isConditionallyEvaluated(call, parentMap) && !isWithinIfInit(call, parentMap) {
			return true
		}

		helperName := calledFunctionName(call.Fun)
		if helperName == "" {
			return true
		}

		helperBody, paramNames := findFunctionBody(pass, helperName)
		if helperBody == nil {
			return true
		}

		delegatedParams := collectDelegatedHelperParams(helperBody, paramNames)
		for _, paramIndex := range delegatedParams {
			if paramIndex >= len(call.Args) {
				continue
			}
			fieldName, ok := delegatedFieldNameForArg(pass, call.Args[paramIndex], recvVarName, aliasBindings, call.Pos())
			if ok {
				out[fieldName] = true
			}
		}
		return true
	})
}

func calledFunctionName(expr ast.Expr) string {
	switch node := stripParens(expr).(type) {
	case *ast.Ident:
		return node.Name
	case *ast.IndexExpr:
		if ident, ok := stripParens(node.X).(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.IndexListExpr:
		if ident, ok := stripParens(node.X).(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

func collectDelegatedHelperParams(body *ast.BlockStmt, paramNames []string) []int {
	if body == nil || len(paramNames) == 0 {
		return nil
	}

	paramIndexes := make(map[string]int, len(paramNames))
	for i, name := range paramNames {
		paramIndexes[name] = i
	}

	delegated := make(map[int]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Validate" {
				return true
			}
			if ident, ok := stripParens(sel.X).(*ast.Ident); ok {
				if index, ok := paramIndexes[ident.Name]; ok {
					delegated[index] = true
				}
			}
			if indexExpr, ok := stripParens(sel.X).(*ast.IndexExpr); ok {
				if ident, ok := stripParens(indexExpr.X).(*ast.Ident); ok {
					if index, ok := paramIndexes[ident.Name]; ok {
						delegated[index] = true
					}
				}
			}
		case *ast.RangeStmt:
			rangeIdent, ok := stripParens(node.X).(*ast.Ident)
			if !ok {
				return true
			}
			paramIndex, ok := paramIndexes[rangeIdent.Name]
			if !ok {
				return true
			}

			valueVar := ""
			if ident, ok := node.Value.(*ast.Ident); ok {
				valueVar = ident.Name
			}
			keyVar := ""
			if ident, ok := node.Key.(*ast.Ident); ok {
				keyVar = ident.Name
			}

			ast.Inspect(node.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Validate" {
					return true
				}
				if valueVar != "" {
					if ident, ok := stripParens(sel.X).(*ast.Ident); ok && ident.Name == valueVar {
						delegated[paramIndex] = true
					}
				}
				if keyVar != "" {
					if indexExpr, ok := stripParens(sel.X).(*ast.IndexExpr); ok {
						if ident, ok := stripParens(indexExpr.X).(*ast.Ident); ok && ident.Name == rangeIdent.Name {
							if keyIdent, ok := stripParens(indexExpr.Index).(*ast.Ident); ok && keyIdent.Name == keyVar {
								delegated[paramIndex] = true
							}
						}
					}
				}
				return true
			})
		}
		return true
	})

	var result []int
	for i := range len(paramNames) {
		if delegated[i] {
			result = append(result, i)
		}
	}
	return result
}

func delegatedFieldNameForArg(
	pass *analysis.Pass,
	arg ast.Expr,
	recvVarName string,
	aliasBindings map[string][]delegationAliasBindingEvent,
	atPos token.Pos,
) (string, bool) {
	if recvVarName == "" {
		return "", false
	}

	if sel, ok := stripParens(arg).(*ast.SelectorExpr); ok {
		if ident, ok := stripParens(sel.X).(*ast.Ident); ok && ident.Name == recvVarName {
			return sel.Sel.Name, true
		}
	}

	aliasKey := targetKeyForExpr(pass, stripParens(arg))
	if aliasKey == "" {
		return "", false
	}
	return latestDelegationAliasFieldBefore(aliasBindings[aliasKey], atPos)
}

// findMethodBody searches the package for a method with the given receiver
// type and name. Returns the body and the receiver variable name.
func findMethodBody(pass *analysis.Pass, typeName, methodName string) (*ast.BlockStmt, string) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			recvName := receiverTypeName(fn.Recv.List[0].Type)
			if recvName != typeName || fn.Name.Name != methodName {
				continue
			}
			recvVar := ""
			if len(fn.Recv.List[0].Names) > 0 {
				recvVar = fn.Recv.List[0].Names[0].Name
			}
			return fn.Body, recvVar
		}
	}
	return nil, ""
}

func findFunctionBody(pass *analysis.Pass, funcName string) (*ast.BlockStmt, []string) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || fn.Name.Name != funcName {
				continue
			}
			return fn.Body, functionParamNames(fn.Type.Params)
		}
	}
	return nil, nil
}

func functionParamNames(fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}

	var names []string
	for _, field := range fields.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

// embeddedFieldTypeName extracts the type name from an anonymous embedded
// field's type expression. Returns the simple type name for same-package
// types (*ast.Ident), qualified name for imported types (*ast.SelectorExpr),
// and handles pointer embeds (*ast.StarExpr). Returns "" if unrecognized.
func embeddedFieldTypeName(expr ast.Expr) string {
	// Unwrap pointer: *Name → Name
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		// Imported type: pkg.Name — the field key is just Name.
		return e.Sel.Name
	}
	return ""
}
