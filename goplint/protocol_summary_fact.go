// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

const (
	protocolSummaryFactVersion               uint32 = 5
	protocolSummaryEffectPure                       = "pure"
	protocolSummaryEffectPreserve                   = "preserve"
	protocolSummaryEffectValidate                   = "conditional-validation"
	protocolSummaryEffectMutate                     = "mutation"
	protocolSummaryEffectReplace                    = "replacement"
	protocolSummaryEffectEscape                     = "escape"
	protocolSummaryEffectConsume                    = "consume"
	protocolSummaryEffectTerminal                   = "terminal"
	protocolSummaryTargetReceiver                   = "receiver"
	protocolSummaryTargetParameter                  = "parameter"
	protocolSummaryTargetResult                     = "result"
	protocolSummaryConditionResultNil               = "result-nil"
	protocolSummaryConditionSuccessfulReturn        = "successful-return"
)

type protocolSummaryFactStatus uint8

const (
	protocolSummaryFactValid protocolSummaryFactStatus = iota
	protocolSummaryFactMissing
	protocolSummaryFactIncompatible
)

// ProtocolSummaryEffectFact describes one slot-sensitive conditional protocol effect.
type ProtocolSummaryEffectFact struct {
	Kind                string
	TargetKind          string
	TargetSlot          int
	SourceKind          string
	SourceSlot          int
	ConditionResultSlot int
	Condition           string
}

// ProtocolSummaryFact is a versioned, package-qualified interprocedural validation summary.
type ProtocolSummaryFact struct {
	FormatVersion    uint32
	PackagePath      string
	FunctionName     string
	FunctionIdentity string
	Complete         bool
	Effects          []ProtocolSummaryEffectFact
}

// AFact marks ProtocolSummaryFact as an analysis fact.
func (*ProtocolSummaryFact) AFact() {}

// String returns deterministic summary provenance for analysis diagnostics.
func (f *ProtocolSummaryFact) String() string {
	if f == nil {
		return "protocol-summary:<nil>"
	}
	return fmt.Sprintf(
		"protocol-summary:v%d:%s:%s:%d",
		f.FormatVersion,
		f.PackagePath,
		f.FunctionIdentity,
		len(f.Effects),
	)
}

func exportProtocolSummaryFacts(pass *analysis.Pass, ssaRes *ssaResult) {
	if pass == nil || !ssaRes.availability().ready() {
		return
	}
	mayReturn := computeProtocolMayReturn(pass, noReturnCallResolver{ssa: ssaRes})
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name == nil {
				continue
			}
			object, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
			if !ok || !object.Exported() || protocolSummaryExcludedFunctionName(object.Name()) {
				continue
			}
			resolution := resolveSSAFunction(ssaRes, object)
			if !resolution.Availability.ready() {
				continue
			}
			fact := buildCompleteProtocolSummaryFact(pass, object, resolution.Function, mayReturn)
			if fact.Complete && len(fact.Effects) > 0 {
				pass.ExportObjectFact(object, &fact)
			}
		}
	}
}

func buildCompleteProtocolSummaryFact(
	pass *analysis.Pass,
	function *types.Func,
	ssaFunction *ssa.Function,
	mayReturn map[string]bool,
) ProtocolSummaryFact {
	fact := buildProtocolSummaryFact(pass.Pkg.Path(), function.Name(), ssaFunction)
	inputEffects := append([]ProtocolSummaryEffectFact(nil), fact.Effects...)
	resultEffects := fact.Effects[:0]
	for _, effect := range fact.Effects {
		if effect.TargetKind == protocolSummaryTargetResult {
			resultEffects = append(resultEffects, effect)
		}
	}
	fact.Effects = resultEffects
	slots := protocolSummaryRelevantInputSlots(function)
	if len(slots) > 0 {
		if returns, known := mayReturn[objectKey(function)]; known && !returns {
			fact.Effects = appendUniqueProtocolSummaryEffect(
				fact.Effects,
				newProtocolCallSummaryEffect(protocolSummaryEffectTerminal),
			)
			return fact
		}
	}

	declaration := findFuncDeclForObject(pass, function)
	if declaration == nil || declaration.Body == nil || !protocolSummaryBodyIsStraightLine(declaration.Body) {
		if len(slots) > 0 {
			fact.Effects = nil
			fact.Complete = false
		} else if len(fact.Effects) == 0 {
			fact.Complete = false
		}
		return fact
	}
	for _, slot := range slots {
		effects, complete := buildStraightLineProtocolSummaryForSlot(
			pass,
			declaration,
			slot,
			inputEffects,
		)
		if !complete {
			fact.Complete = false
			fact.Effects = nil
			return fact
		}
		for _, effect := range effects {
			fact.Effects = appendUniqueProtocolSummaryEffect(fact.Effects, effect)
		}
	}
	return fact
}

func protocolSummaryBodyIsStraightLine(body *ast.BlockStmt) bool {
	straightLine := body != nil
	ast.Inspect(body, func(node ast.Node) bool {
		if !straightLine {
			return false
		}
		switch node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
			*ast.TypeSwitchStmt, *ast.SelectStmt, *ast.GoStmt, *ast.DeferStmt,
			*ast.FuncLit:
			straightLine = false
			return false
		default:
			return true
		}
	})
	return straightLine
}

func buildStraightLineProtocolSummaryForSlot(
	pass *analysis.Pass,
	declaration *ast.FuncDecl,
	slot calleeTargetSlot,
	ssaEffects []ProtocolSummaryEffectFact,
) ([]ProtocolSummaryEffectFact, bool) {
	target, ok := functionTargetForSlot(pass, declaration, slot)
	if !ok {
		return nil, false
	}
	targetKind, targetSlot := protocolSummaryTargetForCalleeSlot(slot)
	positioned := make([]positionedSummaryEffect, 0, 4)
	for _, effect := range ssaEffects {
		if effect.TargetKind != targetKind || effect.TargetSlot != targetSlot {
			continue
		}
		positioned = append(positioned, positionedSummaryEffect{
			position: firstTargetValidationPosition(pass, declaration.Body, target, nil),
			effect:   effect,
		})
	}
	if mutation, position, found := targetMutationSummaryEffect(pass, declaration, slot, target); found {
		positioned = append(positioned, positionedSummaryEffect{position: position, effect: mutation})
	}
	if position := targetReplacementSourcePosition(pass, declaration, slot, target); position.IsValid() {
		positioned = append(positioned, positionedSummaryEffect{
			position: position,
			effect:   newProtocolTargetSummaryEffect(protocolSummaryEffectPreserve, targetKind, targetSlot),
		})
	}
	if targetOnlyDiscardedInBody(pass, declaration.Body, target) {
		positioned = append(positioned, positionedSummaryEffect{
			position: declaration.Body.Pos(),
			effect:   newProtocolTargetSummaryEffect(protocolSummaryEffectPreserve, targetKind, targetSlot),
		})
	} else if position := firstTargetConsumptionPosition(pass, declaration.Body, target); position.IsValid() {
		positioned = append(positioned, positionedSummaryEffect{
			position: position,
			effect:   newProtocolTargetSummaryEffect(protocolSummaryEffectConsume, targetKind, targetSlot),
		})
	} else if position := targetPackageEscapePosition(pass, declaration.Body, target); position.IsValid() {
		positioned = append(positioned, positionedSummaryEffect{
			position: position,
			effect:   newProtocolTargetSummaryEffect(protocolSummaryEffectEscape, targetKind, targetSlot),
		})
	}
	if len(positioned) == 0 {
		if targetReferencedInBody(pass, declaration.Body, target) || !functionBodyIsObviouslyPure(declaration.Body) {
			return nil, false
		}
		positioned = append(positioned, positionedSummaryEffect{
			position: declaration.Body.Pos(),
			effect:   newProtocolCallSummaryEffect(protocolSummaryEffectPure),
		})
	}
	sort.SliceStable(positioned, func(left, right int) bool {
		return positioned[left].position < positioned[right].position
	})
	effects := make([]ProtocolSummaryEffectFact, 0, len(positioned))
	for _, entry := range positioned {
		effects = append(effects, entry.effect)
	}
	return effects, true
}

func targetReplacementSourcePosition(
	pass *analysis.Pass,
	fnDecl *ast.FuncDecl,
	sourceSlot calleeTargetSlot,
	source castTarget,
) token.Pos {
	if pass == nil || fnDecl == nil || fnDecl.Body == nil {
		return token.NoPos
	}
	position := token.NoPos
	ast.Inspect(fnDecl.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for index, rhs := range assignment.Rhs {
			if index >= len(assignment.Lhs) || !source.matchesExpr(pass, rhs) {
				continue
			}
			for _, targetSlot := range allFunctionTargetSlots(fnDecl) {
				if targetSlot == sourceSlot {
					continue
				}
				target, targetOK := functionTargetForSlot(pass, fnDecl, targetSlot)
				if targetOK && writesThroughTarget(pass, assignment.Lhs[index], target) {
					position = assignment.Pos()
					return false
				}
			}
		}
		return true
	})
	return position
}

func allFunctionTargetSlots(fnDecl *ast.FuncDecl) []calleeTargetSlot {
	if fnDecl == nil || fnDecl.Type == nil {
		return nil
	}
	var slots []calleeTargetSlot
	if fnDecl.Recv != nil && len(fnDecl.Recv.List) > 0 && len(fnDecl.Recv.List[0].Names) > 0 {
		slots = append(slots, calleeTargetSlot{kind: calleeTargetSlotReceiver})
	}
	if fnDecl.Type.Params == nil {
		return slots
	}
	parameterIndex := 0
	for _, field := range fnDecl.Type.Params.List {
		for range field.Names {
			slots = append(slots, calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: parameterIndex})
			parameterIndex++
		}
	}
	return slots
}

func targetPackageEscapePosition(pass *analysis.Pass, body *ast.BlockStmt, target castTarget) token.Pos {
	position := token.NoPos
	ast.Inspect(body, func(node ast.Node) bool {
		if position.IsValid() {
			return false
		}
		if nodeEscapesTargetToPackageState(pass, node, target) {
			position = node.Pos()
			return false
		}
		return true
	})
	return position
}

func protocolSummaryExcludedFunctionName(name string) bool {
	switch name {
	case validateMethodName, "String", "Error", "GoString":
		return true
	default:
		return false
	}
}

func protocolSummaryRelevantInputSlots(function *types.Func) []calleeTargetSlot {
	if function == nil {
		return nil
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok {
		return nil
	}
	slots := make([]calleeTargetSlot, 0, signature.Params().Len()+1)
	if receiver := signature.Recv(); receiver != nil && protocolTypeHasValidateMethod(receiver.Type()) {
		slots = append(slots, calleeTargetSlot{kind: calleeTargetSlotReceiver})
	}
	for index := range signature.Params().Len() {
		if protocolTypeHasValidateMethod(signature.Params().At(index).Type()) {
			slots = append(slots, calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: index})
		}
	}
	return slots
}

func protocolTypeHasValidateMethod(typ types.Type) bool {
	if typ == nil {
		return false
	}
	selection := types.NewMethodSet(typ).Lookup(nil, validateMethodName)
	if selection == nil {
		selection = types.NewMethodSet(types.NewPointer(typ)).Lookup(nil, validateMethodName)
	}
	if selection == nil {
		return false
	}
	function, ok := selection.Obj().(*types.Func)
	return ok && protocolValidateSignature(function)
}

func appendUniqueProtocolSummaryEffect(
	effects []ProtocolSummaryEffectFact,
	effect ProtocolSummaryEffectFact,
) []ProtocolSummaryEffectFact {
	if slices.Contains(effects, effect) {
		return effects
	}
	return append(effects, effect)
}

func buildProtocolSummaryFact(packagePath, functionName string, fn *ssa.Function) ProtocolSummaryFact {
	fact := ProtocolSummaryFact{
		FormatVersion:    protocolSummaryFactVersion,
		PackagePath:      packagePath,
		FunctionName:     functionName,
		FunctionIdentity: protocolSSAFunctionIdentity(fn, packagePath, functionName),
		Complete:         fn != nil,
	}
	if fn == nil {
		return fact
	}
	interner := newProtocolIdentityInterner()
	for _, invocation := range collectProtocolValidationInvocations(fn, interner) {
		for resultSlot := range fn.Signature.Results().Len() {
			if !protocolFunctionAlwaysReturnsValueFromSlot(fn, invocation.Result, resultSlot) {
				continue
			}
			if targetKind, targetSlot, ok := protocolSummaryInputSlot(fn, invocation.Receiver); ok {
				fact.Effects = append(fact.Effects, newProtocolSummaryEffect(targetKind, targetSlot, resultSlot))
			}
			for targetSlot := range fn.Signature.Results().Len() {
				if targetSlot == resultSlot || !protocolFunctionAlwaysReturnsValueFromSlot(fn, invocation.Receiver, targetSlot) {
					continue
				}
				fact.Effects = append(fact.Effects, newProtocolSummaryEffect(protocolSummaryTargetResult, targetSlot, resultSlot))
			}
		}
	}
	return fact
}

func newProtocolSummaryEffect(targetKind string, targetSlot, conditionResultSlot int) ProtocolSummaryEffectFact {
	return ProtocolSummaryEffectFact{
		Kind:                protocolSummaryEffectValidate,
		TargetKind:          targetKind,
		TargetSlot:          targetSlot,
		SourceSlot:          -1,
		ConditionResultSlot: conditionResultSlot,
		Condition:           protocolSummaryConditionResultNil,
	}
}

func newProtocolSuccessfulReturnSummaryEffect(targetKind string, targetSlot int) ProtocolSummaryEffectFact {
	return ProtocolSummaryEffectFact{
		Kind:                protocolSummaryEffectValidate,
		TargetKind:          targetKind,
		TargetSlot:          targetSlot,
		SourceSlot:          -1,
		ConditionResultSlot: -1,
		Condition:           protocolSummaryConditionSuccessfulReturn,
	}
}

func newProtocolTargetSummaryEffect(kind, targetKind string, targetSlot int) ProtocolSummaryEffectFact {
	return ProtocolSummaryEffectFact{
		Kind:                kind,
		TargetKind:          targetKind,
		TargetSlot:          targetSlot,
		SourceSlot:          -1,
		ConditionResultSlot: -1,
	}
}

func newProtocolCallSummaryEffect(kind string) ProtocolSummaryEffectFact {
	return ProtocolSummaryEffectFact{
		Kind:                kind,
		TargetSlot:          -1,
		SourceSlot:          -1,
		ConditionResultSlot: -1,
	}
}

func protocolSummaryInputSlot(fn *ssa.Function, receiver ssa.Value) (string, int, bool) {
	if fn == nil || receiver == nil {
		return "", 0, false
	}
	for idx, parameter := range fn.Params {
		if parameter != receiver {
			continue
		}
		if idx == 0 && fn.Signature != nil && fn.Signature.Recv() != nil {
			return protocolSummaryTargetReceiver, 0, true
		}
		parameterSlot := idx
		if fn.Signature != nil && fn.Signature.Recv() != nil {
			parameterSlot--
		}
		return protocolSummaryTargetParameter, parameterSlot, true
	}
	return "", 0, false
}

func protocolFunctionAlwaysReturnsValueFromSlot(fn *ssa.Function, source ssa.Value, resultSlot int) bool {
	foundReturn := false
	for _, block := range fn.Blocks {
		for _, instruction := range block.Instrs {
			ret, ok := instruction.(*ssa.Return)
			if !ok {
				continue
			}
			foundReturn = true
			if resultSlot < 0 || resultSlot >= len(ret.Results) ||
				!protocolValueDerivedFrom(ret.Results[resultSlot], source, make(map[ssa.Value]bool)) {
				return false
			}
		}
	}
	return foundReturn
}

func protocolValueDerivedFrom(candidate, source ssa.Value, seen map[ssa.Value]bool) bool {
	if candidate == nil || source == nil || seen[candidate] {
		return false
	}
	if candidate == source {
		return true
	}
	seen[candidate] = true
	switch typed := candidate.(type) {
	case *ssa.Phi:
		if len(typed.Edges) == 0 {
			return false
		}
		for _, edge := range typed.Edges {
			if !protocolValueDerivedFrom(edge, source, maps.Clone(seen)) {
				return false
			}
		}
		return true
	case *ssa.ChangeType:
		return protocolValueDerivedFrom(typed.X, source, seen)
	case *ssa.ChangeInterface:
		return protocolValueDerivedFrom(typed.X, source, seen)
	case *ssa.MakeInterface:
		return protocolValueDerivedFrom(typed.X, source, seen)
	default:
		return false
	}
}

func validateProtocolSummaryFactShape(fact *ProtocolSummaryFact, packagePath string) protocolSummaryFactStatus {
	if fact == nil {
		return protocolSummaryFactMissing
	}
	if fact.FormatVersion != protocolSummaryFactVersion || !fact.Complete ||
		strings.TrimSpace(fact.PackagePath) == "" || fact.PackagePath != packagePath ||
		strings.TrimSpace(fact.FunctionName) == "" ||
		strings.TrimSpace(fact.FunctionIdentity) == "" {
		return protocolSummaryFactIncompatible
	}
	for _, effect := range fact.Effects {
		if !validProtocolSummaryEffect(effect) {
			return protocolSummaryFactIncompatible
		}
	}
	return protocolSummaryFactValid
}

func validateProtocolSummaryFact(fact *ProtocolSummaryFact, function *types.Func) protocolSummaryFactStatus {
	if fact == nil {
		return protocolSummaryFactMissing
	}
	if function == nil || function.Pkg() == nil ||
		validateProtocolSummaryFactShape(fact, function.Pkg().Path()) != protocolSummaryFactValid ||
		fact.FunctionName != function.Name() ||
		fact.FunctionIdentity != protocolFunctionIdentity(function) {
		return protocolSummaryFactIncompatible
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature == nil {
		return protocolSummaryFactIncompatible
	}
	for _, effect := range fact.Effects {
		if !validProtocolSummaryEffectForFunction(effect, signature) {
			return protocolSummaryFactIncompatible
		}
	}
	return protocolSummaryFactValid
}

func protocolSSAFunctionIdentity(
	function *ssa.Function,
	packagePath,
	functionName string,
) string {
	if function != nil {
		if object, ok := function.Object().(*types.Func); ok {
			if identity := protocolFunctionIdentity(object); identity != "" {
				return identity
			}
		}
	}
	if strings.TrimSpace(packagePath) == "" || strings.TrimSpace(functionName) == "" {
		return ""
	}
	return packagePath + "." + functionName
}

func protocolFunctionIdentity(function *types.Func) string {
	if function == nil || function.Pkg() == nil {
		return ""
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature == nil {
		return ""
	}
	qualifier := func(pkg *types.Package) string {
		if pkg == nil {
			return ""
		}
		return pkg.Path()
	}
	if receiver := signature.Recv(); receiver != nil {
		return "(" + types.TypeString(receiver.Type(), qualifier) + ")." + function.Name()
	}
	return function.Pkg().Path() + "." + function.Name()
}

func validProtocolSummaryEffectForFunction(effect ProtocolSummaryEffectFact, signature *types.Signature) bool {
	if signature == nil || !validProtocolSummaryEffect(effect) {
		return false
	}
	if effect.Kind == protocolSummaryEffectPure || effect.Kind == protocolSummaryEffectTerminal {
		return true
	}
	targetType, targetOK := protocolSummarySlotType(signature, effect.TargetKind, effect.TargetSlot)
	if !targetOK || !hasValidateMethod(targetType) {
		return false
	}
	if effect.Kind == protocolSummaryEffectReplace {
		sourceType, sourceOK := protocolSummarySlotType(signature, effect.SourceKind, effect.SourceSlot)
		if !sourceOK || !types.AssignableTo(sourceType, targetType) {
			return false
		}
	}
	if effect.Kind == protocolSummaryEffectValidate && effect.Condition == protocolSummaryConditionResultNil {
		conditionType, conditionOK := protocolSummarySlotType(
			signature,
			protocolSummaryTargetResult,
			effect.ConditionResultSlot,
		)
		if !conditionOK || !isErrorType(conditionType) {
			return false
		}
	}
	return true
}

func protocolSummarySlotType(signature *types.Signature, kind string, slot int) (types.Type, bool) {
	if signature == nil || slot < 0 {
		return nil, false
	}
	switch kind {
	case protocolSummaryTargetReceiver:
		if slot != 0 || signature.Recv() == nil {
			return nil, false
		}
		return signature.Recv().Type(), true
	case protocolSummaryTargetParameter:
		if signature.Params() == nil || slot >= signature.Params().Len() {
			return nil, false
		}
		return signature.Params().At(slot).Type(), true
	case protocolSummaryTargetResult:
		if signature.Results() == nil || slot >= signature.Results().Len() {
			return nil, false
		}
		return signature.Results().At(slot).Type(), true
	default:
		return nil, false
	}
}

func validProtocolSummaryEffect(effect ProtocolSummaryEffectFact) bool {
	switch effect.Kind {
	case protocolSummaryEffectPure, protocolSummaryEffectTerminal:
		return effect.TargetKind == "" && effect.TargetSlot == -1 &&
			effect.SourceKind == "" && effect.SourceSlot == -1 &&
			effect.ConditionResultSlot == -1 && effect.Condition == ""
	case protocolSummaryEffectPreserve, protocolSummaryEffectMutate,
		protocolSummaryEffectEscape, protocolSummaryEffectConsume:
		return validProtocolSummaryTarget(effect.TargetKind, effect.TargetSlot) &&
			effect.SourceKind == "" && effect.SourceSlot == -1 &&
			effect.ConditionResultSlot == -1 && effect.Condition == ""
	case protocolSummaryEffectReplace:
		return validProtocolSummaryTarget(effect.TargetKind, effect.TargetSlot) &&
			validProtocolSummaryTarget(effect.SourceKind, effect.SourceSlot) &&
			effect.ConditionResultSlot == -1 && effect.Condition == ""
	case protocolSummaryEffectValidate:
		return validProtocolSummaryTarget(effect.TargetKind, effect.TargetSlot) &&
			effect.SourceKind == "" && effect.SourceSlot == -1 && ((effect.ConditionResultSlot >= 0 && effect.Condition == protocolSummaryConditionResultNil) ||
			(effect.ConditionResultSlot == -1 && effect.Condition == protocolSummaryConditionSuccessfulReturn))
	default:
		return false
	}
}

func validProtocolSummaryTarget(kind string, slot int) bool {
	return (kind == protocolSummaryTargetReceiver || kind == protocolSummaryTargetParameter ||
		kind == protocolSummaryTargetResult) && slot >= 0
}
