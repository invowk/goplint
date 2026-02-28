// SPDX-License-Identifier: MPL-2.0

package castvalidation

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

// PortNumber is an int-backed DDD Value Type.
type PortNumber int

func (p PortNumber) Validate() error {
	if p <= 0 || p >= 65536 {
		return fmt.Errorf("invalid port number: %d", int(p))
	}
	return nil
}

func (p PortNumber) String() string { return fmt.Sprintf("%d", int(p)) }

// NoValidate has no Validate method — casts to this should NOT be flagged.
type NoValidate string

func (n NoValidate) String() string { return string(n) }

// AnotherNamedType wraps string — not a raw primitive.
type AnotherNamedType string

// --- Helper functions providing runtime values without param-level findings ---

func runtimeString() string { return "test" } // want `return value of castvalidation\.runtimeString uses primitive type string`

func runtimeInt() int { return 42 } // want `return value of castvalidation\.runtimeInt uses primitive type int`

// --- Assigned cast tests ---

// CastFromVariableNoValidation — SHOULD be flagged.
func CastFromVariableNoValidation(input string) { // want `parameter "input" of castvalidation\.CastFromVariableNoValidation uses primitive type string`
	name := CommandName(input) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = name
}

// CastFromVariableWithValidation — should NOT be flagged for cast.
func CastFromVariableWithValidation(input string) { // want `parameter "input" of castvalidation\.CastFromVariableWithValidation uses primitive type string`
	name := CommandName(input)
	if err := name.Validate(); err != nil {
		return
	}
	_ = name
}

// CastFromStringLiteral — should NOT be flagged (constant).
func CastFromStringLiteral() {
	name := CommandName("test")
	_ = name
}

// CastFromNamedConstant — should NOT be flagged.
func CastFromNamedConstant() {
	const s = "test"
	name := CommandName(s)
	_ = name
}

// CastToTypeWithoutValidate — should NOT be flagged (no Validate method).
func CastToTypeWithoutValidate(input string) { // want `parameter "input" of castvalidation\.CastToTypeWithoutValidate uses primitive type string`
	name := NoValidate(input)
	_ = name
}

// CastFromFuncReturn — SHOULD be flagged.
func CastFromFuncReturn() {
	name := CommandName(runtimeString()) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = name
}

// MultipleAssignments — only unvalidated cast should be flagged.
func MultipleAssignments(a, b string) { // want `parameter "a" of castvalidation\.MultipleAssignments uses primitive type string` `parameter "b" of castvalidation\.MultipleAssignments uses primitive type string`
	x := CommandName(a) // NOT flagged — Validate is called on x below
	if err := x.Validate(); err != nil {
		return
	}
	y := CommandName(b) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = x
	_ = y
}

// CastIntNoValidation — should work for int types.
func CastIntNoValidation() {
	p := PortNumber(runtimeInt()) // want `type conversion to PortNumber from non-constant without Validate\(\) check`
	_ = p
}

// CastIntWithValidation — should NOT be flagged.
func CastIntWithValidation() {
	p := PortNumber(runtimeInt())
	if err := p.Validate(); err != nil {
		return
	}
	_ = p
}

// --- Unassigned cast tests ---

// UnassignedReturn — SHOULD be flagged.
func UnassignedReturn(input string) CommandName { // want `parameter "input" of castvalidation\.UnassignedReturn uses primitive type string`
	return CommandName(input) // want `type conversion to CommandName from non-constant without Validate\(\) check`
}

// UnassignedFuncArg — SHOULD be flagged.
func UnassignedFuncArg(input string) { // want `parameter "input" of castvalidation\.UnassignedFuncArg uses primitive type string`
	useCmd(CommandName(input)) // want `type conversion to CommandName from non-constant without Validate\(\) check`
}

func useCmd(_ CommandName) {}

// UnassignedMapKey — should NOT be flagged (auto-skip: map index).
func UnassignedMapKey(input string) { // want `parameter "input" of castvalidation\.UnassignedMapKey uses primitive type string`
	m := map[CommandName]bool{}
	_ = m[CommandName(input)] // NOT flagged — map lookup
}

// UnassignedComparison — should NOT be flagged (auto-skip: comparison).
func UnassignedComparison(input string, expected CommandName) bool { // want `parameter "input" of castvalidation\.UnassignedComparison uses primitive type string`
	return CommandName(input) == expected // NOT flagged — comparison
}

// UnassignedFmtArg — should NOT be flagged (auto-skip: fmt argument).
func UnassignedFmtArg(input string) string { // want `parameter "input" of castvalidation\.UnassignedFmtArg uses primitive type string` `return value of castvalidation\.UnassignedFmtArg uses primitive type string`
	return fmt.Sprintf("cmd: %s", CommandName(input)) // NOT flagged — display only
}

// UnassignedConstReturn — should NOT be flagged (constant source).
func UnassignedConstReturn() CommandName {
	return CommandName("test") // NOT flagged — constant expression
}

// --- Named-to-named cast tests ---

// CastFromNamedType — should NOT be flagged (named-to-named).
func CastFromNamedType(input AnotherNamedType) {
	name := CommandName(input)
	_ = name
}

// --- Blank identifier assignment ---

// CastToBlankIdentifier — treated as unassigned, SHOULD be flagged.
func CastToBlankIdentifier(input string) { // want `parameter "input" of castvalidation\.CastToBlankIdentifier uses primitive type string`
	_ = CommandName(input) // want `type conversion to CommandName from non-constant without Validate\(\) check`
}

// CastIntLiteral — should NOT be flagged (constant).
func CastIntLiteral() {
	p := PortNumber(8080)
	_ = p
}

// CastWithAssignOp — test = (assign) vs := (define).
func CastWithAssignOp(input string) { // want `parameter "input" of castvalidation\.CastWithAssignOp uses primitive type string`
	var name CommandName
	name = CommandName(input) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = name
}

// CastWithAssignOpValidated — test = with Validate.
func CastWithAssignOpValidated(input string) { // want `parameter "input" of castvalidation\.CastWithAssignOpValidated uses primitive type string`
	var name CommandName
	name = CommandName(input)
	if err := name.Validate(); err != nil {
		return
	}
	_ = name
}

// --- Chained Validate tests (Issue 1 fix) ---

// ChainedValidate — should NOT be flagged (validated directly on cast result).
func ChainedValidate(input string) { // want `parameter "input" of castvalidation\.ChainedValidate uses primitive type string`
	if err := CommandName(input).Validate(); err != nil {
		return
	}
}

// ChainedValidateAssign — should NOT be flagged.
func ChainedValidateAssign(input string) { // want `parameter "input" of castvalidation\.ChainedValidateAssign uses primitive type string`
	err := CommandName(input).Validate()
	_ = err
}

// ChainedNonValidate — SHOULD be flagged (chained method is not Validate).
func ChainedNonValidate(input string) { // want `parameter "input" of castvalidation\.ChainedNonValidate uses primitive type string`
	s := CommandName(input).String() // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = s
}

// --- Closure isolation tests (Issue 6 fix) ---

// CastInsideClosure — closure casts are NOT analyzed (skipped).
func CastInsideClosure(input string) { // want `parameter "input" of castvalidation\.CastInsideClosure uses primitive type string`
	go func() {
		name := CommandName(input) // NOT flagged — closure body skipped
		_ = name
	}()
}

// --- Error-message source auto-skip tests (Issue 7 improvement) ---

// CastFromErrorMethod — should NOT be flagged (source is .Error()).
func CastFromErrorMethod(err error) {
	msg := CommandName(err.Error())
	_ = msg
}

// CastFromFmtSprintf — should NOT be flagged (source is fmt.Sprintf).
func CastFromFmtSprintf(input string) { // want `parameter "input" of castvalidation\.CastFromFmtSprintf uses primitive type string`
	msg := CommandName(fmt.Sprintf("cmd: %s", input))
	_ = msg
}

// CastFromFmtErrorf — should NOT be flagged (source is fmt.Errorf .Error()).
func CastFromFmtErrorf() {
	err := fmt.Errorf("test error")
	msg := CommandName(err.Error())
	_ = msg
}

// UnassignedCastFromError — should NOT be flagged (source is .Error()).
func UnassignedCastFromError(err error) {
	useCmd(CommandName(err.Error())) // NOT flagged — error-message source
}

// CastFromPlainVariableStillFlagged — SHOULD still be flagged.
func CastFromPlainVariableStillFlagged(cmdName string) { // want `parameter "cmdName" of castvalidation\.CastFromPlainVariableStillFlagged uses primitive type string`
	name := CommandName(cmdName) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = name
}

// --- Switch tag auto-skip tests ---

// UnassignedSwitchTag — should NOT be flagged (auto-skip: switch tag).
func UnassignedSwitchTag(input string) { // want `parameter "input" of castvalidation\.UnassignedSwitchTag uses primitive type string`
	switch CommandName(input) { // NOT flagged — switch tag is comparison-like
	case CommandName("test"):
	default:
	}
}

// --- Multi-assignment cast tests ---

// MultiAssignCast tests a, b := DddType(x), DddType(y) pattern.
// First cast is validated, second is not.
func MultiAssignCast(x, y string) { // want `parameter "x" of castvalidation\.MultiAssignCast uses primitive type string` `parameter "y" of castvalidation\.MultiAssignCast uses primitive type string`
	a, b := CommandName(x), CommandName(y) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	if err := a.Validate(); err != nil {
		return
	}
	_ = a
	_ = b
}

// --- Ancestor auto-skip: nested composite literal inside fmt call ---

// CastInCompositeLitFmtArg — should NOT be flagged because the cast is
// nested inside a composite literal that is an argument to fmt.Sprintf.
// The entire expression tree is display-only.
func CastInCompositeLitFmtArg(input string) string { // want `parameter "input" of castvalidation\.CastInCompositeLitFmtArg uses primitive type string` `return value of castvalidation\.CastInCompositeLitFmtArg uses primitive type string`
	type display struct{ Name CommandName }
	return fmt.Sprintf("display: %v", display{Name: CommandName(input)})
}

// CastInSliceFmtArg — should NOT be flagged (slice inside fmt call).
func CastInSliceFmtArg(input string) string { // want `parameter "input" of castvalidation\.CastInSliceFmtArg uses primitive type string` `return value of castvalidation\.CastInSliceFmtArg uses primitive type string`
	return fmt.Sprintf("items: %v", []CommandName{CommandName(input)})
}
