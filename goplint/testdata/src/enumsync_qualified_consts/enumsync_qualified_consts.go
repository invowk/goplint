// SPDX-License-Identifier: MPL-2.0

package enumsync_qualified_consts

import (
	"enumsync_qualified_consts/defs"
	"fmt"
)

//goplint:enum-cue=#Mode
type Mode string

func (m Mode) Validate() error {
	switch m {
	case defs.ModeNative, defs.ModeVirtual:
		return nil
	default:
		return fmt.Errorf("invalid mode %q", m)
	}
}

func (m Mode) String() string { return string(m) }

//goplint:enum-cue=#Mode
type ModeExtra string // want `type enumsync_qualified_consts\.ModeExtra: Validate\(\) switch case "legacy" is not present in CUE disjunction at #Mode`

func (m ModeExtra) Validate() error {
	switch m {
	case defs.ModeNative, defs.ModeVirtual, defs.ModeLegacy:
		return nil
	default:
		return fmt.Errorf("invalid mode %q", m)
	}
}

func (m ModeExtra) String() string { return string(m) }
