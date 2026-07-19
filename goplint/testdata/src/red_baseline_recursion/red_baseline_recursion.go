// SPDX-License-Identifier: MPL-2.0

package red_baseline_recursion

type Name string

func (name Name) Validate() error { return nil }

func preserve(name Name, depth int) error { // want `parameter "depth" of red_baseline_recursion\.preserve uses primitive type int`
	if depth == 0 {
		return nil
	}
	return preserve(name, depth-1)
}

func RecursiveUnsafe(raw string, depth int) error { // want `parameter "raw" of red_baseline_recursion\.RecursiveUnsafe uses primitive type string` `parameter "depth" of red_baseline_recursion\.RecursiveUnsafe uses primitive type int`
	name := Name(raw) // want `type conversion to Name from non-constant without Validate\(\) check`
	return preserve(name, depth)
}
