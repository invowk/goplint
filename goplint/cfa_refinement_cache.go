// SPDX-License-Identifier: MPL-2.0

package goplint

type cfgRefinementCache struct {
	attempts map[string]bool
}

func newCFGRefinementCache() *cfgRefinementCache {
	return &cfgRefinementCache{attempts: make(map[string]bool)}
}

func (c *cfgRefinementCache) record(hash string) bool {
	if c == nil || hash == "" {
		return false
	}
	if c.attempts[hash] {
		return false
	}
	c.attempts[hash] = true
	return true
}
