// SPDX-License-Identifier: MPL-2.0

package goplint

import "sync"

func loadCacheEntry[K comparable, V any](cache *sync.Map, key K, load func() V) V {
	if cache == nil {
		return load()
	}
	if cached, ok := cache.Load(key); ok {
		return cached.(V)
	}

	entry := load()
	actual, _ := cache.LoadOrStore(key, entry)
	return actual.(V)
}
