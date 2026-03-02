// SPDX-License-Identifier: MPL-2.0

package constructorusage_suppressed

import "errors"

type Thing struct{}

func NewThing() (*Thing, error) {
	return nil, errors.New("boom")
}

func Excepted() {
	v, _ := NewThing()
	_ = v
}

func Baselined() {
	v, _ := NewThing()
	_ = v
}

func Reported() {
	v, _ := NewThing() // want `constructor NewThing error return assigned to blank identifier`
	_ = v
}
