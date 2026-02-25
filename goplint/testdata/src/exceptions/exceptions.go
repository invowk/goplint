// SPDX-License-Identifier: MPL-2.0

package exceptions

// ExceptedStruct has fields with various ignore directive forms.
type ExceptedStruct struct {
	Name  string //goplint:ignore -- display-only label
	Age   int    //nolint:goplint
	Score int    //plint:ignore -- short-form alias
	Bad   string // want `struct field exceptions\.ExceptedStruct\.Bad uses primitive type string`
}

// ExceptedFunc has an ignore directive on the whole function.
//
//goplint:ignore
func ExceptedFunc(name string) string {
	return name
}

// NotExceptedFunc does NOT have an ignore directive.
func NotExceptedFunc(name string) string { // want `parameter "name" of exceptions\.NotExceptedFunc uses primitive type string` `return value of exceptions\.NotExceptedFunc uses primitive type string`
	return name
}

// CombinedDirectiveStruct tests comma-separated directive parsing.
type CombinedDirectiveStruct struct {
	Both    string //plint:ignore,internal -- both directives active, no primitive warning
	Reverse string //plint:internal,ignore -- reverse order, same effect
	Bad     string // want `struct field exceptions\.CombinedDirectiveStruct\.Bad uses primitive type string`
}

// UnknownDirectiveStruct tests unknown directive key warnings.
type UnknownDirectiveStruct struct {
	Name string //plint:ignore,foo -- want unknown-directive warning for "foo"  // want `unknown directive key "foo" in goplint directive`
}

// --- Render directive tests ---

// RenderFunc returns rendered display text — return type suppressed,
// but params are still checked.
//
//plint:render
func RenderFunc(name string) string { // want `parameter "name" of exceptions\.RenderFunc uses primitive type string`
	return "rendered: " + name
}

// NonRenderFunc has no render directive — both param and return flagged.
func NonRenderFunc(name string) string { // want `parameter "name" of exceptions\.NonRenderFunc uses primitive type string` `return value of exceptions\.NonRenderFunc uses primitive type string`
	return name
}

// RenderFieldStruct has a field with render directive — suppresses finding.
type RenderFieldStruct struct {
	Output string //plint:render -- rendered display text
	Raw    string // want `struct field exceptions\.RenderFieldStruct\.Raw uses primitive type string`
}
