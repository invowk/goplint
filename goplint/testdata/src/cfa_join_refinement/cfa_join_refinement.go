// SPDX-License-Identifier: MPL-2.0

package cfa_join_refinement

type CommandName string

func (name CommandName) Validate() error { return nil }

func useCommand(CommandName) {}

// JoinedInfeasibleValidation joins an infeasible validated contribution with
// feasible unvalidated contributions at the protected use.
func JoinedInfeasibleValidation(raw string) { // want `parameter "raw" of cfa_join_refinement\.JoinedInfeasibleValidation uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	if raw == "" {
		if raw != "" {
			if err := name.Validate(); err != nil {
				return
			}
		}
	}
	useCommand(name)
}

// MultipleIncomparableWitnesses requires refinement to discharge the first
// contradictory witness without losing the later feasible unsafe witness.
func MultipleIncomparableWitnesses(raw string) { // want `parameter "raw" of cfa_join_refinement\.MultipleIncomparableWitnesses uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	if raw == "" {
		if raw != "" {
			useCommand(name)
			return
		}
	}
	if raw == "live" {
		useCommand(name)
		return
	}
	if err := name.Validate(); err != nil {
		return
	}
}

func collisionHelper(name CommandName, raw string) { // want `parameter "raw" of cfa_join_refinement\.collisionHelper uses primitive type string`
	if raw == "" {
		if raw != "" {
			if err := name.Validate(); err != nil {
				return
			}
		}
	}
	useCommand(name)
}

// CallerCalleeBlockCollision exercises a witness whose caller and callee CFGs
// both use small block indexes. Refinement must not replay callee indexes as
// caller edges or discharge the feasible violation.
func CallerCalleeBlockCollision(raw string) { // want `parameter "raw" of cfa_join_refinement\.CallerCalleeBlockCollision uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	collisionHelper(name, raw)
}
