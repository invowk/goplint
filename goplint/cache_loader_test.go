// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"sync"
	"testing"
)

func TestLoadCacheEntry(t *testing.T) {
	t.Parallel()

	t.Run("uses cached value after first load", func(t *testing.T) {
		t.Parallel()

		var cache sync.Map
		loadCalls := 0
		first := loadCacheEntry(&cache, "key", func() int {
			loadCalls++
			return 1
		})
		second := loadCacheEntry(&cache, "key", func() int {
			loadCalls++
			return 2
		})

		if first != 1 || second != 1 {
			t.Fatalf("expected cached value 1, got first=%d second=%d", first, second)
		}
		if loadCalls != 1 {
			t.Fatalf("expected loader to be called once, got %d", loadCalls)
		}
	})

	t.Run("nil cache invokes loader directly", func(t *testing.T) {
		t.Parallel()

		loadCalls := 0
		_ = loadCacheEntry[string, int](nil, "key", func() int {
			loadCalls++
			return 1
		})
		_ = loadCacheEntry[string, int](nil, "key", func() int {
			loadCalls++
			return 2
		})
		if loadCalls != 2 {
			t.Fatalf("expected loader to be called twice with nil cache, got %d", loadCalls)
		}
	})
}
