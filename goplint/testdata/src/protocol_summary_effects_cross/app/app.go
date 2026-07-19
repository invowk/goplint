// SPDX-License-Identifier: MPL-2.0

package app

import "protocol_summary_effects_cross/util"

func NewMutated(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewMutated uses primitive type string` `constructor app\.NewMutated returns util\.Value which has Validate\(\) but never calls it`
	value := util.Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	_ = util.Mutate(&value)
	return &value, nil
}

func NewReplaced(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewReplaced uses primitive type string` `constructor app\.NewReplaced returns util\.Value which has Validate\(\) but never calls it`
	value := util.Value(raw)
	other := util.Value("other")
	if err := value.Validate(); err != nil {
		return nil, err
	}
	_ = util.Replace(&value, &other)
	return &value, nil
}

func NewEscaped(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewEscaped uses primitive type string` `constructor app\.NewEscaped returns util\.Value with inconclusive Validate\(\) path analysis`
	value := util.Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	_ = util.Escape(&value)
	return &value, nil
}

func NewRevalidated(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewRevalidated uses primitive type string`
	value := util.Value(raw)
	if err := util.MutateThenValidate(&value); err != nil {
		return nil, err
	}
	return &value, nil
}

func NewAliasedRevalidation(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewAliasedRevalidation uses primitive type string`
	value := util.Value(raw)
	err := util.MutateThenValidate(&value)
	alias := err
	if alias != nil {
		return nil, alias
	}
	return &value, nil
}

func NewDiscardedRevalidation(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewDiscardedRevalidation uses primitive type string` `constructor app\.NewDiscardedRevalidation returns util\.Value which has Validate\(\) but never calls it`
	value := util.Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	_ = util.MutateThenValidate(&value)
	return &value, nil
}

func NewOverwrittenRevalidation(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewOverwrittenRevalidation uses primitive type string` `constructor app\.NewOverwrittenRevalidation returns util\.Value which has Validate\(\) but never calls it`
	value := util.Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	err := util.MutateThenValidate(&value)
	err = nil
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func NewMismatchedRevalidation(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewMismatchedRevalidation uses primitive type string` `constructor app\.NewMismatchedRevalidation returns util\.Value which has Validate\(\) but never calls it`
	value := util.Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	err := util.MutateThenValidate(&value)
	other := error(nil)
	if other != nil {
		return nil, other
	}
	_ = err
	return &value, nil
}

func NewPure(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewPure uses primitive type string`
	value := util.Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	_ = util.Pure(&value)
	return &value, nil
}

func NewPreserved(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewPreserved uses primitive type string`
	value := util.Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	_ = util.Preserve(&value)
	return &value, nil
}

func NewConsumed(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewConsumed uses primitive type string`
	value := util.Value(raw)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	_ = util.Consume(&value)
	return &value, nil
}

func NewTerminal(raw string) (*util.Value, error) { // want `parameter "raw" of app\.NewTerminal uses primitive type string`
	value := util.Value(raw)
	_ = util.Terminal(&value)
	return &value, nil
}
