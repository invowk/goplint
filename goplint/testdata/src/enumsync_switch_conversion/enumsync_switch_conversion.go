// SPDX-License-Identifier: MPL-2.0

package enumsync_switch_conversion

import "fmt"

//goplint:enum-cue=#Mode
type Mode string

func (m Mode) Validate() error {
	switch string(m) {
	case "native", "virtual":
		return nil
	default:
		return fmt.Errorf("invalid mode %q", m)
	}
}

func (m Mode) String() string { return string(m) }

func normalizeMode(m Mode) string { // want `return value of enumsync_switch_conversion\.normalizeMode uses primitive type string`
	return string(m)
}

//goplint:enum-cue=#Mode
type ModeIgnoredTag string // want `type enumsync_switch_conversion\.ModeIgnoredTag: CUE member "native" \(at #Mode\) is missing from Validate\(\) switch cases` `type enumsync_switch_conversion\.ModeIgnoredTag: CUE member "virtual" \(at #Mode\) is missing from Validate\(\) switch cases`

func (m ModeIgnoredTag) Validate() error {
	switch normalizeMode(Mode(m)) {
	case "native", "virtual":
		return nil
	default:
		return fmt.Errorf("invalid mode %q", m)
	}
}

func (m ModeIgnoredTag) String() string { return string(m) }
