// SPDX-License-Identifier: MPL-2.0

package red_baseline_constructor_identity

type Service struct{}

func (*Service) Validate() error { return nil }

func NewHistoricallyRebound() (*Service, error) { // want `constructor red_baseline_constructor_identity\.NewHistoricallyRebound returns red_baseline_constructor_identity\.Service which has Validate\(\) but never calls it`
	first := &Service{}
	result := first
	if err := result.Validate(); err != nil {
		return nil, err
	}
	result = &Service{}
	return result, nil
}

func NewDecoyValidated() (*Service, error) { // want `constructor red_baseline_constructor_identity\.NewDecoyValidated returns red_baseline_constructor_identity\.Service which has Validate\(\) but never calls it`
	decoy := &Service{}
	if err := decoy.Validate(); err != nil {
		return nil, err
	}
	return &Service{}, nil
}

func NewFreshLiteralAfterValidation() (*Service, error) { // want `constructor red_baseline_constructor_identity\.NewFreshLiteralAfterValidation returns red_baseline_constructor_identity\.Service which has Validate\(\) but never calls it`
	validated := &Service{}
	if err := validated.Validate(); err != nil {
		return nil, err
	}
	return &Service{}, nil
}
