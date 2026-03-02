// SPDX-License-Identifier: MPL-2.0

package constructorreturn_error_alias

import "fmt"

type Name string

type Err = error

func (n Name) Validate() error {
	if n == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func NewName(raw string) (Name, Err) { // want `parameter "raw" of constructorreturn_error_alias\.NewName uses primitive type string`
	n := Name(raw)
	if err := n.Validate(); err != nil {
		return "", err
	}
	return n, nil
}
