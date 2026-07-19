// SPDX-License-Identifier: MPL-2.0

package app

import "cfa_escaping_closure_cross/lib"

func Callback() func() { return lib.Returned("constant") }
