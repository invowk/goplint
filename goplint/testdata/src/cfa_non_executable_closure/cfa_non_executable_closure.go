// SPDX-License-Identifier: MPL-2.0

package cfa_non_executable_closure

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid")
	}
	return nil
}

func useCmd(_ CommandName) {}

// PackageCallback proves that package-initializer function literals are
// independent protocol procedures even when no call is visible.
var PackageCallback = func(raw string) {
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	useCmd(x)
}

// DetachedClosureLiteral is still an independently analyzable procedure even
// though no invocation is visible in this package.
func DetachedClosureLiteral(raw string) { // want `parameter "raw" of cfa_non_executable_closure\.DetachedClosureLiteral uses primitive type string`
	_ = func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}
}

// ImmediateClosureLiteral is executable and should still be analyzed.
func ImmediateClosureLiteral(raw string) { // want `parameter "raw" of cfa_non_executable_closure\.ImmediateClosureLiteral uses primitive type string`
	func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}()
}
