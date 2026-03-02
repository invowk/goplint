// SPDX-License-Identifier: MPL-2.0

package validatedelegation_helper_cycle

import "fmt"

type Name string

func (n Name) Validate() error {
	if n == "" {
		return fmt.Errorf("empty")
	}
	return nil
}

//goplint:validate-all
type Config struct { // want `validatedelegation_helper_cycle\.Config\.Validate\(\) does not delegate to field Name which has Validate\(\)`
	Name Name
}

func (c Config) Validate() error {
	return c.stepOne()
}

func (c Config) stepOne() error {
	return c.stepTwo()
}

func (c Config) stepTwo() error {
	return c.stepOne()
}
