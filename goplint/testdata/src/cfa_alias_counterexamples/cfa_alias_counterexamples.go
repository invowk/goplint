// SPDX-License-Identifier: MPL-2.0

package cfa_alias_counterexamples

type CommandName string

func (name CommandName) Validate() error { return nil }

type holder struct {
	name CommandName
}

type mutator interface {
	Mutate(*CommandName)
}

func preserve(value *CommandName) { _ = value }

var retainedClosure func()

func MustCopy(raw string) error { // want `parameter "raw" of cfa_alias_counterexamples\.MustCopy uses primitive type string`
	name := CommandName(raw)
	alias := name
	return alias.Validate()
}

func AmbiguousPhi(raw string, choose bool) error { // want `parameter "raw" of cfa_alias_counterexamples\.AmbiguousPhi uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	other := CommandName("other")
	alias := name
	if choose {
		alias = name
	} else {
		alias = other
	}
	return alias.Validate()
}

func IrrelevantPhi(raw string, choose bool) error { // want `parameter "raw" of cfa_alias_counterexamples\.IrrelevantPhi uses primitive type string`
	name := CommandName(raw)
	left := CommandName("left")
	right := CommandName("right")
	alias := left
	if choose {
		alias = left
	} else {
		alias = right
	}
	_ = alias.Validate()
	return name.Validate()
}

func StaticIndex(raw string) error { // want `parameter "raw" of cfa_alias_counterexamples\.StaticIndex uses primitive type string`
	items := []CommandName{"zero", "one"}
	items[0] = CommandName(raw)
	return items[0].Validate()
}

func DynamicIndex(raw string, index int) error { // want `parameter "raw" of cfa_alias_counterexamples\.DynamicIndex uses primitive type string` `parameter "index" of cfa_alias_counterexamples\.DynamicIndex uses primitive type int`
	items := []CommandName{"zero", "one"}
	items[index] = CommandName(raw) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	index = 1
	return items[index].Validate()
}

func DirectPointer(raw string) error { // want `parameter "raw" of cfa_alias_counterexamples\.DirectPointer uses primitive type string`
	name := CommandName(raw)
	pointer := &name
	return (*pointer).Validate()
}

func AmbiguousPointer(raw1, raw2 string, choose bool) error { // want `parameter "raw1" of cfa_alias_counterexamples\.AmbiguousPointer uses primitive type string` `parameter "raw2" of cfa_alias_counterexamples\.AmbiguousPointer uses primitive type string`
	name := CommandName(raw1)  // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	other := CommandName(raw2) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	pointer := &name
	if choose {
		pointer = &name
	} else {
		pointer = &other
	}
	return (*pointer).Validate()
}

func InterfaceRoundTrip(raw string) error { // want `parameter "raw" of cfa_alias_counterexamples\.InterfaceRoundTrip uses primitive type string`
	name := CommandName(raw)
	var boxed any = name
	asserted := boxed.(CommandName)
	return asserted.Validate()
}

func AmbiguousInterface(raw1, raw2 string, choose bool) error { // want `parameter "raw1" of cfa_alias_counterexamples\.AmbiguousInterface uses primitive type string` `parameter "raw2" of cfa_alias_counterexamples\.AmbiguousInterface uses primitive type string`
	name := CommandName(raw1)  // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	other := CommandName(raw2) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	var boxed any
	if choose {
		boxed = name
	} else {
		boxed = other
	}
	asserted := boxed.(CommandName)
	return asserted.Validate()
}

func ClosureLocalAmbiguity(raw1, raw2 string, choose bool) error { // want `parameter "raw1" of cfa_alias_counterexamples\.ClosureLocalAmbiguity uses primitive type string` `parameter "raw2" of cfa_alias_counterexamples\.ClosureLocalAmbiguity uses primitive type string`
	return func() error {
		name := CommandName(raw1)  // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
		other := CommandName(raw2) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
		alias := name
		if choose {
			alias = name
		} else {
			alias = other
		}
		return alias.Validate()
	}()
}

func StoredCopy(raw string) error { // want `parameter "raw" of cfa_alias_counterexamples\.StoredCopy uses primitive type string`
	name := CommandName(raw)
	slot := new(CommandName)
	*slot = name
	return (*slot).Validate()
}

func AmbiguousStoredCopy(raw1, raw2 string, choose bool) error { // want `parameter "raw1" of cfa_alias_counterexamples\.AmbiguousStoredCopy uses primitive type string` `parameter "raw2" of cfa_alias_counterexamples\.AmbiguousStoredCopy uses primitive type string`
	name := CommandName(raw1)  // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	other := CommandName(raw2) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	stored := name
	if choose {
		stored = name
	} else {
		stored = other
	}
	slot := new(CommandName)
	*slot = stored
	return (*slot).Validate()
}

func EscapedPointer(raw string, sink chan *CommandName) error { // want `parameter "raw" of cfa_alias_counterexamples\.EscapedPointer uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	sink <- &name
	return name.Validate()
}

func IrrelevantPointerEscape(raw string, sink chan *CommandName) error { // want `parameter "raw" of cfa_alias_counterexamples\.IrrelevantPointerEscape uses primitive type string`
	name := CommandName(raw)
	other := CommandName("other")
	sink <- &other
	return name.Validate()
}

func EscapedClosure(raw string) error { // want `parameter "raw" of cfa_alias_counterexamples\.EscapedClosure uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	retainedClosure = func() { _ = name }
	return name.Validate()
}

func ImmediateCapture(raw string) error { // want `parameter "raw" of cfa_alias_counterexamples\.ImmediateCapture uses primitive type string`
	name := CommandName(raw)
	func() { _ = name }()
	return name.Validate()
}

func StableSelector(raw string) error { // want `parameter "raw" of cfa_alias_counterexamples\.StableSelector uses primitive type string`
	target := &holder{}
	target.name = CommandName(raw)
	return target.name.Validate()
}

func RebasedSelector(raw string) error { // want `parameter "raw" of cfa_alias_counterexamples\.RebasedSelector uses primitive type string`
	first := &holder{}
	second := &holder{}
	target := first
	target.name = CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	target = second
	return target.name.Validate()
}

func RebasedStaticIndex(raw string) error { // want `parameter "raw" of cfa_alias_counterexamples\.RebasedStaticIndex uses primitive type string`
	first := []CommandName{"first"}
	second := []CommandName{"second"}
	target := first
	target[0] = CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	target = second
	return target[0].Validate()
}

func AmbiguousPostValidationEffect(value mutator, raw string, choose bool) error { // want `parameter "raw" of cfa_alias_counterexamples\.AmbiguousPostValidationEffect uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	other := CommandName("other")
	target := &name
	if choose {
		target = &name
	} else {
		target = &other
	}
	value.Mutate(target)
	return nil
}

func IrrelevantPostValidationEffect(value mutator, raw string, choose bool) error { // want `parameter "raw" of cfa_alias_counterexamples\.IrrelevantPostValidationEffect uses primitive type string`
	name := CommandName(raw)
	if err := name.Validate(); err != nil {
		return err
	}
	left := CommandName("left")
	right := CommandName("right")
	target := &left
	if choose {
		target = &left
	} else {
		target = &right
	}
	value.Mutate(target)
	return nil
}

func AmbiguousPreservingEffect(raw string, choose bool) error { // want `parameter "raw" of cfa_alias_counterexamples\.AmbiguousPreservingEffect uses primitive type string`
	name := CommandName(raw)
	if err := name.Validate(); err != nil {
		return err
	}
	other := CommandName("other")
	target := &name
	if choose {
		target = &name
	} else {
		target = &other
	}
	preserve(target)
	return nil
}
