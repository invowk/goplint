// SPDX-License-Identifier: MPL-2.0

package goplint

import "sync"

func loadCacheEntry[K comparable, V any](cache *sync.Map, key K, load func() V) V {
	if cache == nil {
		return load()
	}
	if cached, ok := cache.Load(key); ok {
		if typed, ok := cached.(V); ok {
			return typed
		}
	}

	entry := load()
	actual, _ := cache.LoadOrStore(key, entry)
	if typed, ok := actual.(V); ok {
		return typed
	}
	return entry
}
