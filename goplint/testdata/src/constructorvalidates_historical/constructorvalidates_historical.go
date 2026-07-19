// SPDX-License-Identifier: MPL-2.0

package constructorvalidates_historical

import "fmt"

type Service struct {
	name string // want `struct field constructorvalidates_historical\.Service\.name uses primitive type string`
}

func (s *Service) Validate() error {
	if s.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func NewDirect(name string) (*Service, error) { // want `parameter "name" of constructorvalidates_historical\.NewDirect uses primitive type string`
	svc := &Service{name: name}
	if err := svc.Validate(); err != nil {
		return nil, err
	}
	return svc, nil
}

func helperValidate(svc *Service) error {
	return svc.Validate()
}

func NewTransitive(name string) (*Service, error) { // want `parameter "name" of constructorvalidates_historical\.NewTransitive uses primitive type string`
	svc := &Service{name: name}
	if err := helperValidate(svc); err != nil {
		return nil, err
	}
	return svc, nil
}

func NewMissing(name string) (*Service, error) { // want `parameter "name" of constructorvalidates_historical\.NewMissing uses primitive type string` `constructor constructorvalidates_historical\.NewMissing returns constructorvalidates_historical\.Service which has Validate\(\) but never calls it`
	return &Service{name: name}, nil
}
