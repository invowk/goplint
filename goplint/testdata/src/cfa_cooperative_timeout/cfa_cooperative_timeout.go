// SPDX-License-Identifier: MPL-2.0

package cfa_cooperative_timeout

type CommandName string

func (c CommandName) Validate() error {
	return nil
}

func Deadline(input string) { // want `parameter "input" of cfa_cooperative_timeout\.Deadline uses primitive type string`
	value := CommandName(input) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	_ = value
}
