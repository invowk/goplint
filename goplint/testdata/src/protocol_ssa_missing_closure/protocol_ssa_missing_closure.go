// SPDX-License-Identifier: MPL-2.0

package protocol_ssa_missing_closure

type Name string

func (n Name) Validate() error { return nil }

func Build(raw string) error { // want `parameter "raw" of protocol_ssa_missing_closure\.Build uses primitive type string`
	return func() error {
		//goplint:ignore -- proof uncertainty remains visible despite inline suppression.
		name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis` `variable name of type Name has inconclusive use-before-validate path analysis`
		return name.Validate()
	}()
}
