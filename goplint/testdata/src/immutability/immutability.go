// SPDX-License-Identifier: MPL-2.0

package immutability

// Good has all unexported fields with a constructor — no diagnostic.
type Good struct {
	name string // want `struct field immutability\.Good\.name uses primitive type string`
}

func NewGood() *Good { return &Good{} }

// Bad has an exported field with a constructor — should be flagged.
type Bad struct {
	Name string // want `struct field immutability\.Bad\.Name uses primitive type string` `struct immutability\.Bad has NewBad\(\) constructor but field Name is exported`
}

func NewBad() *Bad { return &Bad{} }

// NoCtor has exported fields but no constructor — OK (DTO pattern).
type NoCtor struct {
	Name string // want `struct field immutability\.NoCtor\.Name uses primitive type string`
}

// Mixed has both exported and unexported fields with a constructor.
// Only the exported field should be flagged.
type Mixed struct {
	Name   string // want `struct field immutability\.Mixed\.Name uses primitive type string` `struct immutability\.Mixed has NewMixed\(\) constructor but field Name is exported`
	secret string // want `struct field immutability\.Mixed\.secret uses primitive type string`
}

func NewMixed() *Mixed { return &Mixed{} }

// MutableByDesign has exported fields and a constructor but is marked
// //goplint:mutable — no immutability diagnostic expected. Only the
// primitive field finding is emitted.
//
//goplint:mutable
type MutableByDesign struct {
	Name string // want `struct field immutability\.MutableByDesign\.Name uses primitive type string`
}

func NewMutableByDesign() *MutableByDesign { return &MutableByDesign{} }

// unexported is unexported — not checked by any structural mode.
type unexported struct {
	data string // want `struct field immutability\.unexported\.data uses primitive type string`
}
