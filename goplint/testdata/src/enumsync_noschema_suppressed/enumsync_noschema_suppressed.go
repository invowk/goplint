// SPDX-License-Identifier: MPL-2.0

package enumsync_noschema_suppressed

import "fmt"

//goplint:enum-cue=#RuntimeMode
type NoSchemaMode string

func (m NoSchemaMode) Validate() error {
	switch m {
	case "native", "virtual":
		return nil
	default:
		return fmt.Errorf("invalid mode %q", m)
	}
}

func (m NoSchemaMode) String() string { return string(m) }
