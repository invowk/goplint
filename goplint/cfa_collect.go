// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// cfaAssignedCast records a type conversion assigned to a named variable,
// along with its containing AssignStmt for CFG lookup.
type cfaAssignedCast struct {
	target    castTarget
	typeName  string
	pos       ast.Node
	assign    ast.Node // AssignStmt or ValueSpec containing this cast
	castIndex int
}

// cfaUnassignedCast records a type conversion not assigned to a named
// variable (e.g., return, function argument, blank identifier).
type cfaUnassignedCast struct {
	typeName  string
	pos       ast.Node
	castIndex int
}

// cfaClosureHandler is called when the cast-collection walk encounters a
// FuncLit. Implementations decide how to handle the closure (e.g.,
// delegate to inspectClosureCastsCFA or recurse for nested closures).
// Returning false from the outer walk callback prevents descent into
// the closure body.
type cfaClosureHandler func(lit *ast.FuncLit, closureIdx int)

type closureInvocationKind int

const (
	closureInvocationDirect closureInvocationKind = iota
	closureInvocationDefer
	closureInvocationGo
)

type closureVarCall struct {
	call ast.Node
	lit  *ast.FuncLit
	kind closureInvocationKind
}

type closureBindingEvent struct {
	pos token.Pos
	lit *ast.FuncLit
}

type validateMethodValueBindingEvent struct {
	pos          token.Pos
	receiver     ast.Expr
	isMethodExpr bool
}

type methodValueValidateCall struct {
	call     *ast.CallExpr
	receiver ast.Expr
}

// collectCFACasts walks a function or closure body and classifies type
// conversions from raw primitives to DDD Value Types into assigned and
// unassigned casts. Executable closures (IIFEs, go/defer closure calls)
// are delegated to the provided handler rather than being analyzed inline.
// Non-executable literals (for example, detached func values) are skipped.
//
// This is the shared cast-collection logic used by both
// inspectUnvalidatedCastsCFA (outer functions) and
// inspectClosureCastsCFA (closure bodies). The walk root is always
// body (*ast.BlockStmt), so the parent *ast.FuncLit is never visited.
func collectCFACasts(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	parentMap map[ast.Node]ast.Node,
	onClosure cfaClosureHandler,
) ([]cfaAssignedCast, []cfaUnassignedCast, []closureVarCall, []methodValueValidateCall) {
	var assignedCasts []cfaAssignedCast
	var unassignedCasts []cfaUnassignedCast
	var closureCalls []closureVarCall
	var methodValueCalls []methodValueValidateCall
	castIndex := 0
	closureIndex := 0
	closureVarBindings := collectClosureVarBindingEvents(pass, body)
	validateMethodBindings := collectValidateMethodValueBindingEvents(pass, body)
	analyzedClosures := make(map[*ast.FuncLit]bool)

	analyzeClosure := func(lit *ast.FuncLit) {
		if lit == nil || analyzedClosures[lit] {
			return
		}
		analyzedClosures[lit] = true
		onClosure(lit, closureIndex)
		closureIndex++
	}

	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			if isExecutableClosureLiteral(lit, parentMap) {
				analyzeClosure(lit)
			}
			return false
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if lit, kind, ok := executableClosureVarCall(pass, call, closureVarBindings, parentMap); ok {
			analyzeClosure(lit)
			closureCalls = append(closureCalls, closureVarCall{
				call: call,
				lit:  lit,
				kind: kind,
			})
		}
		if receiver, ok := validateMethodReceiverForCall(pass, call, validateMethodBindings); ok {
			methodValueCalls = append(methodValueCalls, methodValueValidateCall{
				call:     call,
				receiver: receiver,
			})
		}

		// Not a type conversion — skip.
		tv, ok := pass.TypesInfo.Types[call.Fun]
		if !ok || !tv.IsType() {
			return true
		}

		if len(call.Args) != 1 {
			return true
		}

		// Target must have Validate() — i.e., it's a DDD Value Type.
		targetType := tv.Type
		if !hasValidateMethod(targetType) {
			return true
		}

		// Source must be a raw primitive from a runtime expression.
		srcTV, srcOK := pass.TypesInfo.Types[call.Args[0]]
		if !srcOK {
			return true
		}
		if srcTV.Value != nil {
			return true // constant expression — skip
		}
		if isErrorMessageExpr(pass, call.Args[0]) {
			return true // error-message source — skip
		}
		if !isRawPrimitive(srcTV.Type) {
			return true // named-to-named cast — skip
		}

		targetTypeName := qualifiedTypeName(targetType, pass.Pkg)
		parent := parentMap[call]

		target, assignNode, assigned := resolveCastAssignmentTarget(pass, call, parentMap)
		if assigned {
			assignedCasts = append(assignedCasts, cfaAssignedCast{
				target:    target,
				typeName:  targetTypeName,
				pos:       call,
				assign:    assignNode,
				castIndex: castIndex,
			})
			castIndex++
			return true
		}

		// Unassigned cast — check auto-skip contexts.
		if isAutoSkipContext(pass, call, parent, parentMap) {
			return true
		}

		unassignedCasts = append(unassignedCasts, cfaUnassignedCast{
			typeName:  targetTypeName,
			pos:       call,
			castIndex: castIndex,
		})
		castIndex++
		return true
	})

	return assignedCasts, unassignedCasts, closureCalls, methodValueCalls
}

// isExecutableClosureLiteral reports whether lit is directly invoked in-place:
// func() { ... }(), go func() { ... }(), or defer func() { ... }().
func isExecutableClosureLiteral(lit *ast.FuncLit, parentMap map[ast.Node]ast.Node) bool {
	if lit == nil || parentMap == nil {
		return false
	}
	call, ok := closureLiteralCall(lit, parentMap)
	if !ok {
		return false
	}
	return stripParens(call.Fun) == lit
}

// closureLiteralCall returns the CallExpr that invokes lit, allowing any number
// of parenthesized wrappers between the literal and the call expression.
func closureLiteralCall(lit *ast.FuncLit, parentMap map[ast.Node]ast.Node) (*ast.CallExpr, bool) {
	if lit == nil || parentMap == nil {
		return nil, false
	}
	current := ast.Node(lit)
	for {
		parent := parentMap[current]
		if parent == nil {
			return nil, false
		}
		if paren, ok := parent.(*ast.ParenExpr); ok && paren.X == current {
			current = paren
			continue
		}
		call, ok := parent.(*ast.CallExpr)
		if !ok {
			return nil, false
		}
		return call, true
	}
}

func collectClosureVarBindingEvents(pass *analysis.Pass, body *ast.BlockStmt) map[string][]closureBindingEvent {
	if pass == nil || pass.TypesInfo == nil || body == nil {
		return nil
	}
	bindings := make(map[string][]closureBindingEvent)

	resolveBinding := func(lhs *ast.Ident, rhs ast.Expr, atPos token.Pos) (string, closureBindingEvent, bool) {
		if lhs == nil || lhs.Name == "_" {
			return "", closureBindingEvent{}, false
		}
		obj := objectForIdent(pass, lhs)
		if obj == nil {
			return "", closureBindingEvent{}, false
		}
		if _, isVar := obj.(*types.Var); !isVar {
			return "", closureBindingEvent{}, false
		}
		lit, ok := exprFuncLit(rhs)
		if !ok {
			if rhsIdent, rhsOK := stripParens(rhs).(*ast.Ident); rhsOK {
				rhsObj := objectForIdent(pass, rhsIdent)
				if rhsObj != nil {
					if matched, aliasOK := latestClosureBindingBefore(bindings[objectKey(rhsObj)], atPos); aliasOK {
						lit = matched
						ok = true
					}
				}
			}
		}
		key := objectKey(obj)
		event := closureBindingEvent{pos: atPos}
		if ok {
			event.lit = lit
		}
		return key, event, true
	}

	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			type pendingBinding struct {
				key   string
				event closureBindingEvent
			}
			pending := make([]pendingBinding, 0, len(node.Rhs))
			for i, rhs := range node.Rhs {
				if i >= len(node.Lhs) {
					break
				}
				lhsIdent, ok := stripParens(node.Lhs[i]).(*ast.Ident)
				if !ok {
					continue
				}
				key, event, ok := resolveBinding(lhsIdent, rhs, lhsIdent.Pos())
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
				event closureBindingEvent
			}
			pending := make([]pendingBinding, 0, len(node.Values))
			for i, rhs := range node.Values {
				if i >= len(node.Names) {
					break
				}
				key, event, ok := resolveBinding(node.Names[i], rhs, node.Names[i].Pos())
				if !ok {
					continue
				}
				pending = append(pending, pendingBinding{key: key, event: event})
			}
			for _, entry := range pending {
				bindings[entry.key] = append(bindings[entry.key], entry.event)
			}
		}
		return true
	})

	return bindings
}

func latestClosureBindingBefore(events []closureBindingEvent, atPos token.Pos) (*ast.FuncLit, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].pos > atPos {
			continue
		}
		if events[i].lit == nil {
			return nil, false
		}
		return events[i].lit, true
	}
	return nil, false
}

func collectValidateMethodValueBindingEvents(pass *analysis.Pass, body *ast.BlockStmt) map[string][]validateMethodValueBindingEvent {
	if pass == nil || pass.TypesInfo == nil || body == nil {
		return nil
	}
	bindings := make(map[string][]validateMethodValueBindingEvent)

	resolveBinding := func(lhs ast.Expr, rhs ast.Expr, atPos token.Pos) (string, string, validateMethodValueBindingEvent, bool) {
		if lhs == nil {
			return "", "", validateMethodValueBindingEvent{}, false
		}
		lhsExpr := stripParens(lhs)
		if ident, ok := lhsExpr.(*ast.Ident); ok {
			if ident.Name == "_" {
				return "", "", validateMethodValueBindingEvent{}, false
			}
			obj := objectForIdent(pass, ident)
			if obj == nil {
				return "", "", validateMethodValueBindingEvent{}, false
			}
			if _, isVar := obj.(*types.Var); !isVar {
				return "", "", validateMethodValueBindingEvent{}, false
			}
		}
		lhsKey := targetKeyForExpr(pass, lhsExpr)
		if lhsKey == "" {
			return "", "", validateMethodValueBindingEvent{}, false
		}
		receiver, isMethodExpr, ok := validateMethodReceiverFromExpr(pass, rhs)
		if !ok {
			rhsKey := targetKeyForExpr(pass, stripParens(rhs))
			if rhsKey != "" {
				if matched, aliasOK := latestValidateMethodBindingBefore(bindings[rhsKey], atPos); aliasOK {
					receiver = matched.receiver
					isMethodExpr = matched.isMethodExpr
					ok = true
				}
			}
		}

		event := validateMethodValueBindingEvent{pos: atPos}
		if ok {
			event.receiver = receiver
			event.isMethodExpr = isMethodExpr
		}
		fallbackKey := targetKeyForExpr(nil, lhsExpr)
		if fallbackKey == lhsKey {
			fallbackKey = ""
		}
		return lhsKey, fallbackKey, event, true
	}

	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			type pendingBinding struct {
				key         string
				fallbackKey string
				event       validateMethodValueBindingEvent
			}
			pending := make([]pendingBinding, 0, len(node.Rhs))
			for i, rhs := range node.Rhs {
				if i >= len(node.Lhs) {
					break
				}
				key, fallbackKey, event, ok := resolveBinding(node.Lhs[i], rhs, node.Lhs[i].Pos())
				if !ok {
					continue
				}
				pending = append(pending, pendingBinding{
					key:         key,
					fallbackKey: fallbackKey,
					event:       event,
				})
			}
			for _, entry := range pending {
				bindings[entry.key] = append(bindings[entry.key], entry.event)
				if entry.fallbackKey != "" {
					bindings[entry.fallbackKey] = append(bindings[entry.fallbackKey], entry.event)
				}
			}
		case *ast.ValueSpec:
			type pendingBinding struct {
				key         string
				fallbackKey string
				event       validateMethodValueBindingEvent
			}
			pending := make([]pendingBinding, 0, len(node.Values))
			for i, rhs := range node.Values {
				if i >= len(node.Names) {
					break
				}
				key, fallbackKey, event, ok := resolveBinding(node.Names[i], rhs, node.Names[i].Pos())
				if !ok {
					continue
				}
				pending = append(pending, pendingBinding{
					key:         key,
					fallbackKey: fallbackKey,
					event:       event,
				})
			}
			for _, entry := range pending {
				bindings[entry.key] = append(bindings[entry.key], entry.event)
				if entry.fallbackKey != "" {
					bindings[entry.fallbackKey] = append(bindings[entry.fallbackKey], entry.event)
				}
			}
		}
		return true
	})

	return bindings
}

func latestValidateMethodBindingBefore(events []validateMethodValueBindingEvent, atPos token.Pos) (validateMethodValueBindingEvent, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].pos > atPos {
			continue
		}
		if events[i].receiver == nil {
			return validateMethodValueBindingEvent{}, false
		}
		return events[i], true
	}
	return validateMethodValueBindingEvent{}, false
}

func validateMethodReceiverFromExpr(pass *analysis.Pass, expr ast.Expr) (receiver ast.Expr, isMethodExpr bool, ok bool) {
	if pass == nil || pass.TypesInfo == nil || expr == nil {
		return nil, false, false
	}
	sel, ok := stripParens(expr).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != validateMethodName {
		return nil, false, false
	}

	if selection, hasSelection := pass.TypesInfo.Selections[sel]; hasSelection {
		if selection.Obj() == nil || selection.Obj().Name() != validateMethodName {
			return nil, false, false
		}
		switch selection.Kind() {
		case types.MethodVal:
			recvType := pass.TypesInfo.TypeOf(sel.X)
			if recvType == nil || !hasValidateMethod(recvType) {
				return nil, false, false
			}
			return sel.X, false, true
		case types.MethodExpr:
			return sel.X, true, true
		default:
			return nil, false, false
		}
	}

	recvType := pass.TypesInfo.TypeOf(sel.X)
	if recvType == nil || !hasValidateMethod(recvType) {
		return nil, false, false
	}
	return sel.X, false, true
}

func validateMethodReceiverForCall(
	pass *analysis.Pass,
	call *ast.CallExpr,
	bindings map[string][]validateMethodValueBindingEvent,
) (ast.Expr, bool) {
	if pass == nil || pass.TypesInfo == nil || call == nil || len(bindings) == 0 {
		return nil, false
	}
	funExpr := stripParens(call.Fun)
	var key string
	switch fun := funExpr.(type) {
	case *ast.Ident:
		key = targetKeyForExpr(pass, fun)
	case *ast.SelectorExpr:
		key = targetKeyForExpr(pass, fun)
	default:
		return nil, false
	}
	if key == "" {
		return nil, false
	}
	events := bindings[key]
	if len(events) == 0 {
		if fallbackKey := targetKeyForExpr(nil, funExpr); fallbackKey != "" && fallbackKey != key {
			events = bindings[fallbackKey]
		}
	}
	matched, ok := latestValidateMethodBindingBefore(events, call.Pos())
	if !ok {
		return nil, false
	}
	if matched.isMethodExpr {
		if len(call.Args) == 0 {
			return nil, false
		}
		return call.Args[0], true
	}
	return matched.receiver, true
}

func collectMethodValueValidateCalls(pass *analysis.Pass, body *ast.BlockStmt) methodValueValidateCallSet {
	if pass == nil || pass.TypesInfo == nil || body == nil {
		return nil
	}
	bindings := collectValidateMethodValueBindingEvents(pass, body)
	if len(bindings) == 0 {
		return nil
	}
	out := make(methodValueValidateCallSet)
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		receiver, ok := validateMethodReceiverForCall(pass, call, bindings)
		if !ok {
			return true
		}
		out[call] = receiver
		return true
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func exprFuncLit(expr ast.Expr) (*ast.FuncLit, bool) {
	if expr == nil {
		return nil, false
	}
	lit, ok := stripParens(expr).(*ast.FuncLit)
	return lit, ok
}

// executableClosureVarCall reports whether call invokes a local variable bound
// to a function literal (for example, f := func() { ... }; f()).
func executableClosureVarCall(
	pass *analysis.Pass,
	call *ast.CallExpr,
	bindings map[string][]closureBindingEvent,
	parentMap map[ast.Node]ast.Node,
) (*ast.FuncLit, closureInvocationKind, bool) {
	if pass == nil || pass.TypesInfo == nil || call == nil || len(bindings) == 0 {
		return nil, closureInvocationDirect, false
	}
	funIdent, ok := stripParens(call.Fun).(*ast.Ident)
	if !ok {
		return nil, closureInvocationDirect, false
	}
	obj := objectForIdent(pass, funIdent)
	if obj == nil {
		return nil, closureInvocationDirect, false
	}

	events, ok := bindings[objectKey(obj)]
	if !ok || len(events) == 0 {
		return nil, closureInvocationDirect, false
	}

	var matched *ast.FuncLit
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].pos > call.Pos() {
			continue
		}
		matched = events[i].lit
		break
	}
	if matched == nil {
		return nil, closureInvocationDirect, false
	}

	return matched, closureInvocationKindForCall(call, parentMap), true
}

func closureInvocationKindForCall(call *ast.CallExpr, parentMap map[ast.Node]ast.Node) closureInvocationKind {
	if call == nil || parentMap == nil {
		return closureInvocationDirect
	}
	switch parentMap[call].(type) {
	case *ast.GoStmt:
		return closureInvocationGo
	case *ast.DeferStmt:
		return closureInvocationDefer
	default:
		return closureInvocationDirect
	}
}
