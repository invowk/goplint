// SPDX-License-Identifier: MPL-2.0

// Package enumsync_multifile tests enum-sync with multiple CUE schema files.
package enumsync_multifile

// Mode is an enum type synced with CUE via the types_schema.cue file.
//
//goplint:enum-cue=#Mode
type Mode string

func (m Mode) Validate() error {
	switch m {
	case "read", "write":
		return nil
	default:
		return &InvalidModeError{Value: m}
	}
}

func (m Mode) String() string { return string(m) }

// InvalidModeError is returned when Mode.Validate() fails.
type InvalidModeError struct {
	Value Mode
}

func (e *InvalidModeError) Error() string {
	return "invalid mode: " + string(e.Value)
}

// Format is an enum type synced with CUE via the config_schema.cue file.
//
//goplint:enum-cue=#Format
type Format string

func (f Format) Validate() error {
	switch f {
	case "json", "yaml":
		return nil
	default:
		return &InvalidFormatError{Value: f}
	}
}

func (f Format) String() string { return string(f) }

// InvalidFormatError is returned when Format.Validate() fails.
type InvalidFormatError struct {
	Value Format
}

func (e *InvalidFormatError) Error() string {
	return "invalid format: " + string(e.Value)
}
