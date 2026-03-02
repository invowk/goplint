// SPDX-License-Identifier: MPL-2.0

package castvalidation_nocfa_errors_noncomparison

import (
	"errors"
	"fmt"
)

type CommandError string

func (e CommandError) Error() string { return string(e) }

func (e CommandError) Validate() error {
	if e == "" {
		return fmt.Errorf("invalid")
	}
	return nil
}

var ErrSentinel = errors.New("sentinel")

func ErrorsComparisonNoFinding(raw string) { // want `parameter "raw" of castvalidation_nocfa_errors_noncomparison\.ErrorsComparisonNoFinding uses primitive type string`
	_ = errors.Is(CommandError(raw), ErrSentinel)
}

func ErrorsJoinReports(raw string) { // want `parameter "raw" of castvalidation_nocfa_errors_noncomparison\.ErrorsJoinReports uses primitive type string`
	_ = errors.Join(CommandError(raw), ErrSentinel) // want `type conversion to CommandError from non-constant without Validate\(\) check`
}
