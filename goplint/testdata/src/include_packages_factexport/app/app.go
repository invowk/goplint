// SPDX-License-Identifier: MPL-2.0

package app

import "include_packages_factexport/util"

func NewWithDirective(addr string) (*util.Server, error) { // want `parameter "addr" of app\.NewWithDirective uses primitive type string`
	s := &util.Server{Addr: addr}
	return s, util.ValidateServer(s)
}

func NewNoDirective(addr string) (*util.Server, error) { // want `parameter "addr" of app\.NewNoDirective uses primitive type string` `constructor app\.NewNoDirective returns util\.Server which has Validate\(\) but never calls it`
	s := &util.Server{Addr: addr}
	return s, util.HelperNoDirective(s)
}
