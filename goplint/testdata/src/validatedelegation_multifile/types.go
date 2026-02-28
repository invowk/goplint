// SPDX-License-Identifier: MPL-2.0

package validatedelegation_multifile

import "fmt"

// Name is a DDD Value Type with Validate.
type Name string

func (n Name) Validate() error {
	if n == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func (n Name) String() string { return string(n) }

// Mode is a DDD Value Type with Validate.
type Mode string

func (m Mode) Validate() error {
	if m == "" {
		return fmt.Errorf("empty mode")
	}
	return nil
}

func (m Mode) String() string { return string(m) }

// Config is defined in this file — its Validate() is in validate.go.
// This tests cross-file delegation detection.
//
//goplint:validate-all
type Config struct {
	FieldName Name
	FieldMode Mode
}

// IncompleteConfig is defined here — its Validate() is in validate.go.
// This tests that cross-file incomplete delegation IS flagged.
//
//goplint:validate-all
type IncompleteConfig struct { // want `validatedelegation_multifile\.IncompleteConfig\.Validate\(\) does not delegate to field FieldMode which has Validate\(\)`
	FieldName Name
	FieldMode Mode
}
