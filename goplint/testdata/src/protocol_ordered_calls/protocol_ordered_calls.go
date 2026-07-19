// SPDX-License-Identifier: MPL-2.0

package protocol_ordered_calls

type Name string

func (name Name) Validate() error { return nil }

func consume(Name) {}

func combine(Name, Name) {}

func mutate(value *Name) Name {
	*value = "changed"
	return *value
}

func preserve(value *Name) Name { return *value }

func stop() Name { panic("stop") }

type transformer interface {
	Transform(*Name) Name
}

// NestedMutation is the original production false negative: the inner
// mutation must transfer before the outer consume call.
func NestedMutation(raw string) error { // want `parameter "raw" of protocol_ordered_calls\.NestedMutation uses primitive type string`
	value := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := value.Validate(); err != nil {
		return err
	}
	consume(mutate(&value))
	return nil
}

// SiblingMutation proves every sibling receives an independent ordered
// transfer; the preserving summary cannot hide the later mutation.
func SiblingMutation(raw string) error { // want `parameter "raw" of protocol_ordered_calls\.SiblingMutation uses primitive type string`
	value := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := value.Validate(); err != nil {
		return err
	}
	combine(preserve(&value), mutate(&value))
	return nil
}

// NoReturnBeforeMutation proves a terminal sibling prevents later effects and
// the continuation from becoming realizable.
func NoReturnBeforeMutation(raw string) error { // want `parameter "raw" of protocol_ordered_calls\.NoReturnBeforeMutation uses primitive type string`
	value := Name(raw)
	if err := value.Validate(); err != nil {
		return err
	}
	combine(stop(), mutate(&value))
	consume(value)
	return nil
}

// UnresolvedSibling proves a relevant unresolved effect fails closed instead
// of being skipped because another sibling call is mapped first.
func UnresolvedSibling(raw string, effect transformer) error { // want `parameter "raw" of protocol_ordered_calls\.UnresolvedSibling uses primitive type string`
	value := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := value.Validate(); err != nil {
		return err
	}
	combine(preserve(&value), effect.Transform(&value))
	consume(value)
	return nil
}
