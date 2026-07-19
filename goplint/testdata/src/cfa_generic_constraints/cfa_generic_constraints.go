// SPDX-License-Identifier: MPL-2.0

package cfa_generic_constraints

type CommandName string

func (c CommandName) Validate() error { return nil }

type StringMethod interface {
	~string
	String() string
}

type MixedConvertible interface {
	~string | []byte
}

func MethodBearingUnsafe[T StringMethod](raw T) { // want `parameter "raw" of cfa_generic_constraints\.MethodBearingUnsafe uses primitive type T`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = name
}

func MethodBearingSafe[T StringMethod](raw T) error { // want `parameter "raw" of cfa_generic_constraints\.MethodBearingSafe uses primitive type T`
	name := CommandName(raw)
	return name.Validate()
}

func MixedAssigned[T MixedConvertible](raw T) error {
	name := CommandName(raw) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	return name.Validate()
}

func MixedUnassigned[T MixedConvertible](raw T) {
	_ = CommandName(raw) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
}
