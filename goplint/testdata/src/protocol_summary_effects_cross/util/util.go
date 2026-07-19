// SPDX-License-Identifier: MPL-2.0

package util

type Value string

func (value *Value) Validate() error { return nil }

func Pure(value *Value) error { // want Pure:"protocol-summary:v5:protocol_summary_effects_cross/util:protocol_summary_effects_cross/util.Pure:1"
	return nil
}

func Preserve(value *Value) error { // want Preserve:"protocol-summary:v5:protocol_summary_effects_cross/util:protocol_summary_effects_cross/util.Preserve:1"
	_ = value
	return nil
}

func Mutate(value *Value) error { // want Mutate:"protocol-summary:v5:protocol_summary_effects_cross/util:protocol_summary_effects_cross/util.Mutate:1"
	*value = "changed"
	return nil
}

func Replace(value, source *Value) error { // want Replace:"protocol-summary:v5:protocol_summary_effects_cross/util:protocol_summary_effects_cross/util.Replace:2"
	*value = *source
	return nil
}

var escaped *Value

func Escape(value *Value) error { // want Escape:"protocol-summary:v5:protocol_summary_effects_cross/util:protocol_summary_effects_cross/util.Escape:1"
	escaped = value
	return nil
}

func consumeValue(value *Value) { _ = len(*value) }

func Consume(value *Value) error { // want Consume:"protocol-summary:v5:protocol_summary_effects_cross/util:protocol_summary_effects_cross/util.Consume:1"
	consumeValue(value)
	return nil
}

func MutateThenValidate(value *Value) error { // want MutateThenValidate:"protocol-summary:v5:protocol_summary_effects_cross/util:protocol_summary_effects_cross/util.MutateThenValidate:2"
	*value = "changed"
	return value.Validate()
}

func Terminal(value *Value) error { // want Terminal:"protocol-summary:v5:protocol_summary_effects_cross/util:protocol_summary_effects_cross/util.Terminal:1"
	panic(value)
}
