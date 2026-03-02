// SPDX-License-Identifier: MPL-2.0

package interfaces

import "fmt"

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

type ContractImplementor struct{}

func (ContractImplementor) Write(p []byte) (int, error) {
	return len(p), nil
}

func (ContractImplementor) WriteString(s string) (int, error) {
	return len(s), nil
}

func (ContractImplementor) ReadRune() (rune, int, error) {
	return 'x', 1, nil
}

func (ContractImplementor) Format(_ fmt.State, _ rune) {}

type ContractNearMiss struct{}

func (ContractNearMiss) Write(s string) (int, error) { // want `parameter "s" of interfaces\.ContractNearMiss\.Write uses primitive type string` `return value of interfaces\.ContractNearMiss\.Write uses primitive type int`
	return len(s), nil
}
