// SPDX-License-Identifier: MPL-2.0

package castvalidation_nocfa_suppression

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid")
	}
	return nil
}

func ConfigSuppressed(input string) { // want `parameter "input" of castvalidation_nocfa_suppression\.ConfigSuppressed uses primitive type string`
	_ = CommandName(input)
}

func BaselineSuppressed(input string) { // want `parameter "input" of castvalidation_nocfa_suppression\.BaselineSuppressed uses primitive type string`
	_ = CommandName(input)
}

func Reported(input string) { // want `parameter "input" of castvalidation_nocfa_suppression\.Reported uses primitive type string`
	_ = CommandName(input) // want `type conversion to CommandName from non-constant without Validate\(\) check`
}
