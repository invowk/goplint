// SPDX-License-Identifier: MPL-2.0

package util

import "constructorvalidates_cross_third/model"

//goplint:validates-type=Server
func ValidateServer(s *model.Server) error { // want ValidateServer:"validates-type\\(Server\\)"
	return s.Validate()
}
