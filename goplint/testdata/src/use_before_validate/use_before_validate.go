// SPDX-License-Identifier: MPL-2.0

// Package use_before_validate provides test fixtures for the
// --check-use-before-validate mode. This mode detects when a DDD Value
// Type variable is used (passed as a function argument or non-display
// method receiver) before Validate() is called in the same basic block.
package use_before_validate

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

// --- Helper functions ---

func runtimeString() string { return "test" } // want `return value of use_before_validate\.runtimeString uses primitive type string`

func useCmd(_ CommandName) {}

// IsBuiltin is a non-display method on CommandName used to test
// that non-display method receivers are detected as "uses" by UBV.
func (c CommandName) IsBuiltin() bool { return c == "help" }

// GoString implements fmt.GoStringer (display-only, exempt from UBV).
func (c CommandName) GoString() string { return "CommandName(" + string(c) + ")" }

// Server is a type with methods for receiver-use testing.
type Server struct {
	cmd CommandName
}

func (s *Server) Setup()          {}
func (s *Server) Validate() error { return nil }

// --- Use-before-validate test cases ---

// UseBeforeValidate — SHOULD be flagged. The variable x is passed to
// useCmd() before x.Validate() is called in the same block.
func UseBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate\.UseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	useCmd(x)
	return x.Validate()
}

// ValidateBeforeUse — should NOT be flagged. Validate() is called
// before the variable is used.
func ValidateBeforeUse(raw string) { // want `parameter "raw" of use_before_validate\.ValidateBeforeUse uses primitive type string`
	x := CommandName(raw)
	if err := x.Validate(); err != nil {
		return
	}
	useCmd(x)
}

// StringBeforeValidate — should NOT be flagged. String() is a
// display-only method and does not count as a "use."
func StringBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate\.StringBeforeValidate uses primitive type string`
	x := CommandName(raw)
	fmt.Println(x.String())
	return x.Validate()
}

// ErrorBeforeValidate — should NOT be flagged. Error() is display-only.
func ErrorBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate\.ErrorBeforeValidate uses primitive type string`
	x := CommandName(raw)
	_ = fmt.Sprintf("%s", x.String())
	return x.Validate()
}

// NoUseAtAll — should NOT be flagged. The cast is assigned and validated
// without any intervening use.
func NoUseAtAll(raw string) error { // want `parameter "raw" of use_before_validate\.NoUseAtAll uses primitive type string`
	x := CommandName(raw)
	return x.Validate()
}

// NoValidateButNoUse — NOT flagged by UBV (no use before validate).
// This IS flagged by the path-to-return check (unvalidated-cast).
func NoValidateButNoUse(raw string) { // want `parameter "raw" of use_before_validate\.NoValidateButNoUse uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = x
}

// UseInFuncArgBeforeValidate — SHOULD be flagged. The variable is
// passed as a function argument before Validate().
func UseInFuncArgBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate\.UseInFuncArgBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	fmt.Println(x)        // passes x as an argument (not x.String())
	return x.Validate()
}

// --- Method receiver UBV tests ---

// MethodReceiverUseBeforeValidate — SHOULD be flagged. The variable x
// is used as a method receiver (x.IsBuiltin()) before Validate().
// IsBuiltin is not a display-only method (not String/Error/GoString/Validate).
func MethodReceiverUseBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate\.MethodReceiverUseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	_ = x.IsBuiltin()
	return x.Validate()
}

// GoStringBeforeValidate — should NOT be flagged. GoString() is
// display-only (implements fmt.GoStringer) and does not count as a "use."
func GoStringBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate\.GoStringBeforeValidate uses primitive type string`
	x := CommandName(raw)
	_ = x.GoString()
	return x.Validate()
}

// MethodReceiverValidateFirst — should NOT be flagged. Validate() is
// called before the non-display method.
func MethodReceiverValidateFirst(raw string) { // want `parameter "raw" of use_before_validate\.MethodReceiverValidateFirst uses primitive type string`
	x := CommandName(raw)
	if err := x.Validate(); err != nil {
		return
	}
	_ = x.IsBuiltin()
}

// --- Composite literal UBV tests ---

// Holder is a struct used to test composite literal UBV detection.
type Holder struct {
	Cmd CommandName
}

// StructLiteralUseBeforeValidate — SHOULD be flagged. The variable x
// is placed in a struct literal field before Validate() is called.
func StructLiteralUseBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate\.StructLiteralUseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	_ = Holder{Cmd: x}
	return x.Validate()
}

// StructLiteralAfterValidate — should NOT be flagged. Validate() is
// called before the variable is placed in a struct literal.
func StructLiteralAfterValidate(raw string) { // want `parameter "raw" of use_before_validate\.StructLiteralAfterValidate uses primitive type string`
	x := CommandName(raw)
	if err := x.Validate(); err != nil {
		return
	}
	_ = Holder{Cmd: x}
}

// MapLiteralUseBeforeValidate — SHOULD be flagged. The variable x
// is placed in a map literal value before Validate() is called.
func MapLiteralUseBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate\.MapLiteralUseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	_ = map[string]CommandName{"key": x}
	return x.Validate()
}

// MapLiteralKeyUseBeforeValidate — SHOULD be flagged. The variable x
// is used as a map literal key before Validate() is called.
func MapLiteralKeyUseBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate\.MapLiteralKeyUseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	_ = map[CommandName]string{x: "value"}
	return x.Validate()
}

// --- Channel send UBV tests ---

// ChannelSendBeforeValidate — SHOULD be flagged. The variable x
// is sent on a channel before Validate() is called.
func ChannelSendBeforeValidate(raw string, ch chan CommandName) error { // want `parameter "raw" of use_before_validate\.ChannelSendBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	ch <- x
	return x.Validate()
}

// ChannelSendAfterValidate — should NOT be flagged. Validate() is
// called before the variable is sent on a channel.
func ChannelSendAfterValidate(raw string, ch chan CommandName) { // want `parameter "raw" of use_before_validate\.ChannelSendAfterValidate uses primitive type string`
	x := CommandName(raw)
	if err := x.Validate(); err != nil {
		return
	}
	ch <- x
}

// DeferredValidateBeforeUse — SHOULD be flagged. Deferred Validate executes
// at function exit and must not suppress a prior use in UBV checks.
func DeferredValidateBeforeUse(raw string) error { // want `parameter "raw" of use_before_validate\.DeferredValidateBeforeUse uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	defer func() {
		_ = x.Validate()
	}()
	useCmd(x)
	return nil
}

// ImmediateValidateBeforeUse — should NOT be flagged. IIFE Validate executes
// immediately and can satisfy UBV ordering before later uses.
func ImmediateValidateBeforeUse(raw string) { // want `parameter "raw" of use_before_validate\.ImmediateValidateBeforeUse uses primitive type string`
	x := CommandName(raw)
	func() {
		_ = x.Validate()
	}()
	useCmd(x)
}

func useThenValidate(_ CommandName, _ error) {}

func validateThenUse(_ error, _ CommandName) {}

// UseThenValidateInSameNode — SHOULD be flagged. Go evaluates call arguments
// left-to-right, so x is used before x.Validate() within one node.
func UseThenValidateInSameNode(raw string) { // want `parameter "raw" of use_before_validate\.UseThenValidateInSameNode uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	useThenValidate(x, x.Validate())
}

// ValidateThenUseInSameNode — should NOT be flagged. Validate argument is
// evaluated before the value-use argument in the same call expression.
func ValidateThenUseInSameNode(raw string) { // want `parameter "raw" of use_before_validate\.ValidateThenUseInSameNode uses primitive type string`
	x := CommandName(raw)
	validateThenUse(x.Validate(), x)
}
