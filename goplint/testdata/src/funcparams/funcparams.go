// SPDX-License-Identifier: MPL-2.0

package funcparams

// CommandName is a DDD Value Type.
type CommandName string

// BadFunc has bare primitive parameters and return.
func BadFunc(name string, count int) string { // want `parameter "name" of funcparams\.BadFunc uses primitive type string` `parameter "count" of funcparams\.BadFunc uses primitive type int` `return value of funcparams\.BadFunc uses primitive type string`
	_ = count
	return name
}

// GoodFunc uses named types — should not be flagged.
func GoodFunc(name CommandName) CommandName {
	return name
}

// MixedFunc has both good and bad params.
func MixedFunc(name CommandName, label string) { // want `parameter "label" of funcparams\.MixedFunc uses primitive type string`
	_ = name
	_ = label
}

// BoolParam uses bool — exempt.
func BoolParam(verbose bool) {
	_ = verbose
}

// ErrorReturn returns error — not flagged.
func ErrorReturn() error {
	return nil
}

// SliceParam has a slice of primitives.
func SliceParam(args []string) { // want `parameter "args" of funcparams\.SliceParam uses primitive type \[\]string`
	_ = args
}

// NamedReturn has named return values.
func NamedReturn() (result string, err error) { // want `return value "result" of funcparams\.NamedReturn uses primitive type string`
	return "", nil
}
