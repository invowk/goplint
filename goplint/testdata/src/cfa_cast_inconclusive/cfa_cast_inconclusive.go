// SPDX-License-Identifier: MPL-2.0

package cfa_cast_inconclusive

type CommandName string

func (c CommandName) Validate() error {
	return nil
}

func Inconclusive(input string) { // want `parameter "input" of cfa_cast_inconclusive\.Inconclusive uses primitive type string`
	v := CommandName(input) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	if len(input) > 0 {
		_ = 1
	}
	_ = v
}
