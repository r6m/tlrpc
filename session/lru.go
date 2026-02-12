package session

import "container/list"

// LRUCache is a small LRU cache for message IDs.
type LRUCache struct {
	capacity int
	entries  map[int64]*list.Element
	order    *list.List
}

type lruEntry struct {
	key int64
}

// NewLRUCache creates a new LRU cache.
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &LRUCache{
		capacity: capacity,
		entries:  make(map[int64]*list.Element),
		order:    list.New(),
	}
}

// Add inserts a key and evicts the oldest if needed.
func (c *LRUCache) Add(key int64) {
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(lruEntry{key: key})
	c.entries[key] = el
	if c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest == nil {
			return
		}
		c.order.Remove(oldest)
		entry := oldest.Value.(lruEntry)
		delete(c.entries, entry.key)
	}
}

// Contains returns true if the key exists and updates its recency.
func (c *LRUCache) Contains(key int64) bool {
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		return true
	}
	return false
}
