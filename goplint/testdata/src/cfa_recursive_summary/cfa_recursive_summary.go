// SPDX-License-Identifier: MPL-2.0

package cfa_recursive_summary

import "fmt"

type CommandName string

func (name CommandName) Validate() error {
	if name == "" {
		return fmt.Errorf("empty command name")
	}
	return nil
}

func recursiveValidate(name CommandName, depth int) error { // want `parameter "depth" of cfa_recursive_summary\.recursiveValidate uses primitive type int`
	if depth <= 0 {
		return name.Validate()
	}
	return recursiveValidate(name, depth-1)
}

func recursivePreserve(name CommandName, depth int) error { // want `parameter "depth" of cfa_recursive_summary\.recursivePreserve uses primitive type int`
	if depth <= 0 {
		return nil
	}
	return recursivePreserve(name, depth-1)
}

func mutualPreserveA(name CommandName, depth int) error { // want `parameter "depth" of cfa_recursive_summary\.mutualPreserveA uses primitive type int`
	if depth <= 0 {
		return nil
	}
	return mutualPreserveB(name, depth-1)
}

func mutualPreserveB(name CommandName, depth int) error { // want `parameter "depth" of cfa_recursive_summary\.mutualPreserveB uses primitive type int`
	if depth <= 0 {
		return nil
	}
	return mutualPreserveA(name, depth-1)
}

func RecursiveValidationSafe(raw string, depth int) error { // want `parameter "raw" of cfa_recursive_summary\.RecursiveValidationSafe uses primitive type string` `parameter "depth" of cfa_recursive_summary\.RecursiveValidationSafe uses primitive type int`
	name := CommandName(raw)
	if err := recursiveValidate(name, depth); err != nil {
		return err
	}
	return nil
}

func RecursivePreserveUnsafe(raw string, depth int) error { // want `parameter "raw" of cfa_recursive_summary\.RecursivePreserveUnsafe uses primitive type string` `parameter "depth" of cfa_recursive_summary\.RecursivePreserveUnsafe uses primitive type int`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	return recursivePreserve(name, depth)
}

func MutualRecursivePreserveUnsafe(raw string, depth int) error { // want `parameter "raw" of cfa_recursive_summary\.MutualRecursivePreserveUnsafe uses primitive type string` `parameter "depth" of cfa_recursive_summary\.MutualRecursivePreserveUnsafe uses primitive type int`
	name := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	return mutualPreserveA(name, depth)
}
