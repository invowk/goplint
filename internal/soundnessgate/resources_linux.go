// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"fmt"
	"os"
)

func availableMemoryBytes() (int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read available linux memory: %w", err)
	}
	return parseMemAvailable(data)
}
