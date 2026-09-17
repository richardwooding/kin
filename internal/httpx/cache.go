package httpx

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
)

// Cache stores fetched response bodies under Dir, keyed by the whole request
// URL, so a second run or a resumed sweep never asks a service the same
// question twice. A nil cache, or one with an empty Dir, is a no-op.
type Cache struct {
	Dir string
	Ext string // file extension, ".json" by default
}

// NewCache returns a cache writing into dir; an empty dir disables caching.
func NewCache(dir string) *Cache { return &Cache{Dir: dir, Ext: ".json"} }

// Path is the file a URL's body is stored in.
func (c *Cache) Path(url string) string {
	if c == nil || c.Dir == "" {
		return ""
	}
	ext := c.Ext
	if ext == "" {
		ext = ".json"
	}
	sum := sha1.Sum([]byte(url))
	return filepath.Join(c.Dir, fmt.Sprintf("%x%s", sum[:8], ext))
}

// Get returns the stored body for a URL.
func (c *Cache) Get(url string) ([]byte, bool) {
	path := c.Path(url)
	if path == "" {
		return nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return nil, false
	}
	return b, true
}

// Put stores a body. Failures are ignored: the cache is an optimisation, and
// a run must not stop because a directory is unwritable.
func (c *Cache) Put(url string, body []byte) {
	path := c.Path(url)
	if path == "" || len(body) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, body, 0o644)
}
