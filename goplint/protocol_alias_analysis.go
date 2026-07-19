// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"cmp"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/ssa"
)

const (
	protocolAliasUnknown protocolAliasResolution = iota
	protocolAliasMust
	protocolAliasAmbiguous
)

type (
	protocolAliasResolution uint8

	protocolAliasSnapshot struct {
		must            map[protocolIdentity]protocolIdentity
		ambiguous       map[protocolIdentity]struct{}
		possible        map[protocolIdentity]map[protocolIdentity]struct{}
		memoryMust      map[protocolIdentity]protocolIdentity
		memoryAmbiguous map[protocolIdentity]struct{}
		memoryPossible  map[protocolIdentity]map[protocolIdentity]struct{}
	}

	protocolAliasAnalysis struct {
		interner *protocolIdentityInterner
		before   map[ssa.Instruction]protocolAliasSnapshot
		after    map[ssa.Instruction]protocolAliasSnapshot
	}
)

func analyzeProtocolAliases(fn *ssa.Function, interner *protocolIdentityInterner) *protocolAliasAnalysis {
	analysis := &protocolAliasAnalysis{
		interner: interner,
		before:   make(map[ssa.Instruction]protocolAliasSnapshot),
		after:    make(map[ssa.Instruction]protocolAliasSnapshot),
	}
	if fn == nil || interner == nil || len(fn.Blocks) == 0 {
		return analysis
	}

	blocks := slices.Clone(fn.Blocks)
	slices.SortFunc(blocks, func(left, right *ssa.BasicBlock) int {
		return cmp.Compare(left.Index, right.Index)
	})
	out := make(map[*ssa.BasicBlock]protocolAliasSnapshot, len(blocks))
	reached := make(map[*ssa.BasicBlock]bool, len(blocks))
	queue := slices.Clone(blocks)
	inQueue := make(map[*ssa.BasicBlock]bool, len(blocks))
	for _, block := range queue {
		inQueue[block] = true
	}

	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		inQueue[block] = false

		state, ok := protocolAliasInputState(fn, block, out, reached, interner)
		if !ok {
			continue
		}
		reached[block] = true
		for _, instruction := range block.Instrs {
			analysis.before[instruction] = state.clone()
			state = transferProtocolAliasInstruction(state, instruction, block, out, reached, interner)
			analysis.after[instruction] = state.clone()
		}
		if previous, exists := out[block]; exists && previous.equal(state) {
			continue
		}
		out[block] = state
		for _, successor := range block.Succs {
			if !inQueue[successor] {
				queue = append(queue, successor)
				inQueue[successor] = true
			}
		}
	}

	return analysis
}

func (a *protocolAliasAnalysis) resolveBefore(instruction ssa.Instruction, value ssa.Value) (protocolIdentity, protocolAliasResolution) {
	if a == nil || a.interner == nil || instruction == nil || value == nil {
		return 0, protocolAliasUnknown
	}
	snapshot, ok := a.before[instruction]
	if !ok {
		return 0, protocolAliasUnknown
	}
	return snapshot.resolve(a.interner.internValue(value))
}

func (a *protocolAliasAnalysis) uncertaintyBefore(instruction ssa.Instruction, value ssa.Value) protocolUncertaintySet {
	_, resolution := a.resolveBefore(instruction, value)
	if resolution == protocolAliasAmbiguous {
		return protocolUncertaintyAmbiguousIdentity
	}
	return 0
}

func (a *protocolAliasAnalysis) resolvePointeeBefore(
	instruction ssa.Instruction,
	address ssa.Value,
) (protocolIdentity, protocolAliasResolution) {
	if a == nil || a.interner == nil || instruction == nil || address == nil {
		return 0, protocolAliasUnknown
	}
	snapshot, ok := a.before[instruction]
	if !ok {
		return 0, protocolAliasUnknown
	}
	return snapshot.resolveMemory(a.interner.internValue(address))
}

func (a *protocolAliasAnalysis) mayAliasBefore(
	instruction ssa.Instruction,
	value ssa.Value,
	object protocolIdentity,
) bool {
	if a == nil || a.interner == nil || instruction == nil || value == nil || object == 0 {
		return false
	}
	snapshot, ok := a.before[instruction]
	if !ok {
		return false
	}
	return snapshot.mayResolveTo(a.interner.internValue(value), object)
}

func (a *protocolAliasAnalysis) pointeeMayAliasBefore(
	instruction ssa.Instruction,
	address ssa.Value,
	object protocolIdentity,
) bool {
	if a == nil || a.interner == nil || instruction == nil || address == nil || object == 0 {
		return false
	}
	snapshot, ok := a.before[instruction]
	if !ok {
		return false
	}
	return slices.Contains(snapshot.possibleMemoryObjects(a.interner.internValue(address)), object)
}

func protocolAliasInputState(
	fn *ssa.Function,
	block *ssa.BasicBlock,
	out map[*ssa.BasicBlock]protocolAliasSnapshot,
	reached map[*ssa.BasicBlock]bool,
	interner *protocolIdentityInterner,
) (protocolAliasSnapshot, bool) {
	if block == fn.Blocks[0] {
		state := newProtocolAliasSnapshot()
		for _, parameter := range fn.Params {
			identity := interner.internValue(parameter)
			state.bind(identity, identity)
		}
		for _, freeVar := range fn.FreeVars {
			identity := interner.internValue(freeVar)
			state.bind(identity, identity)
		}
		return state, true
	}

	predecessors := slices.Clone(block.Preds)
	slices.SortFunc(predecessors, func(left, right *ssa.BasicBlock) int {
		return cmp.Compare(left.Index, right.Index)
	})
	var state protocolAliasSnapshot
	hasReachedPredecessor := false
	for _, predecessor := range predecessors {
		if !reached[predecessor] {
			continue
		}
		if !hasReachedPredecessor {
			state = out[predecessor].clone()
			hasReachedPredecessor = true
			continue
		}
		state = state.join(out[predecessor])
	}
	return state, hasReachedPredecessor
}

func transferProtocolAliasInstruction(
	state protocolAliasSnapshot,
	instruction ssa.Instruction,
	block *ssa.BasicBlock,
	out map[*ssa.BasicBlock]protocolAliasSnapshot,
	reached map[*ssa.BasicBlock]bool,
	interner *protocolIdentityInterner,
) protocolAliasSnapshot {
	if store, ok := instruction.(*ssa.Store); ok {
		state.store(interner.internValue(store.Addr), interner.internValue(store.Val))
		if protocolAliasStoreEscapes(store) {
			state.escapeProtocolValue(store.Val, interner)
		}
		return state
	}
	if send, ok := instruction.(*ssa.Send); ok {
		state.escapeProtocolValue(send.X, interner)
		return state
	}
	if update, ok := instruction.(*ssa.MapUpdate); ok {
		state.escapeProtocolValue(update.Key, interner)
		state.escapeProtocolValue(update.Value, interner)
		return state
	}
	if goCall, ok := instruction.(*ssa.Go); ok {
		state.escapeCallArguments(goCall.Common(), interner)
		return state
	}
	if returned, ok := instruction.(*ssa.Return); ok {
		for _, result := range returned.Results {
			state.escapeProtocolValue(result, interner)
		}
		return state
	}
	value, ok := instruction.(ssa.Value)
	if !ok {
		return state
	}
	identity := interner.internValue(value)
	if closure, ok := value.(*ssa.MakeClosure); ok && protocolClosureEscapes(closure) {
		for _, binding := range closure.Bindings {
			state.escapeProtocolValue(binding, interner)
		}
	}
	if field, ok := value.(*ssa.FieldAddr); ok {
		base := interner.internValue(field.X)
		state.bindDerivedAddress(identity, base, func(object protocolIdentity) protocolIdentity {
			return interner.internFieldAddress(
				object,
				field.Field,
				protocolStaticFieldName(field.X.Type(), field.Field),
				field.Pos(),
			)
		}, interner.identityIsFreshAllocation)
		return state
	}
	if indexed, ok := value.(*ssa.IndexAddr); ok {
		index, isStatic := protocolStaticIndex(indexed.Index)
		if !isStatic {
			state.markAmbiguous(identity)
			return state
		}
		base := interner.internValue(indexed.X)
		state.bindDerivedAddress(identity, base, func(object protocolIdentity) protocolIdentity {
			return interner.internIndexAddress(object, index, indexed.Pos())
		}, interner.identityIsFreshAllocation)
		return state
	}
	if phi, ok := value.(*ssa.Phi); ok {
		return transferProtocolPhi(state, identity, phi, block, out, reached, interner)
	}
	if load, ok := value.(*ssa.UnOp); ok && load.Op == token.MUL {
		state.load(identity, interner.internValue(load.X))
		return state
	}
	if source, ok := interner.copySource(identity); ok {
		state.copyBinding(identity, source)
		return state
	}
	state.bind(identity, identity)
	return state
}

func (s *protocolAliasSnapshot) bindDerivedAddress(
	identity protocolIdentity,
	base protocolIdentity,
	derive func(protocolIdentity) protocolIdentity,
	isFreshBase func(protocolIdentity) bool,
) {
	if object, resolution := s.resolve(base); resolution == protocolAliasMust {
		derived := derive(object)
		s.initializeFreshCell(derived, object, isFreshBase)
		s.bind(identity, derived)
		return
	}
	objects := s.possibleObjects(base)
	derived := make([]protocolIdentity, 0, len(objects))
	for _, object := range objects {
		cell := derive(object)
		s.initializeFreshCell(cell, object, isFreshBase)
		derived = append(derived, cell)
	}
	s.markAmbiguous(identity, derived...)
}

func (s *protocolAliasSnapshot) initializeFreshCell(
	cell protocolIdentity,
	base protocolIdentity,
	isFreshBase func(protocolIdentity) bool,
) {
	if cell == 0 || isFreshBase == nil || !isFreshBase(base) {
		return
	}
	if _, initialized := s.memoryMust[cell]; initialized {
		return
	}
	if _, ambiguous := s.memoryAmbiguous[cell]; ambiguous {
		return
	}
	s.memoryMust[cell] = cell
}

func protocolAliasStoreEscapes(store *ssa.Store) bool {
	if store == nil || store.Addr == nil {
		return true
	}
	switch address := store.Addr.(type) {
	case *ssa.Global:
		return true
	case *ssa.FieldAddr:
		_, global := address.X.(*ssa.Global)
		return global
	default:
		return false
	}
}

func protocolClosureEscapes(closure *ssa.MakeClosure) bool {
	if closure == nil || closure.Referrers() == nil {
		return true
	}
	for _, referrer := range *closure.Referrers() {
		switch instruction := referrer.(type) {
		case *ssa.DebugRef:
			continue
		case *ssa.Call:
			if instruction.Common() != nil && instruction.Common().Value == closure {
				continue
			}
			return true
		case *ssa.Defer:
			if instruction.Common() != nil && instruction.Common().Value == closure {
				continue
			}
			return true
		default:
			return true
		}
	}
	return false
}

func (s *protocolAliasSnapshot) escapeCallArguments(call *ssa.CallCommon, interner *protocolIdentityInterner) {
	if call == nil {
		return
	}
	if closure, ok := call.Value.(*ssa.MakeClosure); ok {
		for _, binding := range closure.Bindings {
			s.escapeProtocolValue(binding, interner)
		}
	}
	for _, argument := range call.Args {
		s.escapeProtocolValue(argument, interner)
	}
}

func (s *protocolAliasSnapshot) escapeProtocolValue(value ssa.Value, interner *protocolIdentityInterner) {
	if !protocolValueMayCarryMutableIdentity(value) || interner == nil {
		return
	}
	s.escape(interner.internValue(value))
}

func protocolValueMayCarryMutableIdentity(value ssa.Value) bool {
	if value == nil || value.Type() == nil {
		return false
	}
	switch types.Unalias(value.Type()).Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Interface, *types.Chan, *types.Signature:
		return true
	default:
		return false
	}
}

func transferProtocolPhi(
	state protocolAliasSnapshot,
	identity protocolIdentity,
	phi *ssa.Phi,
	block *ssa.BasicBlock,
	out map[*ssa.BasicBlock]protocolAliasSnapshot,
	reached map[*ssa.BasicBlock]bool,
	interner *protocolIdentityInterner,
) protocolAliasSnapshot {
	possible := make(map[protocolIdentity]struct{})
	allMust := true
	hasReachedEdge := false
	for edgeIndex, edge := range phi.Edges {
		if edgeIndex >= len(block.Preds) || !reached[block.Preds[edgeIndex]] {
			continue
		}
		hasReachedEdge = true
		edgeIdentity := interner.internValue(edge)
		object, resolution := out[block.Preds[edgeIndex]].resolve(edgeIdentity)
		if resolution == protocolAliasMust {
			possible[object] = struct{}{}
			continue
		}
		allMust = false
		for _, candidate := range out[block.Preds[edgeIndex]].possibleObjects(edgeIdentity) {
			possible[candidate] = struct{}{}
		}
	}
	objects := make([]protocolIdentity, 0, len(possible))
	for object := range possible {
		objects = append(objects, object)
	}
	slices.Sort(objects)
	if !hasReachedEdge || len(objects) == 0 {
		state.markAmbiguous(identity)
		return state
	}
	if allMust && len(objects) == 1 {
		state.bind(identity, objects[0])
		return state
	}
	state.markAmbiguous(identity, objects...)
	return state
}

func newProtocolAliasSnapshot() protocolAliasSnapshot {
	return protocolAliasSnapshot{
		must:            make(map[protocolIdentity]protocolIdentity),
		ambiguous:       make(map[protocolIdentity]struct{}),
		possible:        make(map[protocolIdentity]map[protocolIdentity]struct{}),
		memoryMust:      make(map[protocolIdentity]protocolIdentity),
		memoryAmbiguous: make(map[protocolIdentity]struct{}),
		memoryPossible:  make(map[protocolIdentity]map[protocolIdentity]struct{}),
	}
}

func (s *protocolAliasSnapshot) bind(identity, object protocolIdentity) {
	s.must[identity] = object
	delete(s.ambiguous, identity)
	delete(s.possible, identity)
}

func (s *protocolAliasSnapshot) markAmbiguous(identity protocolIdentity, objects ...protocolIdentity) {
	delete(s.must, identity)
	s.ambiguous[identity] = struct{}{}
	if len(objects) == 0 {
		delete(s.possible, identity)
		return
	}
	possible := make(map[protocolIdentity]struct{}, len(objects))
	for _, object := range objects {
		if object != 0 {
			possible[object] = struct{}{}
		}
	}
	if len(possible) == 0 {
		delete(s.possible, identity)
		return
	}
	s.possible[identity] = possible
}

func (s *protocolAliasSnapshot) copyBinding(identity, source protocolIdentity) {
	if object, resolution := s.resolve(source); resolution == protocolAliasMust {
		s.bind(identity, object)
		return
	}
	s.markAmbiguous(identity, s.possibleObjects(source)...)
}

func (s *protocolAliasSnapshot) store(address, value protocolIdentity) {
	addressObjects := s.possibleObjects(address)
	if _, resolution := s.resolve(address); resolution == protocolAliasUnknown || len(addressObjects) == 0 {
		for cell := range s.memoryMust {
			s.markMemoryAmbiguous(cell)
		}
		return
	}
	valueObjects := s.possibleObjects(value)
	_, valueResolution := s.resolve(value)
	for _, cell := range addressObjects {
		if len(addressObjects) == 1 && valueResolution == protocolAliasMust && len(valueObjects) == 1 {
			s.memoryMust[cell] = valueObjects[0]
			delete(s.memoryAmbiguous, cell)
			delete(s.memoryPossible, cell)
			continue
		}
		s.markMemoryAmbiguous(cell, valueObjects...)
	}
}

func (s *protocolAliasSnapshot) load(identity, address protocolIdentity) {
	object, resolution := s.resolveMemory(address)
	if resolution == protocolAliasMust {
		s.bind(identity, object)
		return
	}
	if resolution == protocolAliasAmbiguous {
		s.markAmbiguous(identity, s.possibleMemoryObjects(address)...)
		return
	}
	s.markAmbiguous(identity)
}

func (s protocolAliasSnapshot) resolveMemory(address protocolIdentity) (protocolIdentity, protocolAliasResolution) {
	addressObjects := s.possibleObjects(address)
	_, addressResolution := s.resolve(address)
	if addressResolution == protocolAliasUnknown || len(addressObjects) == 0 {
		return 0, protocolAliasUnknown
	}
	objects := s.possibleMemoryObjects(address)
	if addressResolution == protocolAliasMust && len(addressObjects) == 1 && len(objects) == 1 {
		if object, ok := s.memoryMust[addressObjects[0]]; ok {
			return object, protocolAliasMust
		}
	}
	if len(objects) > 0 {
		return 0, protocolAliasAmbiguous
	}
	return 0, protocolAliasUnknown
}

func (s protocolAliasSnapshot) possibleMemoryObjects(address protocolIdentity) []protocolIdentity {
	objectsSet := make(map[protocolIdentity]struct{})
	for _, cell := range s.possibleObjects(address) {
		if object, ok := s.memoryMust[cell]; ok {
			objectsSet[object] = struct{}{}
		}
		for object := range s.memoryPossible[cell] {
			objectsSet[object] = struct{}{}
		}
	}
	objects := make([]protocolIdentity, 0, len(objectsSet))
	for object := range objectsSet {
		objects = append(objects, object)
	}
	slices.Sort(objects)
	return objects
}

func (s *protocolAliasSnapshot) markMemoryAmbiguous(cell protocolIdentity, objects ...protocolIdentity) {
	if object, ok := s.memoryMust[cell]; ok {
		objects = append(objects, object)
	}
	delete(s.memoryMust, cell)
	s.memoryAmbiguous[cell] = struct{}{}
	if len(objects) == 0 {
		delete(s.memoryPossible, cell)
		return
	}
	possible := s.memoryPossible[cell]
	if possible == nil {
		possible = make(map[protocolIdentity]struct{}, len(objects))
		s.memoryPossible[cell] = possible
	}
	for _, object := range objects {
		if object != 0 {
			possible[object] = struct{}{}
		}
	}
}

func (s *protocolAliasSnapshot) escape(identity protocolIdentity) {
	for _, cell := range s.possibleObjects(identity) {
		objects := make([]protocolIdentity, 0, 1+len(s.memoryPossible[cell]))
		if object, ok := s.memoryMust[cell]; ok {
			objects = append(objects, object)
		}
		for object := range s.memoryPossible[cell] {
			objects = append(objects, object)
		}
		s.markMemoryAmbiguous(cell, objects...)
	}
	s.killObject(identity)
}

func (s *protocolAliasSnapshot) killObject(identity protocolIdentity) {
	object, resolution := s.resolve(identity)
	if resolution == protocolAliasAmbiguous {
		s.markAmbiguous(identity, s.possibleObjects(identity)...)
		return
	}
	if resolution == protocolAliasUnknown {
		object = identity
	}
	for candidate, target := range s.must {
		if target == object {
			s.markAmbiguous(candidate, object)
		}
	}
	s.markAmbiguous(identity, object)
}

func (s protocolAliasSnapshot) resolve(identity protocolIdentity) (protocolIdentity, protocolAliasResolution) {
	if _, ambiguous := s.ambiguous[identity]; ambiguous {
		return 0, protocolAliasAmbiguous
	}
	object, ok := s.must[identity]
	if !ok {
		return 0, protocolAliasUnknown
	}
	return object, protocolAliasMust
}

func (s protocolAliasSnapshot) possibleObjects(identity protocolIdentity) []protocolIdentity {
	if object, ok := s.must[identity]; ok {
		return []protocolIdentity{object}
	}
	possible := s.possible[identity]
	objects := make([]protocolIdentity, 0, len(possible))
	for object := range possible {
		objects = append(objects, object)
	}
	slices.Sort(objects)
	return objects
}

func (s protocolAliasSnapshot) mayResolveTo(identity, object protocolIdentity) bool {
	if resolved, ok := s.must[identity]; ok {
		return resolved == object
	}
	if _, ambiguous := s.ambiguous[identity]; !ambiguous {
		return false
	}
	possible, bounded := s.possible[identity]
	if !bounded {
		return true
	}
	_, ok := possible[object]
	return ok
}

func (s protocolAliasSnapshot) join(other protocolAliasSnapshot) protocolAliasSnapshot {
	joined := joinProtocolAliasBindings(s, other)
	if len(s.memoryMust) == 0 && len(s.memoryAmbiguous) == 0 && len(other.memoryMust) == 0 && len(other.memoryAmbiguous) == 0 {
		return joined
	}
	memory := joinProtocolAliasBindings(
		protocolAliasSnapshot{must: s.memoryMust, ambiguous: s.memoryAmbiguous, possible: s.memoryPossible},
		protocolAliasSnapshot{must: other.memoryMust, ambiguous: other.memoryAmbiguous, possible: other.memoryPossible},
	)
	joined.memoryMust = memory.must
	joined.memoryAmbiguous = memory.ambiguous
	joined.memoryPossible = memory.possible
	return joined
}

func joinProtocolAliasBindings(s, other protocolAliasSnapshot) protocolAliasSnapshot {
	joined := newProtocolAliasSnapshot()
	identities := make(map[protocolIdentity]struct{}, len(s.must)+len(other.must))
	for identity := range s.must {
		identities[identity] = struct{}{}
	}
	for identity := range other.must {
		identities[identity] = struct{}{}
	}
	for identity := range s.ambiguous {
		identities[identity] = struct{}{}
	}
	for identity := range other.ambiguous {
		identities[identity] = struct{}{}
	}
	for identity := range identities {
		left, leftOK := s.must[identity]
		right, rightOK := other.must[identity]
		if leftOK && rightOK && left == right {
			joined.must[identity] = left
			continue
		}
		joined.ambiguous[identity] = struct{}{}
		possible := make(map[protocolIdentity]struct{}, 2)
		addProtocolAliasPossibilities(possible, s, identity)
		addProtocolAliasPossibilities(possible, other, identity)
		if len(possible) > 0 {
			joined.possible[identity] = possible
		}
	}
	return joined
}

func addProtocolAliasPossibilities(
	destination map[protocolIdentity]struct{},
	source protocolAliasSnapshot,
	identity protocolIdentity,
) {
	if object, ok := source.must[identity]; ok {
		destination[object] = struct{}{}
		return
	}
	for object := range source.possible[identity] {
		destination[object] = struct{}{}
	}
}

func (s protocolAliasSnapshot) clone() protocolAliasSnapshot {
	cloned := newProtocolAliasSnapshot()
	maps.Copy(cloned.must, s.must)
	maps.Copy(cloned.ambiguous, s.ambiguous)
	for identity, possible := range s.possible {
		cloned.possible[identity] = maps.Clone(possible)
	}
	maps.Copy(cloned.memoryMust, s.memoryMust)
	maps.Copy(cloned.memoryAmbiguous, s.memoryAmbiguous)
	for identity, possible := range s.memoryPossible {
		cloned.memoryPossible[identity] = maps.Clone(possible)
	}
	return cloned
}

func (s protocolAliasSnapshot) equal(other protocolAliasSnapshot) bool {
	if !maps.Equal(s.must, other.must) || !maps.Equal(s.ambiguous, other.ambiguous) || len(s.possible) != len(other.possible) ||
		!maps.Equal(s.memoryMust, other.memoryMust) || !maps.Equal(s.memoryAmbiguous, other.memoryAmbiguous) ||
		len(s.memoryPossible) != len(other.memoryPossible) {
		return false
	}
	for identity, possible := range s.possible {
		if !maps.Equal(possible, other.possible[identity]) {
			return false
		}
	}
	for identity, possible := range s.memoryPossible {
		if !maps.Equal(possible, other.memoryPossible[identity]) {
			return false
		}
	}
	return true
}
