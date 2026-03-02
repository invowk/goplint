// SPDX-License-Identifier: MPL-2.0

package castvalidation

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	. "log/slog"
	"slices"
	"strconv"
	"strings"
)

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

// ErrorCode is a DDD Value Type that also implements the error interface.
// Used for errors.Is/errors.As auto-skip tests.
type ErrorCode int

func (e ErrorCode) Validate() error {
	if e < 100 || e > 599 {
		return fmt.Errorf("invalid error code: %d", int(e))
	}
	return nil
}

func (e ErrorCode) Error() string { return fmt.Sprintf("error code %d", int(e)) }

// NoValidate has no Validate method — casts to this should NOT be flagged.
type NoValidate string

func (n NoValidate) String() string { return string(n) }

// AnotherNamedType wraps string — not a raw primitive.
type AnotherNamedType string

// errorFieldHolder exposes a field named Error to ensure we do not treat any
// `.Error()` selector call as an error-message source.
type errorFieldHolder struct {
	Error func() string
}

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

// UnassignedMapKeyWrite — SHOULD be flagged (map assignment LHS key is a use).
func UnassignedMapKeyWrite(input string) { // want `parameter "input" of castvalidation\.UnassignedMapKeyWrite uses primitive type string`
	m := map[CommandName]bool{}
	m[CommandName(input)] = true // want `type conversion to CommandName from non-constant without Validate\(\) check`
}

// UnassignedMapKeyParenLookup — should NOT be flagged (auto-skip with parens).
func UnassignedMapKeyParenLookup(input string) { // want `parameter "input" of castvalidation\.UnassignedMapKeyParenLookup uses primitive type string`
	m := map[CommandName]bool{}
	_ = m[(CommandName(input))] // NOT flagged — map lookup with parenthesized cast
}

// UnassignedSliceIndexNotAutoSkip — SHOULD be flagged. Auto-skip for index
// expressions only applies to map-key lookups.
func UnassignedSliceIndexNotAutoSkip() {
	items := []string{"run", "build"}
	_ = items[PortNumber(runtimeInt())] // want `type conversion to PortNumber from non-constant without Validate\(\) check`
}

// UnassignedComparison — should NOT be flagged (auto-skip: comparison).
func UnassignedComparison(input string, expected CommandName) bool { // want `parameter "input" of castvalidation\.UnassignedComparison uses primitive type string`
	return CommandName(input) == expected // NOT flagged — comparison
}

// UnassignedComparisonParen — should NOT be flagged (comparison with parens).
func UnassignedComparisonParen(input string, expected CommandName) bool { // want `parameter "input" of castvalidation\.UnassignedComparisonParen uses primitive type string`
	return (CommandName(input)) == expected // NOT flagged — parenthesized comparison
}

// UnassignedSwitchParenTag — should NOT be flagged (switch tag with parens).
func UnassignedSwitchParenTag(input string) bool { // want `parameter "input" of castvalidation\.UnassignedSwitchParenTag uses primitive type string`
	switch CommandName(input) {
	case CommandName("run"):
		return true
	default:
		return false
	}
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

// CastWithVarDeclValidated — var declaration assignment should be tracked.
func CastWithVarDeclValidated(input string) { // want `parameter "input" of castvalidation\.CastWithVarDeclValidated uses primitive type string`
	var name CommandName = CommandName(input)
	if err := name.Validate(); err != nil {
		return
	}
	_ = name
}

// CastWithParenAssignmentValidated — parenthesized RHS should still be tracked
// as an assigned cast.
func CastWithParenAssignmentValidated(input string) { // want `parameter "input" of castvalidation\.CastWithParenAssignmentValidated uses primitive type string`
	name := (CommandName(input))
	if err := name.Validate(); err != nil {
		return
	}
	_ = name
}

// CastSelectorLHSValidated — selector assignment should be treated as assigned.
func CastSelectorLHSValidated(input string) { // want `parameter "input" of castvalidation\.CastSelectorLHSValidated uses primitive type string`
	cfg := struct {
		Name CommandName
	}{}
	cfg.Name = CommandName(input)
	if err := cfg.Name.Validate(); err != nil {
		return
	}
	_ = cfg
}

// CastWithAddressOfValidateReceiver — should NOT be flagged. Address-of on the
// Validate receiver must canonicalize to the assigned target.
func CastWithAddressOfValidateReceiver(input string) { // want `parameter "input" of castvalidation\.CastWithAddressOfValidateReceiver uses primitive type string`
	name := CommandName(input)
	if err := (&name).Validate(); err != nil {
		return
	}
	_ = name
}

// IndexedLHSParenCanonicalization — should NOT be flagged. Parentheses on
// an indexed Validate receiver must canonicalize to the same cast target.
func IndexedLHSParenCanonicalization() {
	ports := []PortNumber{0}
	ports[0] = PortNumber(runtimeInt())
	if err := ports[(0)].Validate(); err != nil {
		return
	}
	_ = ports
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

// CastInsideClosure — asynchronous closure casts are still checked.
func CastInsideClosure(input string) { // want `parameter "input" of castvalidation\.CastInsideClosure uses primitive type string`
	go func() {
		name := CommandName(input) // want `type conversion to CommandName from non-constant without Validate\(\) check`
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

// CastFromStrconvItoa — should NOT be flagged (source is strconv formatting).
func CastFromStrconvItoa(v int) { // want `parameter "v" of castvalidation\.CastFromStrconvItoa uses primitive type int`
	msg := CommandName(strconv.Itoa(v))
	_ = msg
}

// CastFromStrconvFormatInt — should NOT be flagged (source is strconv formatting).
func CastFromStrconvFormatInt(v int64) { // want `parameter "v" of castvalidation\.CastFromStrconvFormatInt uses primitive type int64`
	msg := CommandName(strconv.FormatInt(v, 10))
	_ = msg
}

// UnassignedCastFromError — should NOT be flagged (source is .Error()).
func UnassignedCastFromError(err error) {
	useCmd(CommandName(err.Error())) // NOT flagged — error-message source
}

// CastFromNonErrorErrorFieldCall — SHOULD be flagged. A selector call named
// Error() is only auto-skipped when the receiver actually implements error.
func CastFromNonErrorErrorFieldCall(input string) { // want `parameter "input" of castvalidation\.CastFromNonErrorErrorFieldCall uses primitive type string`
	holder := errorFieldHolder{
		Error: func() string { return input },
	}
	name := CommandName(holder.Error()) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = name
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

// --- log/slog auto-skip: display-only logging sinks ---

// LogPrintfAutoSkip — should NOT be flagged (log.Printf is display-only).
func LogPrintfAutoSkip(input string) { // want `parameter "input" of castvalidation\.LogPrintfAutoSkip uses primitive type string`
	log.Printf("cmd: %s", CommandName(input)) // NOT flagged — display only
}

// SlogInfoAutoSkip — should NOT be flagged (slog.Info is display-only).
func SlogInfoAutoSkip(input string) { // want `parameter "input" of castvalidation\.SlogInfoAutoSkip uses primitive type string`
	Info("cmd", "name", CommandName(input)) // NOT flagged — display only (dot import)
}

// LogPrintAutoSkip — should NOT be flagged (log.Print is display-only).
func LogPrintAutoSkip(input string) { // want `parameter "input" of castvalidation\.LogPrintAutoSkip uses primitive type string`
	log.Print("cmd: ", CommandName(input)) // NOT flagged — display only
}

// --- Ancestor depth limit tests (maxAncestorDepth = 5) ---

// CastAtAncestorDepthWithinLimit — should NOT be flagged because the cast
// is nested inside a 2-level composite literal chain within fmt.Sprintf.
// The ancestor walk reaches fmt.Sprintf at hop 4 (within maxAncestorDepth=5):
//
//	start = KeyValueExpr(V: cast)
//	hop 1 = CompositeLit(Inner{...})
//	hop 2 = KeyValueExpr(V: Inner{...})
//	hop 3 = CompositeLit(Outer{...})
//	hop 4 = CallExpr(fmt.Sprintf) → found, auto-skip
func CastAtAncestorDepthWithinLimit(input string) string { // want `parameter "input" of castvalidation\.CastAtAncestorDepthWithinLimit uses primitive type string` `return value of castvalidation\.CastAtAncestorDepthWithinLimit uses primitive type string`
	type inner struct{ V CommandName }
	type outer struct{ V inner }
	return fmt.Sprintf("%v", outer{V: inner{V: CommandName(input)}})
}

// CastBeyondAncestorDepthLimit — SHOULD be flagged because the cast is
// nested inside a 3-level composite literal chain within fmt.Sprintf.
// The ancestor walk exhausts all 5 iterations before reaching fmt.Sprintf:
//
//	start = KeyValueExpr(V: cast)
//	hop 1 = CompositeLit(L1{...})
//	hop 2 = KeyValueExpr(V: L1{...})
//	hop 3 = CompositeLit(L2{...})
//	hop 4 = KeyValueExpr(V: L2{...})
//	hop 5 = CompositeLit(L3{...}) — NOT a call, loop ends
//	fmt.Sprintf would be at hop 6, beyond maxAncestorDepth
func CastBeyondAncestorDepthLimit(input string) string { // want `parameter "input" of castvalidation\.CastBeyondAncestorDepthLimit uses primitive type string` `return value of castvalidation\.CastBeyondAncestorDepthLimit uses primitive type string`
	type l1 struct{ V CommandName }
	type l2 struct{ V l1 }
	type l3 struct{ V l2 }
	return fmt.Sprintf("%v", l3{V: l2{V: l1{V: CommandName(input)}}}) // want `type conversion to CommandName from non-constant without Validate\(\) check`
}

// --- strings.* comparison auto-skip tests ---

// StringsContainsAutoSkip — should NOT be flagged (strings.Contains is
// a comparison predicate — the cast value is tested, not used as domain input).
func StringsContainsAutoSkip(input string) bool { // want `parameter "input" of castvalidation\.StringsContainsAutoSkip uses primitive type string`
	return strings.Contains(string(CommandName(input)), "prefix") // NOT flagged — comparison
}

// StringsHasPrefixAutoSkip — should NOT be flagged.
func StringsHasPrefixAutoSkip(input string) bool { // want `parameter "input" of castvalidation\.StringsHasPrefixAutoSkip uses primitive type string`
	return strings.HasPrefix(string(CommandName(input)), "cmd") // NOT flagged — comparison
}

// StringsHasSuffixAutoSkip — should NOT be flagged.
func StringsHasSuffixAutoSkip(input string) bool { // want `parameter "input" of castvalidation\.StringsHasSuffixAutoSkip uses primitive type string`
	return strings.HasSuffix(string(CommandName(input)), "-run") // NOT flagged — comparison
}

// StringsEqualFoldAutoSkip — should NOT be flagged.
func StringsEqualFoldAutoSkip(input string) bool { // want `parameter "input" of castvalidation\.StringsEqualFoldAutoSkip uses primitive type string`
	return strings.EqualFold(string(CommandName(input)), "RUN") // NOT flagged — comparison
}

// StringsReplaceNotSkipped — SHOULD be flagged because strings.ReplaceAll
// is not a comparison function — it processes the domain value.
func StringsReplaceNotSkipped(input string) string { // want `parameter "input" of castvalidation\.StringsReplaceNotSkipped uses primitive type string` `return value of castvalidation\.StringsReplaceNotSkipped uses primitive type string`
	return strings.ReplaceAll(string(CommandName(input)), "-", "_") // want `type conversion to CommandName from non-constant without Validate\(\) check`
}

// --- slices.* comparison auto-skip tests ---

// SlicesContainsAutoSkip — should NOT be flagged (slices.Contains is
// a membership predicate — the cast value is tested, not consumed).
func SlicesContainsAutoSkip(input string) bool { // want `parameter "input" of castvalidation\.SlicesContainsAutoSkip uses primitive type string`
	items := []CommandName{CommandName("run"), CommandName("build")}
	return slices.Contains(items, CommandName(input)) // NOT flagged — comparison
}

// SlicesIndexAutoSkip — should NOT be flagged (slices.Index is
// a lookup predicate — the cast value is tested for position, not consumed).
func SlicesIndexAutoSkip(input string) int { // want `parameter "input" of castvalidation\.SlicesIndexAutoSkip uses primitive type string` `return value of castvalidation\.SlicesIndexAutoSkip uses primitive type int`
	items := []CommandName{CommandName("run"), CommandName("build")}
	return slices.Index(items, CommandName(input)) // NOT flagged — comparison
}

// SlicesSortNotSkipped — SHOULD be flagged because slices.SortFunc
// is not a comparison function — it processes/mutates the domain values.
func SlicesSortNotSkipped(input string) { // want `parameter "input" of castvalidation\.SlicesSortNotSkipped uses primitive type string`
	items := []CommandName{CommandName(input)} // want `type conversion to CommandName from non-constant without Validate\(\) check`
	slices.SortFunc(items, func(a, b CommandName) int { return 0 })
	_ = items
}

// --- errors.Is/errors.As comparison auto-skip tests ---

// ErrorsIsAutoSkip — should NOT be flagged (errors.Is is a type/identity
// comparison operation — the cast value is used for error matching).
func ErrorsIsAutoSkip(input int) bool { // want `parameter "input" of castvalidation\.ErrorsIsAutoSkip uses primitive type int`
	err := errors.New("test")
	return errors.Is(err, ErrorCode(input)) // NOT flagged — comparison
}

// ErrorsAsAutoSkip — should NOT be flagged (errors.As is a type-matching
// comparison operation). The cast ErrorCode(runtimeInt()) appears as an
// argument to errors.As, which is an auto-skip context.
func ErrorsAsAutoSkip() bool {
	err := errors.New("test")
	return errors.As(err, ErrorCode(runtimeInt())) // NOT flagged — comparison
}

// --- bytes.Contains/HasPrefix/HasSuffix/EqualFold comparison auto-skip tests ---

// BytesContainsAutoSkip — should NOT be flagged (bytes.Contains is
// a comparison predicate — the cast value is tested for containment).
func BytesContainsAutoSkip(input string) bool { // want `parameter "input" of castvalidation\.BytesContainsAutoSkip uses primitive type string`
	return bytes.Contains([]byte(string(CommandName(input))), []byte("run")) // NOT flagged — comparison
}

// BytesHasPrefixAutoSkip — should NOT be flagged (bytes.HasPrefix is
// a comparison predicate — the cast value is tested for prefix matching).
func BytesHasPrefixAutoSkip(input string) bool { // want `parameter "input" of castvalidation\.BytesHasPrefixAutoSkip uses primitive type string`
	return bytes.HasPrefix([]byte(string(CommandName(input))), []byte("cmd")) // NOT flagged — comparison
}

// BytesHasSuffixAutoSkip — should NOT be flagged (bytes.HasSuffix is
// a comparison predicate — the cast value is tested for suffix matching).
func BytesHasSuffixAutoSkip(input string) bool { // want `parameter "input" of castvalidation\.BytesHasSuffixAutoSkip uses primitive type string`
	return bytes.HasSuffix([]byte(string(CommandName(input))), []byte("-run")) // NOT flagged — comparison
}

// BytesEqualFoldAutoSkip — should NOT be flagged (bytes.EqualFold is
// a comparison predicate — the cast value is tested for case-insensitive equality).
func BytesEqualFoldAutoSkip(input string) bool { // want `parameter "input" of castvalidation\.BytesEqualFoldAutoSkip uses primitive type string`
	return bytes.EqualFold([]byte(string(CommandName(input))), []byte("RUN")) // NOT flagged — comparison
}
