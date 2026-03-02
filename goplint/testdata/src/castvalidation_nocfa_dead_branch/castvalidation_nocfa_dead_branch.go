// SPDX-License-Identifier: MPL-2.0

package castvalidation_nocfa_dead_branch

type CommandName string

func (c CommandName) Validate() error { return nil }

func consume(_ CommandName) {}

// DeadBranchValidate documents AST fallback behavior: --no-cfa mode only
// checks for presence of Validate() in the function body, so this case is not
// flagged even though the call is unreachable.
func DeadBranchValidate(raw string) { // want `parameter "raw" of castvalidation_nocfa_dead_branch\.DeadBranchValidate uses primitive type string`
	x := CommandName(raw)
	if false {
		_ = x.Validate()
	}
	consume(x)
}
