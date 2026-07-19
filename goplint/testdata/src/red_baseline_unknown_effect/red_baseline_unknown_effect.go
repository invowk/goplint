// SPDX-License-Identifier: MPL-2.0

package red_baseline_unknown_effect

type Name string

func (name Name) Validate() error { return nil }

type mutator interface {
	Mutate(*Name)
}

func PostValidationMutation(value mutator, raw string) error { // want `parameter "raw" of red_baseline_unknown_effect\.PostValidationMutation uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	value.Mutate(&name)
	return nil
}
