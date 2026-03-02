// SPDX-License-Identifier: MPL-2.0

package enumsync_intmembers

import "fmt"

//goplint:enum-cue=#Level
type Level int // want `type enumsync_intmembers\.Level: CUE member "2" \(at #Level\) is missing from Validate\(\) switch cases`

func (l Level) Validate() error {
	switch l {
	case 0, 1:
		return nil
	default:
		return fmt.Errorf("invalid level %d", l)
	}
}

func (l Level) String() string { return fmt.Sprintf("%d", l) }
