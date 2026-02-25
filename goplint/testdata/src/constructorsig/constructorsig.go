// SPDX-License-Identifier: MPL-2.0

package constructorsig

import "fmt"

// GoodReturn has a constructor returning the correct type — no diagnostic.
type GoodReturn struct{ name string } // want `struct field constructorsig\.GoodReturn\.name uses primitive type string`

func NewGoodReturn() *GoodReturn { return &GoodReturn{} }

// MultiReturn uses (*Type, error) pattern — correct return type.
type MultiReturn struct{ data string } // want `struct field constructorsig\.MultiReturn\.data uses primitive type string`

func NewMultiReturn() (*MultiReturn, error) { return &MultiReturn{}, nil }

// WrongReturn has a constructor that returns the wrong struct type.
type WrongReturn struct{ addr string } // want `struct field constructorsig\.WrongReturn\.addr uses primitive type string`

func NewWrongReturn() *GoodReturn { return nil } // want `constructor NewWrongReturn\(\) for constructorsig\.WrongReturn returns GoodReturn, expected WrongReturn`

// StringReturn has a constructor that returns a bare string, not the struct.
type StringReturn struct{ x string } // want `struct field constructorsig\.StringReturn\.x uses primitive type string`

func NewStringReturn() string { return "" } // want `return value of constructorsig\.NewStringReturn uses primitive type string` `constructor NewStringReturn\(\) for constructorsig\.StringReturn returns string, expected StringReturn`

// NoReturn has a constructor with no return type.
type NoReturn struct{ y string } // want `struct field constructorsig\.NoReturn\.y uses primitive type string`

func NewNoReturn() {} // want `constructor NewNoReturn\(\) for constructorsig\.NoReturn has no return type`

// MissingCtor has no constructor — not checked by constructor-sig.
type MissingCtor struct{ z string } // want `struct field constructorsig\.MissingCtor\.z uses primitive type string`

// InterfaceReturn has a constructor that returns an interface — not flagged.
// Factory functions returning interfaces are a valid Go pattern.
type InterfaceReturn struct{ w string } // want `struct field constructorsig\.InterfaceReturn\.w uses primitive type string`

func NewInterfaceReturn() fmt.Stringer { return nil }
