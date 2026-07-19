// SPDX-License-Identifier: MPL-2.0

package constructorvalidates_deferred

type Value struct{}

func (*Value) Validate() error { return nil }

type deferredEffect interface {
	Apply(*Value, *error)
}

// NewUnconditional is the positive control: its exact validation result is
// preserved in the named error result at exit.
func NewUnconditional() (value *Value, err error) {
	value = &Value{}
	defer func() {
		validationErr := value.Validate()
		if err == nil {
			err = validationErr
		}
	}()
	return value, nil
}

// NewConditional can skip validation on a successful return.
func NewConditional(validate bool) (value *Value, err error) { // want `constructor constructorvalidates_deferred\.NewConditional returns constructorvalidates_deferred\.Value with inconclusive Validate\(\) path analysis`
	value = &Value{}
	defer func() {
		if validate {
			validationErr := value.Validate()
			if err == nil {
				err = validationErr
			}
		}
	}()
	return value, nil
}

// NewValidationThenOverwrite registers the overwrite first, so LIFO exit
// execution validates and then disconnects the returned error relation.
func NewValidationThenOverwrite() (value *Value, err error) { // want `constructor constructorvalidates_deferred\.NewValidationThenOverwrite returns constructorvalidates_deferred\.Value with inconclusive Validate\(\) path analysis`
	value = &Value{}
	defer func() { err = nil }()
	defer func() {
		validationErr := value.Validate()
		if err == nil {
			err = validationErr
		}
	}()
	return value, nil
}

// NewMultiplePreserved is a positive LIFO control: the later effect observes
// but does not overwrite the exact validation relation.
func NewMultiplePreserved() (value *Value, err error) {
	value = &Value{}
	defer func() { _ = err }()
	defer func() {
		validationErr := value.Validate()
		if err == nil {
			err = validationErr
		}
	}()
	return value, nil
}

// NewUnresolved cannot conservatively summarize the deferred interface call.
func NewUnresolved(effect deferredEffect) (value *Value, err error) { // want `constructor constructorvalidates_deferred\.NewUnresolved returns constructorvalidates_deferred\.Value with inconclusive Validate\(\) path analysis`
	value = &Value{}
	defer effect.Apply(value, &err)
	return value, nil
}

// NewCaptureRebound validates a different captured allocation at exit.
func NewCaptureRebound() (result *Value, err error) { // want `constructor constructorvalidates_deferred\.NewCaptureRebound returns constructorvalidates_deferred\.Value with inconclusive Validate\(\) path analysis`
	value := &Value{}
	result = value
	defer func() {
		validationErr := value.Validate()
		if err == nil {
			err = validationErr
		}
	}()
	value = &Value{}
	return result, nil
}
