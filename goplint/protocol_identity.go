// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

const (
	protocolIdentityValue      protocolIdentityKind = "ssa-value"
	protocolIdentityAllocation protocolIdentityKind = "allocation"
	protocolIdentityParameter  protocolIdentityKind = "parameter"
	protocolIdentityReceiver   protocolIdentityKind = "receiver"
	protocolIdentityResult     protocolIdentityKind = "result"
	protocolIdentityCopy       protocolIdentityKind = "copy"
	protocolIdentityPhi        protocolIdentityKind = "phi"
	protocolIdentityGlobalAddr protocolIdentityKind = "global-address"
	protocolIdentityFieldAddr  protocolIdentityKind = "static-field-address"
	protocolIdentityIndexAddr  protocolIdentityKind = "static-index-address"
)

type (
	protocolIdentityKind string

	protocolIdentityDescriptor struct {
		ID        protocolIdentity
		Kind      protocolIdentityKind
		Procedure string
		Name      string
		Slot      int
		Position  token.Pos
		ObjectKey string
	}

	protocolIdentityInterner struct {
		nextID      protocolIdentity
		values      map[ssa.Value]protocolIdentity
		objects     map[string]protocolIdentity
		descriptors map[protocolIdentity]protocolIdentityDescriptor
		copySources map[protocolIdentity]protocolIdentity
	}
)

func newProtocolIdentityInterner() *protocolIdentityInterner {
	return &protocolIdentityInterner{
		nextID:      1,
		values:      make(map[ssa.Value]protocolIdentity),
		objects:     make(map[string]protocolIdentity),
		descriptors: make(map[protocolIdentity]protocolIdentityDescriptor),
		copySources: make(map[protocolIdentity]protocolIdentity),
	}
}

func (i *protocolIdentityInterner) internValue(value ssa.Value) protocolIdentity {
	if value == nil {
		return 0
	}
	if identity, ok := i.values[value]; ok {
		return identity
	}
	if identity, ok := i.internAddressValue(value); ok {
		i.values[value] = identity
		return identity
	}

	kind, slot := protocolIdentityKindForValue(value)
	descriptor := protocolIdentityDescriptor{
		Kind:      kind,
		Procedure: protocolProcedureKey(value.Parent()),
		Name:      value.Name(),
		Slot:      slot,
		Position:  value.Pos(),
	}
	descriptor.ObjectKey = protocolIdentityObjectKey(descriptor)
	identity := i.allocate(descriptor)
	i.values[value] = identity
	if source := protocolCopySource(value); source != nil {
		i.copySources[identity] = i.internValue(source)
	}
	return identity
}

func (i *protocolIdentityInterner) internResult(fn *ssa.Function, slot int) protocolIdentity {
	procedure := protocolProcedureKey(fn)
	key := fmt.Sprintf("result|%s|%d", procedure, slot)
	if identity, ok := i.objects[key]; ok {
		return identity
	}
	identity := i.allocate(protocolIdentityDescriptor{
		Kind:      protocolIdentityResult,
		Procedure: procedure,
		Name:      fmt.Sprintf("result[%d]", slot),
		Slot:      slot,
		ObjectKey: key,
	})
	i.objects[key] = identity
	return identity
}

func (i *protocolIdentityInterner) internAddressValue(value ssa.Value) (protocolIdentity, bool) {
	switch typed := value.(type) {
	case *ssa.Global:
		procedure := protocolGlobalProcedureKey(typed)
		key := fmt.Sprintf("global-address|%s|%s", procedure, typed.Name())
		if identity, ok := i.objects[key]; ok {
			return identity, true
		}
		identity := i.allocate(protocolIdentityDescriptor{
			Kind:      protocolIdentityGlobalAddr,
			Procedure: procedure,
			Name:      typed.Name(),
			Slot:      -1,
			Position:  typed.Pos(),
			ObjectKey: key,
		})
		i.objects[key] = identity
		return identity, true
	case *ssa.FieldAddr:
		baseIdentity := i.internValue(typed.X)
		identity := i.internFieldAddress(baseIdentity, typed.Field, protocolStaticFieldName(typed.X.Type(), typed.Field), typed.Pos())
		if sourceBase, ok := i.copySource(baseIdentity); ok {
			i.copySources[identity] = i.internFieldAddress(
				sourceBase,
				typed.Field,
				protocolStaticFieldName(typed.X.Type(), typed.Field),
				typed.Pos(),
			)
		}
		return identity, identity != 0
	case *ssa.IndexAddr:
		index, ok := protocolStaticIndex(typed.Index)
		if !ok {
			return 0, false
		}
		baseIdentity := i.internValue(typed.X)
		identity := i.internIndexAddress(baseIdentity, index, typed.Pos())
		if sourceBase, copied := i.copySource(baseIdentity); copied {
			i.copySources[identity] = i.internIndexAddress(sourceBase, index, typed.Pos())
		}
		return identity, identity != 0
	default:
		return 0, false
	}
}

func (i *protocolIdentityInterner) internIndexAddress(
	baseIdentity protocolIdentity,
	index string,
	position token.Pos,
) protocolIdentity {
	base, ok := i.descriptor(baseIdentity)
	if !ok || base.ObjectKey == "" || index == "" {
		return 0
	}
	key := fmt.Sprintf("static-index-address|%s|%s", base.ObjectKey, index)
	if identity, exists := i.objects[key]; exists {
		return identity
	}
	identity := i.allocate(protocolIdentityDescriptor{
		Kind:      protocolIdentityIndexAddr,
		Procedure: base.Procedure,
		Name:      base.Name + "[" + index + "]",
		Slot:      -1,
		Position:  position,
		ObjectKey: key,
	})
	i.objects[key] = identity
	return identity
}

func protocolStaticIndex(value ssa.Value) (string, bool) {
	constant, ok := value.(*ssa.Const)
	if !ok || constant.Value == nil {
		return "", false
	}
	return constant.Value.ExactString(), true
}

func (i *protocolIdentityInterner) internFieldAddress(
	baseIdentity protocolIdentity,
	field int,
	fieldName string,
	position token.Pos,
) protocolIdentity {
	base, ok := i.descriptor(baseIdentity)
	if !ok || base.ObjectKey == "" {
		return 0
	}
	key := fmt.Sprintf("static-field-address|%s|%d", base.ObjectKey, field)
	if identity, exists := i.objects[key]; exists {
		return identity
	}
	if fieldName == "" {
		fieldName = fmt.Sprintf("field[%d]", field)
	}
	identity := i.allocate(protocolIdentityDescriptor{
		Kind:      protocolIdentityFieldAddr,
		Procedure: base.Procedure,
		Name:      base.Name + "." + fieldName,
		Slot:      field,
		Position:  position,
		ObjectKey: key,
	})
	i.objects[key] = identity
	return identity
}

func (i *protocolIdentityInterner) descriptor(identity protocolIdentity) (protocolIdentityDescriptor, bool) {
	descriptor, ok := i.descriptors[identity]
	return descriptor, ok
}

func (i *protocolIdentityInterner) identityIsFreshAllocation(identity protocolIdentity) bool {
	descriptor, ok := i.descriptor(identity)
	return ok && descriptor.Kind == protocolIdentityAllocation
}

func (i *protocolIdentityInterner) copySource(identity protocolIdentity) (protocolIdentity, bool) {
	source, ok := i.copySources[identity]
	return source, ok
}

func (i *protocolIdentityInterner) allocate(descriptor protocolIdentityDescriptor) protocolIdentity {
	identity := i.nextID
	i.nextID++
	descriptor.ID = identity
	i.descriptors[identity] = descriptor
	return identity
}

func protocolIdentityKindForValue(value ssa.Value) (protocolIdentityKind, int) {
	switch typed := value.(type) {
	case *ssa.Alloc:
		return protocolIdentityAllocation, -1
	case *ssa.MakeSlice:
		return protocolIdentityAllocation, -1
	case *ssa.Parameter:
		return protocolParameterKindAndSlot(typed)
	case *ssa.Extract:
		return protocolIdentityResult, typed.Index
	case *ssa.Phi:
		return protocolIdentityPhi, -1
	case *ssa.Global:
		return protocolIdentityGlobalAddr, -1
	case *ssa.FieldAddr:
		return protocolIdentityFieldAddr, typed.Field
	case *ssa.IndexAddr:
		return protocolIdentityIndexAddr, -1
	case *ssa.ChangeType, *ssa.ChangeInterface, *ssa.MakeInterface:
		return protocolIdentityCopy, -1
	default:
		return protocolIdentityValue, -1
	}
}

func protocolIdentityObjectKey(descriptor protocolIdentityDescriptor) string {
	return fmt.Sprintf(
		"%s|%s|%d|%s",
		descriptor.Kind,
		descriptor.Procedure,
		descriptor.Slot,
		descriptor.Name,
	)
}

func protocolGlobalProcedureKey(global *ssa.Global) string {
	if global == nil || global.Pkg == nil || global.Pkg.Pkg == nil {
		return "<unknown-package>:<global>"
	}
	return global.Pkg.Pkg.Path() + ":<global>"
}

func protocolStaticFieldName(baseType types.Type, field int) string {
	if baseType == nil || field < 0 {
		return ""
	}
	baseType = types.Unalias(baseType)
	if pointer, ok := baseType.Underlying().(*types.Pointer); ok {
		baseType = types.Unalias(pointer.Elem())
	}
	structure, ok := baseType.Underlying().(*types.Struct)
	if !ok || field >= structure.NumFields() {
		return ""
	}
	return structure.Field(field).Name()
}

func protocolParameterKindAndSlot(parameter *ssa.Parameter) (protocolIdentityKind, int) {
	fn := parameter.Parent()
	if fn == nil {
		return protocolIdentityParameter, -1
	}
	for idx, candidate := range fn.Params {
		if candidate != parameter {
			continue
		}
		if idx == 0 && fn.Signature != nil && fn.Signature.Recv() != nil {
			return protocolIdentityReceiver, 0
		}
		return protocolIdentityParameter, idx
	}
	return protocolIdentityParameter, -1
}

func protocolCopySource(value ssa.Value) ssa.Value {
	switch typed := value.(type) {
	case *ssa.ChangeType:
		return typed.X
	case *ssa.ChangeInterface:
		return typed.X
	case *ssa.MakeInterface:
		return typed.X
	case *ssa.Slice:
		return typed.X
	case *ssa.TypeAssert:
		return typed.X
	case *ssa.Extract:
		assertion, ok := typed.Tuple.(*ssa.TypeAssert)
		if ok && typed.Index == 0 {
			return assertion
		}
		return nil
	default:
		return nil
	}
}

func protocolProcedureKey(fn *ssa.Function) string {
	if fn == nil {
		return ""
	}
	if object := fn.Object(); object != nil {
		if key := objectKey(object); key != "" {
			return key
		}
	}
	packagePath := "<unknown-package>"
	if fn.Pkg != nil && fn.Pkg.Pkg != nil {
		packagePath = fn.Pkg.Pkg.Path()
	}
	if syntax := fn.Syntax(); syntax != nil {
		owner := syntax
		ownerKey := packagePath
		if parent := fn.Parent(); parent != nil {
			ownerKey = protocolProcedureKey(parent)
			if parentSyntax := parent.Syntax(); parentSyntax != nil {
				owner = parentSyntax
			}
		}
		return ownerKey + ".func-lit@" + semanticASTNodeKey(owner, syntax)
	}
	return packagePath + ".ssa-function@" + fn.String()
}
