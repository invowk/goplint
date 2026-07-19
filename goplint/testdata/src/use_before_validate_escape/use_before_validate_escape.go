// SPDX-License-Identifier: MPL-2.0

package use_before_validate_escape

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

func useCmd(_ CommandName) {}

func validateFirst(x CommandName) {
	_ = x.Validate()
}

func validateViaWrapper(x CommandName) {
	validateFirst(x)
}

func pureHelper(_ CommandName) {}

func preserveHelper(x CommandName) {
	_ = x
}

func conditionalValidate(x CommandName, cond bool) {
	if cond {
		_ = x.Validate()
	}
}

func escapeThenValidate(x CommandName) {
	useCmd(x)
	_ = x.Validate()
}

func recursiveValidateA(x CommandName) {
	recursiveValidateB(x)
}

func recursiveValidateB(x CommandName) {
	recursiveValidateA(x)
}

// DelegatedValidationCoversPath should not be flagged: validateFirst guarantees
// first-arg validation before escape and there is no unvalidated return path.
func DelegatedValidationCoversPath(raw string) error { // want `parameter "raw" of use_before_validate_escape\.DelegatedValidationCoversPath uses primitive type string`
	x := CommandName(raw)
	validateFirst(x)
	return x.Validate()
}

// DelegatedValidationViaWrapper should not be flagged: transitive summaries
// through wrapper calls still prove first-arg validation before escape.
func DelegatedValidationViaWrapper(raw string) error { // want `parameter "raw" of use_before_validate_escape\.DelegatedValidationViaWrapper uses primitive type string`
	x := CommandName(raw)
	validateViaWrapper(x)
	return x.Validate()
}

// PureSummaryDoesNotConsumeTarget proves a known pure call does not create a
// pre-validation use or escape for the tracked value.
func PureSummaryDoesNotConsumeTarget(raw string) error { // want `parameter "raw" of use_before_validate_escape\.PureSummaryDoesNotConsumeTarget uses primitive type string`
	x := CommandName(raw)
	pureHelper(x)
	return x.Validate()
}

// PreserveSummaryDoesNotConsumeTarget proves a known preserving call leaves
// the tracked validation obligation unchanged.
func PreserveSummaryDoesNotConsumeTarget(raw string) error { // want `parameter "raw" of use_before_validate_escape\.PreserveSummaryDoesNotConsumeTarget uses primitive type string`
	x := CommandName(raw)
	preserveHelper(x)
	return x.Validate()
}

// ConditionalDelegatedValidation should be flagged for UBV in escape mode:
// conditionalValidate does not guarantee validation on all paths.
func ConditionalDelegatedValidation(raw string, cond bool) error { // want `parameter "raw" of use_before_validate_escape\.ConditionalDelegatedValidation uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	conditionalValidate(x, cond)
	return x.Validate()
}

// EscapeBeforeValidateInHelper should be flagged for UBV in escape mode:
// helper usage escapes x before helper-level Validate().
func EscapeBeforeValidateInHelper(raw string) error { // want `parameter "raw" of use_before_validate_escape\.EscapeBeforeValidateInHelper uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	escapeThenValidate(x)
	return x.Validate()
}

// RecursiveCycleConservative reports a definite UBV escape: the finite
// recursive summary proves that the raw value is repeatedly passed onward
// without validation.
func RecursiveCycleConservative(raw string) error { // want `parameter "raw" of use_before_validate_escape\.RecursiveCycleConservative uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	recursiveValidateA(x)
	return x.Validate()
}

type CommandConsumer interface {
	Consume(CommandName)
}

// UnresolvedRelevantCall stays inconclusive because an interface dispatch can
// change the tracked validation obligation and has no compatible summary.
func UnresolvedRelevantCall(raw string, consumer CommandConsumer) error { // want `parameter "raw" of use_before_validate_escape\.UnresolvedRelevantCall uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName has inconclusive use-before-validate path analysis`
	consumer.Consume(x)
	return x.Validate()
}
