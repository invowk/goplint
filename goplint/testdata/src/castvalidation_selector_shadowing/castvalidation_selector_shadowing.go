// SPDX-License-Identifier: MPL-2.0

package castvalidation_selector_shadowing

type CommandName string

func (c CommandName) Validate() error { return nil }

type Holder struct {
	Name CommandName
}

func ShadowedSelectorAssignment(raw string, holder *Holder) { // want `parameter "raw" of castvalidation_selector_shadowing\.ShadowedSelectorAssignment uses primitive type string`
	{
		holder := &Holder{}
		holder.Name = CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	}

	// Validates outer holder.Name only.
	_ = holder.Name.Validate()
}
