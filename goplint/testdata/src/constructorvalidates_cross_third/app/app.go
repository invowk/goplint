// SPDX-License-Identifier: MPL-2.0

package app

import (
	"constructorvalidates_cross_third/model"
	"constructorvalidates_cross_third/util"
)

// NewServer delegates validation to util.ValidateServer, whose extracted
// summary targets model.Server by package-qualified slot identity.
func NewServer(addr string) (*model.Server, error) { // want `parameter "addr" of app\.NewServer uses primitive type string`
	s := &model.Server{Addr: addr}
	return s, util.ValidateServer(s)
}
