// SPDX-License-Identifier: MPL-2.0

package skipfuncs

// init is skipped — no diagnostic expected.
func init() {
	var _ string = ""
}

// BenchmarkFoo is no longer skipped by prefix; only _test.go files are skipped.
func BenchmarkFoo(name string) { // want `parameter "name" of skipfuncs\.BenchmarkFoo uses primitive type string`
	_ = name
}

// FuzzBar is no longer skipped by prefix; only _test.go files are skipped.
func FuzzBar(data string) { // want `parameter "data" of skipfuncs\.FuzzBar uses primitive type string`
	_ = data
}

// ExampleBaz is skipped — no diagnostic expected.
func ExampleBaz() {
	var _ string = ""
}

// TestSomething is no longer skipped by prefix; only _test.go files are skipped.
func TestSomething(name string) { // want `parameter "name" of skipfuncs\.TestSomething uses primitive type string`
	_ = name
}

// NormalFunc is NOT skipped — should be flagged.
func NormalFunc(name string) { // want `parameter "name" of skipfuncs\.NormalFunc uses primitive type string`
	_ = name
}
