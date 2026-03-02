// SPDX-License-Identifier: MPL-2.0

package constructorvalidates_method_value

import "fmt"

type Service struct {
	name string // want `struct field constructorvalidates_method_value\.Service\.name uses primitive type string`
}

func (s *Service) Validate() error {
	if s.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

// NewService should not be flagged: method-value invocation validates the
// returned type before returning.
func NewService(name string) (*Service, error) { // want `parameter "name" of constructorvalidates_method_value\.NewService uses primitive type string`
	svc := &Service{name: name}
	validateFn := svc.Validate
	if err := validateFn(); err != nil {
		return nil, err
	}
	return svc, nil
}

// NewServiceAlias should also not be flagged when the method value call goes
// through an alias variable.
func NewServiceAlias(name string) (*Service, error) { // want `parameter "name" of constructorvalidates_method_value\.NewServiceAlias uses primitive type string`
	svc := &Service{name: name}
	validateFn := svc.Validate
	alias := validateFn
	if err := alias(); err != nil {
		return nil, err
	}
	return svc, nil
}

// NewServiceMethodValueNotCalled should be flagged because Validate() is never
// invoked even though the method value is referenced.
func NewServiceMethodValueNotCalled(name string) (*Service, error) { // want `parameter "name" of constructorvalidates_method_value\.NewServiceMethodValueNotCalled uses primitive type string` `constructor constructorvalidates_method_value\.NewServiceMethodValueNotCalled returns constructorvalidates_method_value\.Service which has Validate\(\) but never calls it`
	svc := &Service{name: name}
	validateFn := svc.Validate
	_ = validateFn
	return svc, nil
}

// NewServiceMethodExpr should not be flagged when Validate is invoked via a
// method expression with explicit receiver argument.
func NewServiceMethodExpr(name string) (*Service, error) { // want `parameter "name" of constructorvalidates_method_value\.NewServiceMethodExpr uses primitive type string`
	svc := &Service{name: name}
	validateExpr := (*Service).Validate
	if err := validateExpr(svc); err != nil {
		return nil, err
	}
	return svc, nil
}

// NewServiceConditionalMethodValueCall should be flagged because Validate() is
// only invoked conditionally and does not cover every path to return.
func NewServiceConditionalMethodValueCall(name string, strict bool) (*Service, error) { // want `parameter "name" of constructorvalidates_method_value\.NewServiceConditionalMethodValueCall uses primitive type string` `constructor constructorvalidates_method_value\.NewServiceConditionalMethodValueCall returns constructorvalidates_method_value\.Service which has Validate\(\) but never calls it`
	svc := &Service{name: name}
	validateFn := svc.Validate
	if strict && validateFn() != nil {
		return nil, fmt.Errorf("invalid service")
	}
	return svc, nil
}
