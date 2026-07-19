// SPDX-License-Identifier: MPL-2.0

package cfa_refinement_query_limit

type CommandName string

func (name CommandName) Validate() error { return nil }

func useCommand(CommandName) {}

func QueryLimit(raw string) { // want `parameter "raw" of cfa_refinement_query_limit\.QueryLimit uses primitive type string`
	//goplint:ignore -- proof uncertainty remains visible despite policy suppression.
	name := CommandName(raw) // want `type conversion to CommandName from non-constant has inconclusive Validate\(\) path analysis`
	if raw == "" {
		if raw != "" {
			useCommand(name)
			return
		}
	}
	if err := name.Validate(); err != nil {
		return
	}
}
