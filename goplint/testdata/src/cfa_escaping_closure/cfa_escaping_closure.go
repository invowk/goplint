// SPDX-License-Identifier: MPL-2.0

package cfa_escaping_closure

type Name string

func (Name) Validate() error { return nil }

func consume(Name) {}

func register(func()) {}

var stored func()

var callbacks []func()

// Returned contains executable package code even though its invocation is not
// visible in the enclosing body.
func Returned(raw string) func() { // want `parameter "raw" of cfa_escaping_closure\.Returned uses primitive type string`
	return func() {
		value := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
		consume(value)
	}
}

func Stored(raw string) { // want `parameter "raw" of cfa_escaping_closure\.Stored uses primitive type string`
	stored = func() {
		value := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
		consume(value)
	}
}

func Passed(raw string) { // want `parameter "raw" of cfa_escaping_closure\.Passed uses primitive type string`
	register(func() {
		value := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
		consume(value)
	})
}

func Callback(raw string) { // want `parameter "raw" of cfa_escaping_closure\.Callback uses primitive type string`
	callbacks = append(callbacks, func() {
		value := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
		consume(value)
	})
}

func NestedReturned(raw string) func() { // want `parameter "raw" of cfa_escaping_closure\.NestedReturned uses primitive type string`
	return func() {
		inner := func() {
			value := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
			consume(value)
		}
		inner()
	}
}

func RecursiveCallback(raw string) func() { // want `parameter "raw" of cfa_escaping_closure\.RecursiveCallback uses primitive type string`
	var callback func()
	callback = func() {
		value := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
		consume(value)
		if raw == "again" {
			callback()
		}
	}
	return callback
}
