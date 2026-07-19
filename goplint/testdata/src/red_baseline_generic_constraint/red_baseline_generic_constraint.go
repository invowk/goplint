// SPDX-License-Identifier: MPL-2.0

package red_baseline_generic_constraint

type Name string

func (name Name) Validate() error { return nil }

type StringMethod interface {
	~string
	String() string
}

type MixedConvertible interface {
	~string | []byte
}

func MethodBearingUnsafe[T StringMethod](raw T) { // want `parameter "raw" of red_baseline_generic_constraint\.MethodBearingUnsafe uses primitive type T`
	_ = Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
}

func MixedUnsupported[T MixedConvertible](raw T) error {
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	return name.Validate()
}
