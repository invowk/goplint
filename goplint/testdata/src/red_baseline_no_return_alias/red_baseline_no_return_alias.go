// SPDX-License-Identifier: MPL-2.0

package red_baseline_no_return_alias

import "os"

type Name string

func (name Name) Validate() error { return nil }

func ConditionalAlias(raw string, terminate bool) { // want `parameter "raw" of red_baseline_no_return_alias\.ConditionalAlias uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
	exit := func(int) {}
	if terminate {
		exit = os.Exit
	}
	exit(1)
	_ = name
}
