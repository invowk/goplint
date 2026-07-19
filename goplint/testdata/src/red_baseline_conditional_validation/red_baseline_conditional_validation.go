// SPDX-License-Identifier: MPL-2.0

package red_baseline_conditional_validation

type Name string

func (name Name) Validate() error { return nil }

func consume(Name) {}

func FailureContinuation(raw string) { // want `parameter "raw" of red_baseline_conditional_validation\.FailureContinuation uses primitive type string`
	name := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
	if err := name.Validate(); err != nil {
		consume(name)
		return
	}
}
