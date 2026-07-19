// SPDX-License-Identifier: MPL-2.0

package protocol_unknown_effects

import (
	"reflect"
	"unsafe"

	external "protocol_unknown_effects_external"
)

type Name string

func (n Name) Validate() error { return nil }

func UnresolvedMutation(raw string) error { // want `parameter "raw" of protocol_unknown_effects\.UnresolvedMutation uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	external.Apply(&name)
	return nil
}

func Replacement(raw string) error { // want `parameter "raw" of protocol_unknown_effects\.Replacement uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	external.Replace(&name, Name("replacement"))
	return nil
}

func Escape(raw string) error { // want `parameter "raw" of protocol_unknown_effects\.Escape uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	external.Retain(&name)
	return nil
}

func ConcurrentMutation(raw string) error { // want `parameter "raw" of protocol_unknown_effects\.ConcurrentMutation uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	go external.Apply(&name)
	return nil
}

func ReflectionMutation(raw string) error { // want `parameter "raw" of protocol_unknown_effects\.ReflectionMutation uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	reflect.ValueOf(&name).Elem().SetString("changed")
	return nil
}

func UnsafeAccess(raw string) error { // want `parameter "raw" of protocol_unknown_effects\.UnsafeAccess uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	_ = unsafe.Pointer(&name)
	return nil
}

type mutator interface {
	Apply(*Name)
}

func InterfaceDispatch(value mutator, raw string) error { // want `parameter "raw" of protocol_unknown_effects\.InterfaceDispatch uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	value.Apply(&name)
	return nil
}

var escapedName *Name

func EscapedHeapMutation(raw string) error { // want `parameter "raw" of protocol_unknown_effects\.EscapedHeapMutation uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	escapedName = &name
	return nil
}

func MutationThenRevalidate(raw string) error { // want `parameter "raw" of protocol_unknown_effects\.MutationThenRevalidate uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	external.Apply(&name)
	if err := name.Validate(); err != nil {
		return err
	}
	return nil
}

func ViolationOutranksUnknown(raw string, unknownBranch bool) error { // want `parameter "raw" of protocol_unknown_effects\.ViolationOutranksUnknown uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
	if unknownBranch {
		if err := name.Validate(); err != nil {
			return err
		}
		external.Apply(&name)
	}
	return nil
}

func IrrelevantMutation(raw string) error { // want `parameter "raw" of protocol_unknown_effects\.IrrelevantMutation uses primitive type string`
	name := Name(raw)
	if err := name.Validate(); err != nil {
		return err
	}
	unrelated := Name("unrelated")
	external.Apply(&unrelated)
	return nil
}

func UnreachableMutation(raw string) error { // want `parameter "raw" of protocol_unknown_effects\.UnreachableMutation uses primitive type string`
	name := Name(raw)
	if err := name.Validate(); err != nil {
		return err
	}
	return nil
	external.Apply(&name)
	return nil
}
