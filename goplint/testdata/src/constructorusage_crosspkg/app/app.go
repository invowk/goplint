// SPDX-License-Identifier: MPL-2.0

package app

import "constructorusage_crosspkg/lib"

func BlankedCrossPkg() {
	v, _ := lib.NewThing() // want `constructor NewThing error return assigned to blank identifier`
	_ = v
}

func CapturedCrossPkg() {
	v, err := lib.NewThing()
	if err != nil {
		return
	}
	_ = v
}
