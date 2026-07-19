// SPDX-License-Identifier: MPL-2.0

package castvalidation_dead_branch

type CommandName string

func (c CommandName) Validate() error { return nil }

func consume(_ CommandName) {}

// DeadBranchValidate ensures an unreachable Validate call cannot discharge the
// live validation obligation.
func DeadBranchValidate(raw string) { // want `parameter "raw" of castvalidation_dead_branch\.DeadBranchValidate uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	if false {
		_ = x.Validate()
	}
	consume(x)
}
