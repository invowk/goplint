// SPDX-License-Identifier: MPL-2.0

package util

type Value[T ~string] struct {
	Raw T
}

func (value *Value[T]) Validate() error { return nil }

func ValidateValue[T ~string](value *Value[T]) error { // want ValidateValue:"protocol-summary:v5:protocol_generic_cross/util:protocol_generic_cross/util.ValidateValue:1"
	return value.Validate()
}

func NewValue[T ~string](raw T) (*Value[T], error) { // want NewValue:"protocol-summary:v5:protocol_generic_cross/util:protocol_generic_cross/util.NewValue:1"
	value := &Value[T]{Raw: raw}
	return value, value.Validate()
}
