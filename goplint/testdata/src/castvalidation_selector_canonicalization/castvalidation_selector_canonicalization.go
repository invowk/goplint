// SPDX-License-Identifier: MPL-2.0

package castvalidation_selector_canonicalization

type CommandName string

func (c CommandName) Validate() error { return nil }

type Holder struct {
	Name CommandName
}

func CanonicalPointerSelector(raw string, holder *Holder) { // want `parameter "raw" of castvalidation_selector_canonicalization\.CanonicalPointerSelector uses primitive type string`
	(*holder).Name = CommandName(raw)
	if err := holder.Name.Validate(); err != nil {
		return
	}
}
