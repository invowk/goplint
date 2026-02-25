// SPDX-License-Identifier: MPL-2.0

package interfaces

// Service is an interface — its methods should NOT be flagged.
// Interface methods appear as *ast.Field in InterfaceType, not as
// *ast.FuncDecl, so they are naturally excluded.
type Service interface {
	Execute(name string) error
	GetName() string
}

// Implementor implements Service. Its concrete methods ARE checked.
type Implementor struct{}

func (i Implementor) Execute(name string) error { // want `parameter "name" of interfaces\.Implementor\.Execute uses primitive type string`
	_ = name
	return nil
}

func (i Implementor) GetName() string { // want `return value of interfaces\.Implementor\.GetName uses primitive type string`
	return ""
}
