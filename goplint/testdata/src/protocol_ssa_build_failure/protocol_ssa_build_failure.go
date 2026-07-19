// SPDX-License-Identifier: MPL-2.0

package protocol_ssa_build_failure

type Name string

func (n Name) Validate() error { return nil }

func Build(raw string) error { // want `parameter "raw" of protocol_ssa_build_failure\.Build uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis` `variable name of type Name has inconclusive use-before-validate path analysis`
	return name.Validate()
}

type Config struct{}

func (c *Config) Validate() error { return nil }

func NewConfig() (*Config, error) { // want `constructor protocol_ssa_build_failure\.NewConfig returns protocol_ssa_build_failure\.Config with inconclusive Validate\(\) path analysis`
	config := &Config{}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return config, nil
}

type CreateRequest struct{}

func (r CreateRequest) Validate() error { return nil }

func Handle(request CreateRequest) error { // want `parameter "request" of protocol_ssa_build_failure\.CreateRequest has inconclusive checked Validate\(\) analysis at exported boundary`
	return request.Validate()
}
