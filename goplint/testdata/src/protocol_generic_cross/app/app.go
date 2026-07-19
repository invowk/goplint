// SPDX-License-Identifier: MPL-2.0

package app

import "protocol_generic_cross/util"

func Use(raw string) error { // want `parameter "raw" of app\.Use uses primitive type string`
	value, err := util.NewValue[string](raw)
	if err != nil {
		return err
	}
	return util.ValidateValue[string](value)
}
