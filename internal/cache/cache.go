// Package cache provides an in-memory query result cache with TTL and size limits.
package cache

import (
	"sync"
	"time"
)

// entry is a single cached item.
type entry struct {
	value   any
	expires time.Time
}

// Cache is a thread-safe in-memory cache with per-key TTL and max capacity.
type Cache struct {
	mu       sync.RWMutex
	items    map[string]*entry
	maxSize  int
	defaultTTL time.Duration

	hits   uint64
	misses uint64
}

// New creates a new Cache with the given max capacity and default TTL.
func New(maxSize int, defaultTTL time.Duration) *Cache {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if defaultTTL <= 0 {
		defaultTTL = 5 * time.Minute
	}
	c := &Cache{
		items:    make(map[string]*entry, maxSize),
		maxSize:  maxSize,
		defaultTTL: defaultTTL,
	}
	go c.reapLoop()
	return c
}

// Get retrieves a cached value. Returns (nil, false) on miss.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()

	if !ok || time.Now().After(e.expires) {
		c.mu.Lock()
		if e, ok = c.items[key]; ok && time.Now().After(e.expires) {
			delete(c.items, key)
		}
		c.misses++
		c.mu.Unlock()
		return nil, false
	}

	c.hits++
	return e.value, true
}

// Set stores a value with the default TTL. Evicts oldest entries if at capacity.
func (c *Cache) Set(key string, value any) {
	c.SetTTL(key, value, c.defaultTTL)
}

// SetTTL stores a value with a custom TTL.
func (c *Cache) SetTTL(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity.
	if len(c.items) >= c.maxSize {
		c.evictOne()
	}

	c.items[key] = &entry{
		value:   value,
		expires: time.Now().Add(ttl),
	}
}

// Delete removes a key from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

// Stats returns hit/miss counts.
func (c *Cache) Stats() (hits, misses uint64) {
	return c.hits, c.misses
}

// Len returns the current number of cached items.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// evictOne removes the oldest (nearest expiry) entry. Must hold c.mu.
func (c *Cache) evictOne() {
	var oldestKey string
	var oldest time.Time
	i := 0
	for k, e := range c.items {
		if i == 0 || e.expires.Before(oldest) {
			oldestKey = k
			oldest = e.expires
		}
		i++
	}
	if oldestKey != "" {
		delete(c.items, oldestKey)
	}
}

// reapLoop periodically cleans expired entries.
func (c *Cache) reapLoop() {
	ticker := time.NewTicker(c.defaultTTL / 2)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, e := range c.items {
			if now.After(e.expires) {
				delete(c.items, k)
			}
		}
		c.mu.Unlock()
	}
}
