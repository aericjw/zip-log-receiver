package checkpoint

import (
	"context"
	"sync"
)

// Cached is a Persister that remembers values in memory and writes through to an underlying
// Persister. The stanza adapter hands operators a persister that forgets everything when no
// storage extension is configured; Cached keeps progress for the process lifetime in that case
// and still persists across restarts when a real storage extension sits underneath.
type Cached struct {
	under Persister
	mu    sync.RWMutex
	mem   map[string][]byte
}

// NewCached wraps under.
func NewCached(under Persister) *Cached {
	return &Cached{under: under, mem: map[string][]byte{}}
}

// Get returns the in-memory value if present, else asks the underlying persister.
func (c *Cached) Get(ctx context.Context, key string) ([]byte, error) {
	c.mu.RLock()
	v, ok := c.mem[key]
	c.mu.RUnlock()
	if ok {
		return v, nil
	}
	return c.under.Get(ctx, key)
}

// Set writes through to the underlying persister and only remembers the value if that succeeded.
func (c *Cached) Set(ctx context.Context, key string, value []byte) error {
	if err := c.under.Set(ctx, key, value); err != nil {
		return err
	}
	c.mu.Lock()
	c.mem[key] = value
	c.mu.Unlock()
	return nil
}
