// SPDX-License-Identifier: MPL-2.0

package lib

type Name string

func (Name) Validate() error { return nil }

func consume(Name) {}

// Returned proves a closure-local obligation is analyzed in the package that
// defines it even when its only visible consumer is in another package.
func Returned(raw string) func() { // want `parameter "raw" of lib\.Returned uses primitive type string`
	return func() {
		value := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
		consume(value)
	}
}
