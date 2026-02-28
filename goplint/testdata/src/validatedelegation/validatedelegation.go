// SPDX-License-Identifier: MPL-2.0

package validatedelegation

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

// --- Complete delegation (all 3 fields) — should NOT be flagged ---

//goplint:validate-all
type CompleteConfig struct {
	FieldName  Name
	FieldMode  Mode
	FieldLevel Level
}

func (c *CompleteConfig) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	if err := c.FieldLevel.Validate(); err != nil {
		return err
	}
	return nil
}

// --- Incomplete delegation (misses FieldLevel) — should be flagged ---

//goplint:validate-all
type IncompleteConfig struct { // want `validatedelegation\.IncompleteConfig\.Validate\(\) does not delegate to field FieldLevel which has Validate\(\)`
	FieldName  Name
	FieldMode  Mode
	FieldLevel Level
}

func (c *IncompleteConfig) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	// FieldLevel.Validate() is missing!
	return nil
}

// --- No validate-all directive — should NOT be flagged even with missing delegation ---

type NoDirectiveConfig struct {
	FieldName Name
	FieldMode Mode
}

func (c *NoDirectiveConfig) Validate() error {
	// Only validates FieldName, skips FieldMode — but no directive, so not checked.
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	return nil
}

// --- Delegation via intermediate variable — should NOT be flagged ---

//goplint:validate-all
type AliasConfig struct {
	FieldName Name
	FieldMode Mode
}

func (c *AliasConfig) Validate() error {
	name := c.FieldName
	if err := name.Validate(); err != nil {
		return err
	}
	mode := c.FieldMode
	if err := mode.Validate(); err != nil {
		return err
	}
	return nil
}

// --- Value receiver delegation — should NOT be flagged ---

//goplint:validate-all
type ValueReceiverConfig struct {
	FieldName Name
	FieldMode Mode
}

func (c ValueReceiverConfig) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	return nil
}

// --- Embedded field delegation — complete ---

//goplint:validate-all
type EmbeddedComplete struct {
	Name
	FieldMode Mode
}

func (c *EmbeddedComplete) Validate() error {
	if err := c.Name.Validate(); err != nil {
		return err
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	return nil
}

// --- Embedded field delegation — incomplete ---

//goplint:validate-all
type EmbeddedIncomplete struct { // want `validatedelegation\.EmbeddedIncomplete\.Validate\(\) does not delegate to field Name which has Validate\(\)`
	Name
	FieldMode Mode
}

func (c *EmbeddedIncomplete) Validate() error {
	// Name.Validate() is missing!
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	return nil
}

// --- Mix of validatable and non-validatable fields ---

//goplint:validate-all
type MixedFieldConfig struct { // want `validatedelegation\.MixedFieldConfig\.Validate\(\) does not delegate to field FieldMode which has Validate\(\)`
	FieldName Name
	FieldMode Mode
	plain     int // want `struct field validatedelegation\.MixedFieldConfig\.plain uses primitive type int`
}

func (c *MixedFieldConfig) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	// FieldMode.Validate() is missing!
	_ = c.plain
	return nil
}

// --- Field with no-delegate directive — not required in Validate() ---

//goplint:validate-all
type NoDelegateConfig struct {
	FieldName Name
	//goplint:no-delegate -- validated by external caller
	FieldMode Mode
}

func (c *NoDelegateConfig) Validate() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	// FieldMode.Validate() deliberately not called — no-delegate.
	return nil
}

// --- no-delegate with missing delegation on non-annotated field ---

//goplint:validate-all
type NoDelegateIncomplete struct { // want `validatedelegation\.NoDelegateIncomplete\.Validate\(\) does not delegate to field FieldName which has Validate\(\)`
	FieldName Name
	//goplint:no-delegate -- validated externally
	FieldMode Mode
}

func (c *NoDelegateIncomplete) Validate() error {
	// FieldName.Validate() is missing — should be flagged.
	// FieldMode.Validate() is skipped via no-delegate — not flagged.
	return nil
}

// --- Range loop delegation — complete ---

//goplint:validate-all
type LoopConfig struct {
	Items     []Name
	FieldMode Mode
}

func (c *LoopConfig) Validate() error {
	for _, item := range c.Items {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	return nil
}

// --- Range loop delegation — incomplete (Items not iterated) ---

//goplint:validate-all
type IncompleteLoopConfig struct { // want `validatedelegation\.IncompleteLoopConfig\.Validate\(\) does not delegate to field Items which has Validate\(\)`
	Items     []Name
	FieldMode Mode
}

func (c *IncompleteLoopConfig) Validate() error {
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	// Items not iterated — should be flagged.
	return nil
}

// --- Map value delegation — complete ---

//goplint:validate-all
type MapConfig struct {
	Entries   map[string]Name // want `struct field validatedelegation\.MapConfig\.Entries uses primitive type string \(in map key\)`
	FieldMode Mode
}

func (c *MapConfig) Validate() error {
	for _, v := range c.Entries {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	return nil
}

// --- Map value delegation — incomplete (Entries not iterated) ---

//goplint:validate-all
type IncompleteMapConfig struct { // want `validatedelegation\.IncompleteMapConfig\.Validate\(\) does not delegate to field Entries which has Validate\(\)`
	Entries   map[string]Name // want `struct field validatedelegation\.IncompleteMapConfig\.Entries uses primitive type string \(in map key\)`
	FieldMode Mode
}

func (c *IncompleteMapConfig) Validate() error {
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	// Entries not iterated — should be flagged.
	return nil
}

// --- errors.Join delegation (already works via Pass 1 recursive walk) ---

//goplint:validate-all
type JoinConfig struct {
	FieldName Name
	FieldMode Mode
}

func (c *JoinConfig) Validate() error {
	return errors.Join(c.FieldName.Validate(), c.FieldMode.Validate())
}

// --- Helper method delegation — complete ---

//goplint:validate-all
type HelperConfig struct {
	FieldName Name
	FieldMode Mode
}

func (c *HelperConfig) Validate() error {
	return c.validateFields()
}

func (c *HelperConfig) validateFields() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	return nil
}

// --- Helper method delegation — incomplete (missing FieldMode in helper) ---

//goplint:validate-all
type IncompleteHelperConfig struct { // want `validatedelegation\.IncompleteHelperConfig\.Validate\(\) does not delegate to field FieldMode which has Validate\(\)`
	FieldName Name
	FieldMode Mode
}

func (c *IncompleteHelperConfig) Validate() error {
	return c.validatePartial()
}

func (c *IncompleteHelperConfig) validatePartial() error {
	if err := c.FieldName.Validate(); err != nil {
		return err
	}
	// FieldMode.Validate() missing — should be flagged.
	return nil
}

// --- Two-level helper method delegation — complete ---

//goplint:validate-all
type TwoLevelHelperConfig struct {
	FieldName Name
	FieldMode Mode
}

func (c *TwoLevelHelperConfig) Validate() error {
	return c.validateBase()
}

func (c *TwoLevelHelperConfig) validateBase() error {
	if err := c.validateNameField(); err != nil {
		return err
	}
	if err := c.FieldMode.Validate(); err != nil {
		return err
	}
	return nil
}

func (c *TwoLevelHelperConfig) validateNameField() error {
	return c.FieldName.Validate()
}

// --- Two-level helper method delegation — incomplete ---

//goplint:validate-all
type TwoLevelIncomplete struct { // want `validatedelegation\.TwoLevelIncomplete\.Validate\(\) does not delegate to field FieldMode which has Validate\(\)`
	FieldName Name
	FieldMode Mode
}

func (c *TwoLevelIncomplete) Validate() error {
	return c.validateOnly()
}

func (c *TwoLevelIncomplete) validateOnly() error {
	return c.validateJustName()
}

func (c *TwoLevelIncomplete) validateJustName() error {
	return c.FieldName.Validate()
	// FieldMode.Validate() missing — should be flagged.
}
