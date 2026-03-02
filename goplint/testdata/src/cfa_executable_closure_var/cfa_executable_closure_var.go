// SPDX-License-Identifier: MPL-2.0

package cfa_executable_closure_var

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

func useCmd(_ CommandName) {}

// DetachedClosureVariable should not be flagged because the closure is stored
// but never executed.
func DetachedClosureVariable(raw string) { // want `parameter "raw" of cfa_executable_closure_var\.DetachedClosureVariable uses primitive type string`
	f := func() {
		x := CommandName(raw)
		useCmd(x)
	}
	_ = f
}

// ExecutedClosureVariable should be flagged: closure bound to a local variable
// and executed synchronously via f().
func ExecutedClosureVariable(raw string) { // want `parameter "raw" of cfa_executable_closure_var\.ExecutedClosureVariable uses primitive type string`
	f := func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}
	f()
}

// ExecutedClosureVariableVarSpec should also be flagged when binding uses
// a var declaration instead of short assignment.
func ExecutedClosureVariableVarSpec(raw string) { // want `parameter "raw" of cfa_executable_closure_var\.ExecutedClosureVariableVarSpec uses primitive type string`
	var f = func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}
	f()
}

// DeferredClosureVariable should be flagged: deferred invocation still executes
// the closure body, so casts inside it are analyzed.
func DeferredClosureVariable(raw string) { // want `parameter "raw" of cfa_executable_closure_var\.DeferredClosureVariable uses primitive type string`
	f := func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}
	defer f()
}

// GoroutineClosureVariable should be flagged: asynchronous invocation is still
// executable and closure-local casts must be analyzed.
func GoroutineClosureVariable(raw string) { // want `parameter "raw" of cfa_executable_closure_var\.GoroutineClosureVariable uses primitive type string`
	f := func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}
	go f()
}

// ParenthesizedLiteralIIFE should be flagged: parenthesized function literals
// are executable closures and must be analyzed.
func ParenthesizedLiteralIIFE(raw string) { // want `parameter "raw" of cfa_executable_closure_var\.ParenthesizedLiteralIIFE uses primitive type string`
	(func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	})()
}

// ExecutedClosureVariableValidated should not be flagged because the closure
// validates the cast before using it.
func ExecutedClosureVariableValidated(raw string) { // want `parameter "raw" of cfa_executable_closure_var\.ExecutedClosureVariableValidated uses primitive type string`
	f := func() {
		x := CommandName(raw)
		if err := x.Validate(); err != nil {
			return
		}
		useCmd(x)
	}
	f()
}
