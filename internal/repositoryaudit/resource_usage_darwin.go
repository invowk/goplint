// SPDX-License-Identifier: MPL-2.0

package repositoryaudit

import "syscall"

func peakRSSBytes(systemUsage any) int64 {
	usage, ok := systemUsage.(*syscall.Rusage)
	if !ok || usage == nil {
		return 0
	}
	return usage.Maxrss
}
