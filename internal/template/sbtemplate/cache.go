package sbtemplate

import "sync"

// Cache stores parsed templates by an opaque string key.
// It is safe for concurrent use. The cache grows without bound;
// template counts in practice are small (1–hundreds), so no eviction
// is needed.
type Cache struct {
	mu    sync.RWMutex
	items map[string]*cacheEntry
}

type cacheEntry struct {
	parsed *Template
	err    error
}

// NewCache returns an empty Cache.
func NewCache() *Cache {
	return &Cache{items: map[string]*cacheEntry{}}
}

// Get returns the cached result for key. On a miss it calls Parse(src, cb),
// stores the result (including errors), and returns it. Errors are cached so
// a bad template body does not re-parse on every request.
func (c *Cache) Get(key, src string, cb ParseCallback) (*Template, error) {
	c.mu.RLock()
	if e, ok := c.items[key]; ok {
		c.mu.RUnlock()
		return e.parsed, e.err
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	// double-check under write lock
	if e, ok := c.items[key]; ok {
		return e.parsed, e.err
	}
	parsed, err := Parse(src, cb)
	c.items[key] = &cacheEntry{parsed: parsed, err: err}
	return parsed, err
}
