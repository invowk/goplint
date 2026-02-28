// SPDX-License-Identifier: MPL-2.0

// Package cfa_closure provides test fixtures for CFA closure analysis.
// In AST mode, closure bodies are completely skipped. In CFA mode,
// each closure gets its own CFG and independent validation analysis.
package cfa_closure

import "fmt"

// --- DDD Value Types for testing ---

// CommandName is a DDD Value Type with Validate.
type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command name")
	}
	return nil
}

func (c CommandName) String() string { return string(c) }

func useCmd(_ CommandName) {}

// --- Closure test cases ---

// CastInGoroutineClosure — flagged by CFA because the closure's cast
// has no Validate() call. In AST mode, closure bodies are skipped entirely.
func CastInGoroutineClosure(raw string) { // want `parameter "raw" of cfa_closure\.CastInGoroutineClosure uses primitive type string`
	go func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}()
}

// CastInClosureWithValidation — NOT flagged because the closure's cast
// is followed by Validate() on all paths.
func CastInClosureWithValidation(raw string) { // want `parameter "raw" of cfa_closure\.CastInClosureWithValidation uses primitive type string`
	go func() {
		x := CommandName(raw)
		if err := x.Validate(); err != nil {
			return
		}
		useCmd(x)
	}()
}

// CastInDeferClosure — flagged by CFA (deferred closure, no validation).
func CastInDeferClosure(raw string) { // want `parameter "raw" of cfa_closure\.CastInDeferClosure uses primitive type string`
	defer func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}()
}

// CastInImmediateClosure — flagged by CFA (immediately invoked, no validation).
func CastInImmediateClosure(raw string) { // want `parameter "raw" of cfa_closure\.CastInImmediateClosure uses primitive type string`
	func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}()
}

// OuterValidationDoesNotCoverClosure — the outer function validates x,
// but the closure creates its own y which is not validated.
func OuterValidationDoesNotCoverClosure(raw string) { // want `parameter "raw" of cfa_closure\.OuterValidationDoesNotCoverClosure uses primitive type string`
	x := CommandName(raw)
	if err := x.Validate(); err != nil {
		return
	}
	useCmd(x)
	go func() {
		y := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(y)
	}()
}

// ConstantInClosure — NOT flagged (constant source inside closure).
func ConstantInClosure() {
	go func() {
		x := CommandName("literal")
		useCmd(x)
	}()
}

// NestedClosureAnalyzed — nested closures are now recursively analyzed.
// The cast inside the inner closure IS flagged.
func NestedClosureAnalyzed(raw string) { // want `parameter "raw" of cfa_closure\.NestedClosureAnalyzed uses primitive type string`
	go func() {
		go func() {
			x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
			useCmd(x)
		}()
	}()
}
