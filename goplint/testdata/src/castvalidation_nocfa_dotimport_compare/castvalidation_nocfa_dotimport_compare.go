// SPDX-License-Identifier: MPL-2.0

package castvalidation_nocfa_dotimport_compare

import (
	"fmt"
	. "strings"
)

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid")
	}
	return nil
}

func DotImportComparatorNoFinding(raw string) { // want `parameter "raw" of castvalidation_nocfa_dotimport_compare\.DotImportComparatorNoFinding uses primitive type string`
	_ = HasPrefix(string(CommandName(raw)), "x")
}

func DotImportNonComparatorFinding(raw string) { // want `parameter "raw" of castvalidation_nocfa_dotimport_compare\.DotImportNonComparatorFinding uses primitive type string`
	_ = TrimSpace(string(CommandName(raw))) // want `type conversion to CommandName from non-constant without Validate\(\) check`
}
