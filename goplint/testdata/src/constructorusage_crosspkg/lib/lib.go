// SPDX-License-Identifier: MPL-2.0

package lib

import "errors"

type Thing struct{}

func NewThing() (*Thing, error) {
	return nil, errors.New("boom")
}
