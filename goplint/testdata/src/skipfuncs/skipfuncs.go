// SPDX-License-Identifier: MPL-2.0

package skipfuncs

// init is skipped — no diagnostic expected.
func init() {
	var _ string = ""
}

// BenchmarkFoo is skipped — no diagnostic expected.
func BenchmarkFoo(name string) {
	_ = name
}

// FuzzBar is skipped — no diagnostic expected.
func FuzzBar(data string) {
	_ = data
}

// ExampleBaz is skipped — no diagnostic expected.
func ExampleBaz() {
	var _ string = ""
}

// TestSomething is skipped — no diagnostic expected.
func TestSomething(name string) {
	_ = name
}

// NormalFunc is NOT skipped — should be flagged.
func NormalFunc(name string) { // want `parameter "name" of skipfuncs\.NormalFunc uses primitive type string`
	_ = name
}
