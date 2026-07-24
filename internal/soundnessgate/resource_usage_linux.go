// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import "syscall"

func peakRSSBytes(systemUsage any) int64 {
	usage, ok := systemUsage.(*syscall.Rusage)
	if !ok || usage.Maxrss < 0 {
		return 0
	}
	return usage.Maxrss * 1024
}
