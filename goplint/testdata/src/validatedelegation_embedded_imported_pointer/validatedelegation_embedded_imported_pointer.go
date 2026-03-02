// SPDX-License-Identifier: MPL-2.0

package validatedelegation_embedded_imported_pointer

import "validatedelegation_embedded_imported_pointer/dep"

//goplint:validate-all
type Wrapper struct { // want `validatedelegation_embedded_imported_pointer\.Wrapper\.Validate\(\) does not delegate to field Child which has Validate\(\)`
	*dep.Child
}

func (w Wrapper) Validate() error {
	return nil
}
