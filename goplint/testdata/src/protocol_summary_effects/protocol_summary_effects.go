// SPDX-License-Identifier: MPL-2.0

package protocol_summary_effects

type Value string

func (value *Value) Validate() error { return nil }

func (value *Value) String() string { return string(*value) }

func Pure(value *Value) error { // want Pure:"protocol-summary:v5:protocol_summary_effects:protocol_summary_effects.Pure:1"
	return nil
}

func Preserve(value *Value) error { // want Preserve:"protocol-summary:v5:protocol_summary_effects:protocol_summary_effects.Preserve:1"
	_ = value
	return nil
}

func ConditionalValidate(value *Value) error { // want ConditionalValidate:"protocol-summary:v5:protocol_summary_effects:protocol_summary_effects.ConditionalValidate:1"
	return value.Validate()
}

func MutateThenValidate(value *Value) error { // want MutateThenValidate:"protocol-summary:v5:protocol_summary_effects:protocol_summary_effects.MutateThenValidate:2"
	*value = "changed"
	return value.Validate()
}

func Mutate(value *Value) error { // want Mutate:"protocol-summary:v5:protocol_summary_effects:protocol_summary_effects.Mutate:1"
	*value = "changed"
	return nil
}

func Replace(value, source *Value) error { // want Replace:"protocol-summary:v5:protocol_summary_effects:protocol_summary_effects.Replace:2"
	*value = *source
	return nil
}

var escaped *Value

func Escape(value *Value) error { // want Escape:"protocol-summary:v5:protocol_summary_effects:protocol_summary_effects.Escape:1"
	escaped = value
	return nil
}

func consumeValue(value *Value) { _ = len(*value) }

func Consume(value *Value) error { // want Consume:"protocol-summary:v5:protocol_summary_effects:protocol_summary_effects.Consume:1"
	consumeValue(value)
	return nil
}

func Terminal(_ *Value) error { // want Terminal:"protocol-summary:v5:protocol_summary_effects:protocol_summary_effects.Terminal:1"
	panic("stop")
}

func NewDiscardedConditional(raw string) (*Value, error) { // want `parameter "raw" of protocol_summary_effects\.NewDiscardedConditional uses primitive type string` `constructor protocol_summary_effects\.NewDiscardedConditional returns protocol_summary_effects\.Value which has Validate\(\) but never calls it`
	value := Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	_ = MutateThenValidate(&value)
	return &value, nil
}

func NewOverwrittenConditional(raw string) (*Value, error) { // want `parameter "raw" of protocol_summary_effects\.NewOverwrittenConditional uses primitive type string` `constructor protocol_summary_effects\.NewOverwrittenConditional returns protocol_summary_effects\.Value which has Validate\(\) but never calls it`
	value := Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	err := MutateThenValidate(&value)
	err = nil
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func NewMismatchedConditional(raw string) (*Value, error) { // want `parameter "raw" of protocol_summary_effects\.NewMismatchedConditional uses primitive type string` `constructor protocol_summary_effects\.NewMismatchedConditional returns protocol_summary_effects\.Value which has Validate\(\) but never calls it`
	value := Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	err := MutateThenValidate(&value)
	other := error(nil)
	if other != nil {
		return nil, other
	}
	_ = err
	return &value, nil
}

func NewCheckedConditional(raw string) (*Value, error) { // want `parameter "raw" of protocol_summary_effects\.NewCheckedConditional uses primitive type string`
	value := Value(raw)
	if err := MutateThenValidate(&value); err != nil {
		return nil, err
	}
	return &value, nil
}

func NewAliasedConditional(raw string) (*Value, error) { // want `parameter "raw" of protocol_summary_effects\.NewAliasedConditional uses primitive type string`
	value := Value(raw)
	err := MutateThenValidate(&value)
	alias := err
	if alias != nil {
		return nil, alias
	}
	return &value, nil
}
