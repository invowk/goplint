// SPDX-License-Identifier: MPL-2.0

package cfa_refinement_evidence_corrupt

type CommandName string

func (name CommandName) Validate() error { return nil }

func useCommand(CommandName) {}

func CorruptEvidence(raw string) { // want `parameter "raw" of cfa_refinement_evidence_corrupt\.CorruptEvidence uses primitive type string`
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
