// SPDX-License-Identifier: MPL-2.0

//go:build !linux

package soundnessgate

import "errors"

func availableMemoryBytes() (int64, error) {
	return 0, errors.New("portable available-memory discovery is unavailable")
}
