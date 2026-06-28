package service

import (
	"sync"
	"time"
)

type ttlCacheEntry[T any] struct {
	value     T
	expiresAt time.Time
}

type ttlCache[T any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]ttlCacheEntry[T]
}

func newTTLCache[T any](ttl time.Duration) *ttlCache[T] {
	return &ttlCache[T]{
		ttl:     ttl,
		entries: make(map[string]ttlCacheEntry[T]),
	}
}

func (c *ttlCache[T]) Get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero T
	entry, ok := c.entries[key]
	if !ok {
		return zero, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return zero, false
	}
	return entry.value, true
}

func (c *ttlCache[T]) Set(key string, value T) {
	c.SetWithTTL(key, value, c.ttl)
}

func (c *ttlCache[T]) SetWithTTL(key string, value T, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ttl <= 0 {
		ttl = c.ttl
	}
	c.entries[key] = ttlCacheEntry[T]{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
}
