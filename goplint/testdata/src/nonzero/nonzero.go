// SPDX-License-Identifier: MPL-2.0

package nonzero

import "fmt"

// --- Types with //goplint:nonzero ---

//goplint:nonzero
type CommandName string // want CommandName:"nonzero"

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("empty command name")
	}
	return nil
}

func (c CommandName) String() string { return string(c) }

//goplint:nonzero
type ModuleID string // want ModuleID:"nonzero"

func (m ModuleID) Validate() error {
	if m == "" {
		return fmt.Errorf("empty module id")
	}
	return nil
}

func (m ModuleID) String() string { return string(m) }

// ZeroValidType does NOT have //goplint:nonzero — zero value is valid.
type ZeroValidType string

func (z ZeroValidType) Validate() error { return nil }
func (z ZeroValidType) String() string  { return string(z) }

// --- Struct using nonzero types ---

// ConfigWithNonZero uses nonzero types as value fields — should be flagged.
type ConfigWithNonZero struct {
	Name     CommandName   // want `struct field nonzero\.ConfigWithNonZero\.Name uses nonzero type CommandName as value`
	Module   ModuleID      // want `struct field nonzero\.ConfigWithNonZero\.Module uses nonzero type ModuleID as value`
	ZeroOK   ZeroValidType // NOT flagged — no nonzero annotation
	internal string        // want `struct field nonzero\.ConfigWithNonZero\.internal uses primitive type string`
}

// ConfigWithPointers uses *Type for optional nonzero fields — correct.
type ConfigWithPointers struct {
	Name   *CommandName  // NOT flagged — pointer is correct
	Module *ModuleID     // NOT flagged — pointer is correct
	ZeroOK ZeroValidType // NOT flagged — no nonzero annotation
}

// MixedStruct has both correct and incorrect usage.
type MixedStruct struct {
	Required CommandName  // want `struct field nonzero\.MixedStruct\.Required uses nonzero type CommandName as value`
	Optional *CommandName // NOT flagged — pointer
}

// EmbeddedNonZero embeds a nonzero type directly — should be flagged.
type EmbeddedNonZero struct {
	CommandName // want `struct field nonzero\.EmbeddedNonZero\.\(embedded\) uses nonzero type CommandName as value`
	Other       string // want `struct field nonzero\.EmbeddedNonZero\.Other uses primitive type string`
}

// --- Function parameters with nonzero types: NOT checked ---
// --check-nonzero only checks struct fields, not function parameters.

// FuncWithNonZeroParam takes a nonzero type by value — NOT flagged.
// Documents that --check-nonzero scope is struct-field-only.
func FuncWithNonZeroParam(id CommandName) { _ = id }
