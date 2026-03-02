// SPDX-License-Identifier: MPL-2.0

package use_before_validate_closure

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command name")
	}
	return nil
}

func useCmd(_ CommandName) {}

// IIFEUseBeforeValidate — SHOULD be flagged. The synchronous closure uses x
// before x.Validate() is reached in the enclosing block.
func IIFEUseBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate_closure\.IIFEUseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	func() {
		useCmd(x)
	}()
	return x.Validate()
}

// IIFEUseAfterValidate — should NOT be flagged. Validation occurs before use.
func IIFEUseAfterValidate(raw string) error { // want `parameter "raw" of use_before_validate_closure\.IIFEUseAfterValidate uses primitive type string`
	x := CommandName(raw)
	if err := x.Validate(); err != nil {
		return err
	}
	func() {
		useCmd(x)
	}()
	return nil
}

// GoroutineUseBeforeValidate — should NOT be flagged by UBV. Goroutine
// closures execute asynchronously and are excluded from synchronous use checks.
func GoroutineUseBeforeValidate(raw string) error { // want `parameter "raw" of use_before_validate_closure\.GoroutineUseBeforeValidate uses primitive type string`
	x := CommandName(raw)
	go func() {
		useCmd(x)
	}()
	return x.Validate()
}

// CrossBlockIIFEUseBeforeValidate — SHOULD be flagged by cross-block UBV.
// The cast occurs in the entry block and the use occurs in an IIFE within a
// successor block before the shared Validate() call.
func CrossBlockIIFEUseBeforeValidate(raw string, cond bool) error { // want `parameter "raw" of use_before_validate_closure\.CrossBlockIIFEUseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) across blocks`
	if cond {
		func() {
			useCmd(x)
		}()
	}
	return x.Validate()
}

// DeferredValidateDoesNotSuppressUBV — SHOULD be flagged. Deferred Validate
// must not suppress use-before-validate in the enclosing block.
func DeferredValidateDoesNotSuppressUBV(raw string) error { // want `parameter "raw" of use_before_validate_closure\.DeferredValidateDoesNotSuppressUBV uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	defer func() {
		_ = x.Validate()
	}()
	useCmd(x)
	return nil
}

// DirectDeferValidateIgnoredForOrdering — SHOULD be flagged. A direct
// defer x.Validate() must not count as validation-before-use for UBV ordering.
func DirectDeferValidateIgnoredForOrdering(raw string) error { // want `parameter "raw" of use_before_validate_closure\.DirectDeferValidateIgnoredForOrdering uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	defer x.Validate()
	useCmd(x)
	return x.Validate()
}

// DirectGoValidateIgnoredForOrdering — SHOULD be flagged. A direct
// go x.Validate() call is asynchronous and must not satisfy UBV ordering.
func DirectGoValidateIgnoredForOrdering(raw string) error { // want `parameter "raw" of use_before_validate_closure\.DirectGoValidateIgnoredForOrdering uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	go x.Validate()
	useCmd(x)
	return x.Validate()
}

// IIFEValidateBeforeUseCounts — should NOT be flagged. Immediate closure
// Validate runs before use and should count for UBV ordering.
func IIFEValidateBeforeUseCounts(raw string) { // want `parameter "raw" of use_before_validate_closure\.IIFEValidateBeforeUseCounts uses primitive type string`
	x := CommandName(raw)
	func() {
		_ = x.Validate()
	}()
	useCmd(x)
}
