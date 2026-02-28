// SPDX-License-Identifier: MPL-2.0

// Package enumsync_noschema tests --check-enum-sync behavior when the
// package has types with //goplint:enum-cue directives but no *_schema.cue
// file. The analyzer should emit a "no schema file found" diagnostic.
package enumsync_noschema

import "fmt"

//goplint:enum-cue=#RuntimeMode
type NoSchemaMode string // want `type enumsync_noschema\.NoSchemaMode has //goplint:enum-cue directive but no \*_schema\.cue file found in package directory`

func (m NoSchemaMode) Validate() error {
	switch m {
	case "native", "virtual":
		return nil
	default:
		return fmt.Errorf("invalid mode %q", m)
	}
}

func (m NoSchemaMode) String() string { return string(m) }
