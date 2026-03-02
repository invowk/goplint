// SPDX-License-Identifier: MPL-2.0

package castvalidation_nocfa_validate_before_cast

type CommandName string

func (c CommandName) Validate() error { return nil }

func consume(_ CommandName) {}

func ValidateBeforeCast(raw string) { // want `parameter "raw" of castvalidation_nocfa_validate_before_cast\.ValidateBeforeCast uses primitive type string`
	var x CommandName
	_ = x.Validate()
	x = CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	consume(x)
}
