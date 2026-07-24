// SPDX-License-Identifier: MPL-2.0

//go:build !linux && !darwin

package repositoryaudit

func peakRSSBytes(any) int64 { return 0 }
