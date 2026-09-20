package main

import (
	"sync"
	"time"
)

type CacheItem struct {
	Value     interface{}
	ExpiresAt time.Time
}

// defaultCacheMaxSize 限制内存缓存条目数上限，防止用户可控的查询参数
// （city/start/end 拼接进 cacheKey）被无限枚举导致 map 无限增长撑爆内存。
const defaultCacheMaxSize = 500

type Cache struct {
	items   map[string]CacheItem
	mu      sync.RWMutex
	ttl     time.Duration
	maxSize int
	fifo    []string
}

func NewCache(ttl time.Duration) *Cache {
	c := &Cache{
		items:   make(map[string]CacheItem),
		ttl:     ttl,
		maxSize: defaultCacheMaxSize,
	}
	go c.cleanup()
	return c
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.items[key]
	if !exists || time.Now().After(item.ExpiresAt) {
		return nil, false
	}
	return item.Value, true
}

func (c *Cache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = CacheItem{
		Value:     value,
		ExpiresAt: time.Now().Add(c.ttl),
	}
	c.fifo = append(c.fifo, key)

	// 超过容量上限时按 FIFO 淘汰最旧条目，将内存占用收敛到 maxSize 之内。
	if c.maxSize > 0 {
		for len(c.items) > c.maxSize && len(c.fifo) > 0 {
			old := c.fifo[0]
			c.fifo = c.fifo[1:]
			delete(c.items, old)
		}
	}
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]CacheItem)
	c.fifo = nil
}

func (c *Cache) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, item := range c.items {
			if now.After(item.ExpiresAt) {
				delete(c.items, key)
			}
		}
		c.mu.Unlock()
	}
}
