// SPDX-License-Identifier: MPL-2.0

package red_baseline_ambiguous_alias

type Name string

func (name Name) Validate() error { return nil }

func AmbiguousPhi(raw string, choose bool) error { // want `parameter "raw" of red_baseline_ambiguous_alias\.AmbiguousPhi uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant has inconclusive Validate\(\) path analysis`
	other := Name("other")
	alias := name
	if choose {
		alias = name
	} else {
		alias = other
	}
	return alias.Validate()
}
