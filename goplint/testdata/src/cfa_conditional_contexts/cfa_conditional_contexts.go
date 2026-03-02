// SPDX-License-Identifier: MPL-2.0

package cfa_conditional_contexts

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command name")
	}
	return nil
}

func useCmd(_ CommandName) {}

// ValidateInRangeLoop — FLAGGED. Range bodies are conditionally executed and
// may run zero times, so Validate inside the loop body does not guarantee
// validation on every path.
func ValidateInRangeLoop(raw string, items []int) { // want `parameter "raw" of cfa_conditional_contexts\.ValidateInRangeLoop uses primitive type string` `parameter "items" of cfa_conditional_contexts\.ValidateInRangeLoop uses primitive type \[\]int`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	for range items {
		_ = x.Validate()
	}
	useCmd(x)
}

// ValidateInTypeSwitch — FLAGGED. Validate in one type-switch case does not
// guarantee validation on all paths.
func ValidateInTypeSwitch(raw string, v any) { // want `parameter "raw" of cfa_conditional_contexts\.ValidateInTypeSwitch uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	switch v.(type) {
	case string:
		_ = x.Validate()
	default:
	}
	useCmd(x)
}

// ValidateInIfInit — NOT flagged. If init statements execute unconditionally
// when the if statement is evaluated.
func ValidateInIfInit(raw string) { // want `parameter "raw" of cfa_conditional_contexts\.ValidateInIfInit uses primitive type string`
	x := CommandName(raw)
	if err := x.Validate(); err != nil {
		return
	}
	useCmd(x)
}

// ValidateInSwitchInit — NOT flagged. Switch init statements execute
// unconditionally before case dispatch.
func ValidateInSwitchInit(raw string) { // want `parameter "raw" of cfa_conditional_contexts\.ValidateInSwitchInit uses primitive type string`
	x := CommandName(raw)
	switch err := x.Validate(); {
	case err != nil:
		return
	default:
	}
	useCmd(x)
}
