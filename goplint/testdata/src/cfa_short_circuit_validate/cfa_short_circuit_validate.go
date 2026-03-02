// SPDX-License-Identifier: MPL-2.0

package cfa_short_circuit_validate

type CommandName string

func (c CommandName) Validate() error { return nil }

func consume(_ CommandName) {}

func ConditionalValidate(raw string, cond bool) { // want `parameter "raw" of cfa_short_circuit_validate\.ConditionalValidate uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	if cond && x.Validate() == nil {
		// Validate() is conditionally executed due to short-circuit.
	}
	consume(x)
}
