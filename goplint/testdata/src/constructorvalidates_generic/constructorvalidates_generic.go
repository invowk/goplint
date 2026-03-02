// SPDX-License-Identifier: MPL-2.0

package constructorvalidates_generic

type Box[T any] struct {
	Value T
}

func (b Box[T]) Validate() error {
	return nil
}

func validateStringBox(b Box[string]) error {
	return b.Validate()
}

func NewStringBox(raw string) (Box[string], error) { // want `parameter "raw" of constructorvalidates_generic\.NewStringBox uses primitive type string`
	b := Box[string]{Value: raw}
	if err := validateStringBox(b); err != nil {
		return Box[string]{}, err
	}
	return b, nil
}

func NewIntBox(raw int) (Box[int], error) { // want `parameter "raw" of constructorvalidates_generic\.NewIntBox uses primitive type int` `constructor constructorvalidates_generic\.NewIntBox returns constructorvalidates_generic\.Box which has Validate\(\) but never calls it`
	b := Box[int]{Value: raw}
	if err := validateStringBox(Box[string]{Value: "ok"}); err != nil {
		return Box[int]{}, err
	}
	return b, nil
}
