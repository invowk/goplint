// SPDX-License-Identifier: MPL-2.0

package validatedelegationall

import (
	"errors"
	"fmt"
)

// --- Types with Validate() for use as fields ---

type Name string

func (n Name) Validate() error {
	if n == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func (n Name) String() string { return string(n) }

type Mode string

func (m Mode) Validate() error {
	if m == "" {
		return fmt.Errorf("empty mode")
	}
	return nil
}

func (m Mode) String() string { return string(m) }

type Level string

func (l Level) Validate() error {
	if l == "" {
		return fmt.Errorf("empty level")
	}
	return nil
}

func (l Level) String() string { return string(l) }

// --- Sub-case 1: struct with validatable fields but NO Validate() ---
// Should be flagged as missing-struct-validate-fields.

type MissingValidateStruct struct { // want `struct validatedelegationall\.MissingValidateStruct has 2 validatable field\(s\) but no Validate\(\) method`
	FieldName Name
	FieldMode Mode
	plain     int // want `struct field validatedelegationall\.MissingValidateStruct\.plain uses primitive type int`
}

// --- Sub-case 1b: struct with slice of validatable elements but no Validate() ---

type MissingValidateSlice struct { // want `struct validatedelegationall\.MissingValidateSlice has 1 validatable field\(s\) but no Validate\(\) method`
	Items []Name
}

// --- Sub-case 2: struct HAS Validate() but incomplete delegation ---
// No //goplint:validate-all directive, but still checked by the universal mode.

type IncompleteDelegationNoDirective struct { // want `validatedelegationall\.IncompleteDelegationNoDirective\.Validate\(\) does not delegate to field FieldMode which has Validate\(\)`
	FieldName Name
	FieldMode Mode
}

func (c *IncompleteDelegationNoDirective) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	// FieldMode.Validate() missing — flagged by universal mode.
	return nil
}

// --- Complete delegation without directive — should NOT be flagged ---

type CompleteDelegationNoDirective struct {
	FieldName Name
	FieldMode Mode
}

func (c *CompleteDelegationNoDirective) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	return nil
}

// --- Struct with no validatable fields — should NOT be flagged ---

type NoValidatableFields struct {
	plain int    // want `struct field validatedelegationall\.NoValidatableFields\.plain uses primitive type int`
	text  string // want `struct field validatedelegationall\.NoValidatableFields\.text uses primitive type string`
}

// --- Error type struct — should be excluded ---

type ConfigError struct {
	FieldName Name
	FieldMode Mode
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config error: %s %s", e.FieldName, e.FieldMode)
}

// --- Error type by suffix only (no Error() method) — should also be excluded ---

type ValidationError struct {
	Field Name
}

// --- Struct with //goplint:no-delegate — should respect field exemption ---

type PartialDelegateConfig struct {
	FieldName Name
	//goplint:no-delegate -- validated externally
	FieldMode Mode
}

func (c *PartialDelegateConfig) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	// FieldMode.Validate() deliberately not called — no-delegate.
	return nil
}

// --- Struct with //goplint:validate-all — handled by opt-in mode ---
// Universal mode skips directive-annotated structs to avoid duplicate findings.
// The incomplete delegation is caught by --check-validate-delegation instead.

//goplint:validate-all
type DirectiveAndUniversal struct { // want `validatedelegationall\.DirectiveAndUniversal\.Validate\(\) does not delegate to field FieldLevel which has Validate\(\)`
	FieldName  Name
	FieldMode  Mode
	FieldLevel Level
}

func (c *DirectiveAndUniversal) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	// FieldLevel.Validate() missing.
	return nil
}

// --- errors.Join delegation — should NOT be flagged ---

type JoinDelegation struct {
	FieldName Name
	FieldMode Mode
}

func (c *JoinDelegation) Validate() error {
	return errors.Join(c.FieldName.Validate(), c.FieldMode.Validate())
}

// --- Embedded field delegation via universal mode ---

type EmbeddedMissing struct { // want `validatedelegationall\.EmbeddedMissing\.Validate\(\) does not delegate to field Name which has Validate\(\)`
	Name
	FieldMode Mode
}

func (c *EmbeddedMissing) Validate() error {
	// Name.Validate() missing!
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	return nil
}
