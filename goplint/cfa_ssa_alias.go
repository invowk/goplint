// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

// maxAliasSetSize caps the alias set to avoid unbounded growth in functions
// with many copy assignments. Beyond this limit the set is conservatively
// cleared (no aliases recognized).
const maxAliasSetSize = 32

type ssaFlowAliasMatcher struct {
	pass                   *analysis.Pass
	analysis               *protocolAliasAnalysis
	castValue              ssa.Value
	originIdentity         protocolIdentity
	originPointer          bool
	originValues           map[ssa.Value]bool
	closureValues          map[ssa.Value]bool
	ambiguousClosureValues map[ssa.Value]bool
	capturedBindings       map[string]ssa.Value
	debugRefs              map[ast.Expr]*ssa.DebugRef
	debugRefsByObject      map[string][]*ssa.DebugRef
	castStoreAddresses     []ssa.Value
}

func newSSAFlowAliasMatcherForIdentity(
	pass *analysis.Pass,
	ssaFn *ssa.Function,
	analysis *protocolAliasAnalysis,
	origin protocolIdentity,
	originPointer bool,
	originValue ssa.Value,
) *ssaFlowAliasMatcher {
	if ssaFn == nil || analysis == nil || origin == 0 {
		return nil
	}
	matcher := &ssaFlowAliasMatcher{
		pass:                   pass,
		analysis:               analysis,
		originIdentity:         origin,
		originPointer:          originPointer,
		originValues:           map[ssa.Value]bool{originValue: originValue != nil},
		closureValues:          make(map[ssa.Value]bool),
		ambiguousClosureValues: make(map[ssa.Value]bool),
		capturedBindings:       make(map[string]ssa.Value),
		debugRefs:              make(map[ast.Expr]*ssa.DebugRef),
		debugRefsByObject:      make(map[string][]*ssa.DebugRef),
	}
	matcher.collectDebugRefs(ssaFn)
	matcher.collectClosureBindings(ssaFn)
	return matcher
}

func newSSAFlowAliasMatcher(pass *analysis.Pass, ssaFn *ssa.Function, castNode ast.Node) *ssaFlowAliasMatcher {
	castValue := findSSACastValue(ssaFn, castNode)
	if castValue == nil {
		return nil
	}
	interner := newProtocolIdentityInterner()
	// The cast result starts a new protocol identity. ChangeType is normally
	// transparent to alias analysis, but resolving through the cast here would
	// incorrectly make the raw primitive input an alias of the DDD value.
	delete(interner.copySources, interner.internValue(castValue))
	matcher := &ssaFlowAliasMatcher{
		pass:              pass,
		analysis:          analyzeProtocolAliases(ssaFn, interner),
		castValue:         castValue,
		originPointer:     ssaValueHasPointerType(castValue),
		debugRefs:         make(map[ast.Expr]*ssa.DebugRef),
		debugRefsByObject: make(map[string][]*ssa.DebugRef),
	}
	matcher.collectDebugRefs(ssaFn)
	return matcher
}

func (m *ssaFlowAliasMatcher) collectDebugRefs(ssaFn *ssa.Function) {
	if m == nil || ssaFn == nil {
		return
	}
	for _, block := range ssaFn.Blocks {
		for _, instruction := range block.Instrs {
			if store, ok := instruction.(*ssa.Store); ok && m.castValue != nil &&
				protocolValueDerivedFrom(store.Val, m.castValue, make(map[ssa.Value]bool)) {
				m.castStoreAddresses = append(m.castStoreAddresses, store.Addr)
			}
			debugRef, ok := instruction.(*ssa.DebugRef)
			if !ok || debugRef.Expr == nil || debugRef.X == nil {
				continue
			}
			m.debugRefs[stripParens(debugRef.Expr)] = debugRef
			m.debugRefs[stripParensAndStar(debugRef.Expr)] = debugRef
			if key := objectKeyAt(m.pass, debugRef.Object()); key != "" {
				m.debugRefsByObject[key] = append(m.debugRefsByObject[key], debugRef)
			}
		}
	}
}

func (m *ssaFlowAliasMatcher) collectClosureBindings(ssaFn *ssa.Function) {
	if m == nil || m.analysis == nil || m.originIdentity == 0 || ssaFn == nil {
		return
	}
	for _, block := range ssaFn.Blocks {
		for _, instruction := range block.Instrs {
			closure, ok := instruction.(*ssa.MakeClosure)
			if !ok {
				continue
			}
			function, ok := closure.Fn.(*ssa.Function)
			if !ok {
				continue
			}
			for index, binding := range closure.Bindings {
				if index >= len(function.FreeVars) {
					break
				}
				freeVariable := function.FreeVars[index]
				for _, closureBlock := range function.Blocks {
					for _, closureInstruction := range closureBlock.Instrs {
						reference, referenceOK := closureInstruction.(*ssa.DebugRef)
						if !referenceOK || reference.X != freeVariable {
							continue
						}
						if key := objectKeyAt(m.pass, reference.Object()); key != "" {
							m.capturedBindings[key] = binding
						}
					}
				}
				identity, resolution := m.analysis.resolveBefore(closure, binding)
				if ssaValueHasPointerType(binding) {
					if pointee, pointeeResolution := m.analysis.resolvePointeeBefore(closure, binding); pointeeResolution == protocolAliasMust {
						identity, resolution = pointee, pointeeResolution
					}
				}
				if resolution == protocolAliasMust && identity == m.originIdentity {
					if m.closureBindingReassigned(ssaFn, binding) {
						m.ambiguousClosureValues[freeVariable] = true
					} else {
						m.closureValues[freeVariable] = true
					}
				}
			}
		}
	}
}

func (m *ssaFlowAliasMatcher) closureBindingReassigned(function *ssa.Function, binding ssa.Value) bool {
	if m == nil || m.analysis == nil || function == nil || binding == nil {
		return true
	}
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		for _, instruction := range block.Instrs {
			store, ok := instruction.(*ssa.Store)
			if !ok || store.Addr != binding {
				continue
			}
			identity, resolution := m.analysis.resolveBefore(store, store.Val)
			if resolution != protocolAliasMust || identity != m.originIdentity {
				return true
			}
		}
	}
	return false
}

func (m *ssaFlowAliasMatcher) capturedResolutionAt(
	pass *analysis.Pass,
	instruction ssa.Instruction,
	expression ast.Expr,
) protocolAliasResolution {
	if m == nil || m.analysis == nil || instruction == nil || expression == nil || m.originIdentity == 0 {
		return protocolAliasUnknown
	}
	binding := m.capturedBindings[targetKeyForExpr(pass, expression)]
	if binding == nil {
		return protocolAliasUnknown
	}
	identity, resolution := m.analysis.resolvePointeeBefore(instruction, binding)
	if resolution == protocolAliasMust && identity == m.originIdentity {
		return protocolAliasMust
	}
	if resolution == protocolAliasAmbiguous && m.analysis.pointeeMayAliasBefore(instruction, binding, m.originIdentity) {
		return protocolAliasAmbiguous
	}
	return protocolAliasUnknown
}

func (m *ssaFlowAliasMatcher) matches(pass *analysis.Pass, expr ast.Expr) bool {
	return m.resolution(pass, expr) == protocolAliasMust
}

func (m *ssaFlowAliasMatcher) resolution(pass *analysis.Pass, expr ast.Expr) protocolAliasResolution {
	if m == nil || m.analysis == nil || expr == nil {
		return protocolAliasUnknown
	}
	debugRef := m.debugRefForExpr(pass, expr)
	if debugRef == nil {
		return protocolAliasUnknown
	}
	origin, originResolution := m.originAt(debugRef)
	candidate, candidateResolution := m.analysis.resolveBefore(debugRef, debugRef.X)
	usePointee := !m.originPointer && (debugRef.IsAddr || ssaValueHasPointerType(debugRef.X))
	if usePointee {
		candidate, candidateResolution = m.analysis.resolvePointeeBefore(debugRef, debugRef.X)
	}
	if originResolution != protocolAliasMust {
		return originResolution
	}
	if candidateResolution == protocolAliasMust {
		if origin == candidate {
			return protocolAliasMust
		}
		return protocolAliasUnknown
	}
	if candidateResolution == protocolAliasAmbiguous {
		if usePointee && m.analysis.pointeeMayAliasBefore(debugRef, debugRef.X, origin) {
			return protocolAliasAmbiguous
		}
		if !usePointee && m.analysis.mayAliasBefore(debugRef, debugRef.X, origin) {
			return protocolAliasAmbiguous
		}
	}
	return protocolAliasUnknown
}

func (m *ssaFlowAliasMatcher) resolutionAt(
	instruction ssa.Instruction,
	candidateValue ssa.Value,
) protocolAliasResolution {
	if m == nil || m.analysis == nil || instruction == nil || candidateValue == nil {
		return protocolAliasUnknown
	}
	origin, originResolution := m.originAt(instruction)
	if m.originValues[candidateValue] {
		return protocolAliasMust
	}
	for closureValue := range m.ambiguousClosureValues {
		if candidateValue == closureValue ||
			protocolValueDerivedFrom(candidateValue, closureValue, make(map[ssa.Value]bool)) {
			return protocolAliasAmbiguous
		}
		if loaded, ok := candidateValue.(*ssa.UnOp); ok && loaded.Op == token.MUL && loaded.X == closureValue {
			return protocolAliasAmbiguous
		}
	}
	if loaded, ok := candidateValue.(*ssa.UnOp); ok && loaded.Op == token.MUL &&
		m.analysis.interner.internValue(loaded.X) == origin {
		return protocolAliasMust
	}
	if m.closureValues[candidateValue] {
		return protocolAliasMust
	}
	for closureValue := range m.closureValues {
		if loaded, ok := candidateValue.(*ssa.UnOp); ok && loaded.Op == token.MUL && loaded.X == closureValue {
			return protocolAliasMust
		}
		if protocolValueDerivedFrom(candidateValue, closureValue, make(map[ssa.Value]bool)) {
			return protocolAliasMust
		}
	}
	if m.originIdentity != 0 && m.analysis.interner.internValue(candidateValue) == origin {
		return protocolAliasMust
	}
	candidate, candidateResolution := m.analysis.resolveBefore(instruction, candidateValue)
	if loaded, ok := candidateValue.(*ssa.UnOp); ok && loaded.Op == token.MUL {
		if pointee, resolution := m.analysis.resolvePointeeBefore(instruction, loaded.X); resolution != protocolAliasUnknown {
			candidate, candidateResolution = pointee, resolution
		}
	}
	usePointee := ssaValueHasPointerType(candidateValue) && !m.originPointer
	if usePointee {
		candidate, candidateResolution = m.analysis.resolvePointeeBefore(instruction, candidateValue)
	}
	if originResolution != protocolAliasMust {
		return originResolution
	}
	if phi, ok := candidateValue.(*ssa.Phi); ok {
		return m.phiResolutionAt(instruction, phi, origin)
	}
	if candidateResolution == protocolAliasMust {
		if origin == candidate {
			return protocolAliasMust
		}
		return protocolAliasUnknown
	}
	if candidateResolution != protocolAliasAmbiguous {
		return protocolAliasUnknown
	}
	if snapshot, ok := m.analysis.before[instruction]; ok {
		possibleObjects := snapshot.possibleObjects(m.analysis.interner.internValue(candidateValue))
		if len(possibleObjects) > 0 && !slices.Contains(possibleObjects, origin) {
			return protocolAliasUnknown
		}
	}
	if m.candidateLoadProvenDisjoint(instruction, candidateValue, origin) {
		return protocolAliasUnknown
	}
	if usePointee && m.analysis.pointeeMayAliasBefore(instruction, candidateValue, origin) {
		return protocolAliasAmbiguous
	}
	if !usePointee && m.analysis.mayAliasBefore(instruction, candidateValue, origin) {
		return protocolAliasAmbiguous
	}
	return protocolAliasUnknown
}

func (m *ssaFlowAliasMatcher) originAt(instruction ssa.Instruction) (protocolIdentity, protocolAliasResolution) {
	if m == nil || m.analysis == nil || instruction == nil {
		return 0, protocolAliasUnknown
	}
	if m.originIdentity != 0 {
		return m.originIdentity, protocolAliasMust
	}
	return m.analysis.resolveBefore(instruction, m.castValue)
}

func (m *ssaFlowAliasMatcher) phiResolutionAt(
	instruction ssa.Instruction,
	phi *ssa.Phi,
	origin protocolIdentity,
) protocolAliasResolution {
	if m == nil || m.analysis == nil || instruction == nil || phi == nil || len(phi.Edges) == 0 {
		return protocolAliasUnknown
	}
	allMustAlias := true
	mayAlias := false
	for _, edge := range phi.Edges {
		object, resolution := m.analysis.resolveBefore(instruction, edge)
		switch resolution {
		case protocolAliasMust:
			if object == origin {
				mayAlias = true
				continue
			}
			allMustAlias = false
		case protocolAliasAmbiguous:
			allMustAlias = false
			if m.analysis.mayAliasBefore(instruction, edge, origin) {
				mayAlias = true
			}
		case protocolAliasUnknown:
			allMustAlias = false
		}
	}
	if allMustAlias && mayAlias {
		return protocolAliasMust
	}
	if mayAlias {
		return protocolAliasAmbiguous
	}
	return protocolAliasUnknown
}

func (m *ssaFlowAliasMatcher) candidateLoadProvenDisjoint(
	instruction ssa.Instruction,
	candidateValue ssa.Value,
	origin protocolIdentity,
) bool {
	load, ok := candidateValue.(*ssa.UnOp)
	if !ok || load.Op != token.MUL || origin == 0 {
		return false
	}
	snapshot, ok := m.analysis.before[instruction]
	if !ok {
		return false
	}
	interner := m.analysis.interner
	candidateAddress := interner.internValue(load.X)
	possibleObjects := snapshot.possibleMemoryObjects(candidateAddress)
	if len(possibleObjects) > 0 && !slices.Contains(possibleObjects, origin) {
		return true
	}
	if len(m.castStoreAddresses) == 0 {
		return false
	}
	candidateCell, candidateResolution := snapshot.resolve(candidateAddress)
	if candidateResolution != protocolAliasMust ||
		slices.Contains(possibleObjects, origin) {
		return false
	}
	for _, address := range m.castStoreAddresses {
		storageCell, storageResolution := snapshot.resolve(interner.internValue(address))
		if storageResolution != protocolAliasMust || storageCell == candidateCell {
			return false
		}
	}
	return true
}

func ssaValueHasPointerType(value ssa.Value) bool {
	if value == nil || value.Type() == nil {
		return false
	}
	_, ok := types.Unalias(value.Type()).Underlying().(*types.Pointer)
	return ok
}

func (m *ssaFlowAliasMatcher) debugRefForExpr(pass *analysis.Pass, expr ast.Expr) *ssa.DebugRef {
	debugRef := m.debugRefs[stripParens(expr)]
	if debugRef == nil {
		debugRef = m.debugRefs[stripParensAndStar(expr)]
	}
	if debugRef != nil {
		return debugRef
	}
	key := targetKeyForExpr(pass, expr)
	for _, candidate := range m.debugRefsByObject[key] {
		if candidate.Pos() > expr.Pos() || debugRef != nil && candidate.Pos() <= debugRef.Pos() {
			continue
		}
		debugRef = candidate
	}
	return debugRef
}

// enrichTargetWithSSAAlias returns a copy of target with an SSA-derived
// alias set attached. If SSA information is unavailable or the cast
// expression cannot be resolved, the original target is returned unchanged.
func enrichTargetWithSSAAlias(
	pass *analysis.Pass,
	ssaFn *ssa.Function,
	ac cfaAssignedCast,
) castTarget {
	if ssaFn == nil {
		return ac.target
	}
	aliases := computeMustAliasKeys(pass, ssaFn, ac.pos)
	if len(aliases) == 0 {
		return ac.target
	}
	// Remove the primary target key from aliases — it is already matched
	// by the targetKey comparison in matchesExpr.
	delete(aliases, ac.target.targetKey)
	if len(aliases) == 0 {
		return ac.target
	}
	enriched := ac.target
	enriched.aliasKeys = aliases
	return enriched
}

// computeMustAliasKeys finds all source-level variables that provably hold
// the same SSA value as the cast expression result — and are never
// reassigned to a different value in the function.
//
// The algorithm uses DebugRef instructions emitted in GlobalDebug mode:
//
//  1. Find the SSA Convert/ChangeType instruction matching the cast position.
//  2. Collect all DebugRef instructions where X == castValue.
//  3. Exclude any object that has another DebugRef with a different X
//     (indicating reassignment).
func computeMustAliasKeys(
	pass *analysis.Pass,
	ssaFn *ssa.Function,
	castNode ast.Node,
) ssaAliasSet {
	castValue := findSSACastValue(ssaFn, castNode)
	if castValue == nil {
		return nil
	}

	// Phase 1: collect all DebugRef instructions in the function.
	// Build two maps:
	//   objToValues: object → set of distinct SSA values assigned via DebugRef
	//   castAliasObjs: objects whose DebugRef X matches the cast value
	type objInfo struct {
		key         string
		valueCount  int
		aliasesCast bool
	}
	objMap := make(map[types.Object]*objInfo)

	for _, block := range ssaFn.Blocks {
		for _, instr := range block.Instrs {
			dbg, ok := instr.(*ssa.DebugRef)
			if !ok || dbg.IsAddr {
				continue
			}
			obj := dbg.Object()
			if obj == nil {
				continue
			}
			info, exists := objMap[obj]
			if !exists {
				info = &objInfo{key: objectKeyAt(pass, obj)}
				objMap[obj] = info
			}
			if dbg.X == castValue {
				info.aliasesCast = true
			} else {
				info.valueCount++
			}
		}
	}

	// Phase 2: build the alias set from objects that alias the cast value
	// AND were never assigned a different value.
	result := make(ssaAliasSet)
	for _, info := range objMap {
		if !info.aliasesCast {
			continue
		}
		// Exclude objects with any DebugRef pointing to a different value.
		// This conservatively handles reassignment: y := x; y = other
		// produces two DebugRefs for y (one with castValue, one without),
		// so y is excluded from the alias set.
		if info.valueCount > 0 {
			continue
		}
		if info.key == "" {
			continue
		}
		result[info.key] = true
		if len(result) > maxAliasSetSize {
			return nil
		}
	}

	return result
}

// findSSACastValue locates the SSA value produced by a type conversion
// at the given AST node position. SSA and AST use different position
// anchors for type conversions: AST CallExpr.Pos() is the type name
// start, while SSA ChangeType/Convert.Pos() usually points at the Lparen.
//
// We deliberately prefer the earliest conversion instruction in the node span.
// Nested helper calls inside the cast argument (for example,
// T(strings.TrimSpace(raw))) also produce in-range SSA values, but they are not
// the cast result and must not drive alias inference.
func findSSACastValue(ssaFn *ssa.Function, castNode ast.Node) ssa.Value {
	if ssaFn == nil || castNode == nil {
		return nil
	}
	nodeStart := castNode.Pos()
	nodeEnd := castNode.End()
	if !nodeStart.IsValid() {
		return nil
	}

	var best ssa.Value
	var bestPos token.Pos
	for _, block := range ssaFn.Blocks {
		for _, instr := range block.Instrs {
			val, ok := instr.(ssa.Value)
			if !ok {
				continue
			}
			valPos := val.Pos()
			if !valPos.IsValid() || valPos < nodeStart || valPos >= nodeEnd {
				continue
			}
			switch val.(type) {
			case *ssa.ChangeType, *ssa.Convert:
				if best == nil || valPos < bestPos {
					best = val
					bestPos = valPos
				}
			}
		}
	}
	return best
}

// enrichAssignedCastsWithSSA attaches the canonical SSA-derived alias state to
// all assigned casts.
func enrichAssignedCastsWithSSA(
	pass *analysis.Pass,
	ssaRes *ssaResult,
	fn *ast.FuncDecl,
	assignedCasts []cfaAssignedCast,
) ssaAvailability {
	if pass == nil || pass.TypesInfo == nil || fn == nil || fn.Name == nil {
		return ssaAvailability{Status: ssaAvailabilityMissingFunction}
	}
	obj, ok := pass.TypesInfo.Defs[fn.Name]
	if !ok || obj == nil {
		return ssaAvailability{Status: ssaAvailabilityMissingFunction}
	}
	typesFunc, ok := obj.(*types.Func)
	if !ok {
		return ssaAvailability{Status: ssaAvailabilityMissingFunction}
	}
	resolution := resolveSSAFunction(ssaRes, typesFunc)
	if !resolution.Availability.ready() {
		return resolution.Availability
	}
	for i := range assignedCasts {
		enrichAssignedCastWithFlowSensitiveAliases(pass, resolution.Function, &assignedCasts[i])
	}
	return resolution.Availability
}

// enrichAssignedCastsWithSSAClosure attaches SSA-derived alias sets for
// casts within a closure body. Closures are separate *ssa.Function objects
// in SSA; this helper locates the correct one by matching positions.
func enrichAssignedCastsWithSSAClosure(
	pass *analysis.Pass,
	ssaRes *ssaResult,
	lit *ast.FuncLit,
	assignedCasts []cfaAssignedCast,
) ssaAvailability {
	if lit == nil {
		return ssaAvailability{Status: ssaAvailabilityMissingClosure}
	}
	resolution := resolveSSAClosure(ssaRes, lit.Pos())
	if !resolution.Availability.ready() {
		return resolution.Availability
	}
	for i := range assignedCasts {
		enrichAssignedCastWithFlowSensitiveAliases(pass, resolution.Function, &assignedCasts[i])
	}
	return resolution.Availability
}

func enrichAssignedCastWithFlowSensitiveAliases(
	pass *analysis.Pass,
	ssaFn *ssa.Function,
	assignedCast *cfaAssignedCast,
) {
	if assignedCast == nil {
		return
	}
	assignedCast.target.flowAliases = newSSAFlowAliasMatcher(pass, ssaFn, assignedCast.pos)
}
