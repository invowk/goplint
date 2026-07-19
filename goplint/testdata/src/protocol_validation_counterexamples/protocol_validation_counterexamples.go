// SPDX-License-Identifier: MPL-2.0

package protocol_validation_counterexamples

import "log"

type CommandName string

func (name CommandName) Validate() error { return nil }

func use(CommandName) {}

func Check(name CommandName) error { // want Check:"protocol-summary:v5:protocol_validation_counterexamples:protocol_validation_counterexamples.Check:1"
	return name.Validate()
}

func Checked(raw string) error { // want `parameter "raw" of protocol_validation_counterexamples\.Checked uses primitive type string`
	name := CommandName(raw)
	if err := name.Validate(); err != nil {
		return err
	}
	return nil
}

func ContinuedAfterFailure(raw string) error { // want `parameter "raw" of protocol_validation_counterexamples\.ContinuedAfterFailure uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	if err := name.Validate(); err != nil {
		log.Print(err)
	}
	return nil
}

func ConsumedOnFailure(raw string) error { // want `parameter "raw" of protocol_validation_counterexamples\.ConsumedOnFailure uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	if err := name.Validate(); err != nil {
		use(name)
		return nil
	}
	return nil
}

func AssignedAndLogged(raw string) error { // want `parameter "raw" of protocol_validation_counterexamples\.AssignedAndLogged uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	err := name.Validate()
	log.Print(err)
	return nil
}

func BlankValidationError(raw string) error { // want `parameter "raw" of protocol_validation_counterexamples\.BlankValidationError uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = name.Validate()
	return nil
}

func DiscardedMethodValueError(raw string) error { // want `parameter "raw" of protocol_validation_counterexamples\.DiscardedMethodValueError uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	validate := name.Validate
	_ = validate()
	return nil
}

func SuccessBranchTerminates(raw string) error { // want `parameter "raw" of protocol_validation_counterexamples\.SuccessBranchTerminates uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	if err := name.Validate(); err == nil {
		return nil
	}
	use(name)
	return nil
}

func DiscardedHelperResult(raw string) error { // want `parameter "raw" of protocol_validation_counterexamples\.DiscardedHelperResult uses primitive type string`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = Check(name)
	return nil
}

func CheckedHelperResult(raw string) error { // want `parameter "raw" of protocol_validation_counterexamples\.CheckedHelperResult uses primitive type string`
	name := CommandName(raw)
	if err := Check(name); err != nil {
		return err
	}
	return nil
}
