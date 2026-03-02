// SPDX-License-Identifier: MPL-2.0

package enumsync_multiswitch_scoped

//goplint:enum-cue=#Mode
type Mode string

func (m Mode) Validate() error {
	switch m {
	case "a", "b":
		return nil
	}

	n := 1
	switch n {
	case 1:
		// unrelated switch should not affect enum-sync
	}

	return nil
}
