// SPDX-License-Identifier: MPL-2.0

package validatedelegation_var_alias

import "fmt"

type Name string

func (n Name) Validate() error {
	if n == "" {
		return fmt.Errorf("empty")
	}
	return nil
}

//goplint:validate-all
type Config struct {
	Field Name
}

func (c *Config) Validate() error {
	var alias = c.Field
	return alias.Validate()
}
