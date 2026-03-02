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

// DetachedClosureLiteral should not produce cast-validation diagnostics because
// the closure literal is never invoked.
func DetachedClosureLiteral(raw string) { // want `parameter "raw" of cfa_non_executable_closure\.DetachedClosureLiteral uses primitive type string`
	_ = func() {
		x := CommandName(raw)
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
