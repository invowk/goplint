// SPDX-License-Identifier: MPL-2.0

package post_validation_escapes

type Name string

func (name Name) Validate() error { return nil }

func useName(Name) {}

type holder struct {
	pointer *Name
	value   Name
}

var packagePointer *Name

var storedClosure func()

func PointerChannel(out chan<- *Name, raw string) error { // want `parameter "raw" of post_validation_escapes\.PointerChannel uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	out <- &name
	useName(name)
	return nil
}

func AggregatePointer(target *holder, raw string) error { // want `parameter "raw" of post_validation_escapes\.AggregatePointer uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	target.pointer = &name
	useName(name)
	return nil
}

func IndirectPointer(target **Name, raw string) error { // want `parameter "raw" of post_validation_escapes\.IndirectPointer uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	*target = &name
	useName(name)
	return nil
}

func PackagePointer(raw string) error { // want `parameter "raw" of post_validation_escapes\.PackagePointer uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	packagePointer = &name
	useName(name)
	return nil
}

func StoredClosure(raw string) error { // want `parameter "raw" of post_validation_escapes\.StoredClosure uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	storedClosure = func() { name = Name("changed") }
	useName(name)
	return nil
}

func DeferredClosure(raw string) error { // want `parameter "raw" of post_validation_escapes\.DeferredClosure uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	defer func() { useName(name) }()
	useName(name)
	return nil
}

func storePointer(target **Name, name *Name) {
	*target = name
}

func HelperPointer(target **Name, raw string) error { // want `parameter "raw" of post_validation_escapes\.HelperPointer uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	storePointer(target, &name)
	useName(name)
	return nil
}

func recursiveStore(target **Name, name *Name, depth int) { // want `parameter "depth" of post_validation_escapes\.recursiveStore uses primitive type int`
	if depth <= 0 {
		*target = name
		return
	}
	recursiveStore(target, name, depth-1)
}

func RecursivePointer(target **Name, raw string, depth int) error { // want `parameter "raw" of post_validation_escapes\.RecursivePointer uses primitive type string` `parameter "depth" of post_validation_escapes\.RecursivePointer uses primitive type int`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	recursiveStore(target, &name, depth)
	useName(name)
	return nil
}

func ImmutableChannel(out chan<- Name, raw string) error { // want `parameter "raw" of post_validation_escapes\.ImmutableChannel uses primitive type string`
	name := Name(raw)
	if err := name.Validate(); err != nil {
		return err
	}
	out <- name
	useName(name)
	return nil
}

func ImmutableAggregate(target *holder, raw string) error { // want `parameter "raw" of post_validation_escapes\.ImmutableAggregate uses primitive type string`
	name := Name(raw)
	if err := name.Validate(); err != nil {
		return err
	}
	target.value = name
	useName(name)
	return nil
}

func ProtectedUseBeforeEscape(target **Name, raw string) error { // want `parameter "raw" of post_validation_escapes\.ProtectedUseBeforeEscape uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	if err := name.Validate(); err != nil {
		return err
	}
	useName(name)
	*target = &name
	return nil
}
