// SPDX-License-Identifier: MPL-2.0

package validatedelegation_helper_function

import "errors"

type Name string

func (n Name) Validate() error {
	if n == "" {
		return errors.New("empty")
	}
	return nil
}

type Child struct {
	Name Name
}

func (c *Child) Validate() error {
	if c == nil {
		return errors.New("nil")
	}
	return c.Name.Validate()
}

func appendFieldError(errs *[]error, err error) {
	if err != nil {
		*errs = append(*errs, err)
	}
}

func appendOptionalValidation[T interface{ Validate() error }](errs *[]error, value T, present bool) {
	if present {
		appendFieldError(errs, value.Validate())
	}
}

func appendEachValidation[T interface{ Validate() error }](errs *[]error, values []T) {
	for i := range values {
		appendFieldError(errs, values[i].Validate())
	}
}

//goplint:validate-all
type CompleteConfig struct {
	FieldName Name
	Items     []Name
}

func (c CompleteConfig) Validate() error {
	var errs []error
	appendOptionalValidation(&errs, c.FieldName, c.FieldName != "")
	appendEachValidation(&errs, c.Items)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

//goplint:validate-all
type GuardedConfig struct {
	FieldName Name
	Child     *Child
}

func (c GuardedConfig) Validate() error {
	var errs []error
	appendOptionalValidation(&errs, c.FieldName, c.FieldName != "")
	appendOptionalValidation(&errs, c.Child, c.Child != nil)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

//goplint:validate-all
type DirectGuardConfig struct {
	FieldName Name
	Child     *Child
}

func (c DirectGuardConfig) Validate() error {
	var errs []error
	if c.FieldName != "" {
		errs = append(errs, c.FieldName.Validate())
	}
	if c.Child != nil {
		errs = append(errs, c.Child.Validate())
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

//goplint:validate-all
type IncompleteConfig struct { // want `validatedelegation_helper_function\.IncompleteConfig\.Validate\(\) does not delegate to field FieldMode which has Validate\(\)`
	FieldName Name
	FieldMode Name
}

func (c IncompleteConfig) Validate() error {
	var errs []error
	appendOptionalValidation(&errs, c.FieldName, c.FieldName != "")
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
