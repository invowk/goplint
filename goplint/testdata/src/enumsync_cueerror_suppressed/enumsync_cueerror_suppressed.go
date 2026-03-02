// SPDX-License-Identifier: MPL-2.0

package enumsync_cueerror_suppressed

import "fmt"

//goplint:enum-cue=#MissingRuntimeMode
type RuntimeMode string

func (m RuntimeMode) Validate() error {
	switch m {
	case "native", "virtual":
		return nil
	default:
		return fmt.Errorf("invalid mode %q", m)
	}
}

func (m RuntimeMode) String() string { return string(m) }
