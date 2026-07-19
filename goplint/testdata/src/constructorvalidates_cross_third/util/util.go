// SPDX-License-Identifier: MPL-2.0

package util

import "constructorvalidates_cross_third/model"

func ValidateServer(s *model.Server) error { // want ValidateServer:"protocol-summary:v5:constructorvalidates_cross_third/util:constructorvalidates_cross_third/util.ValidateServer:1"
	return s.Validate()
}
