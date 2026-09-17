package httpx

import (
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	c := NewCache(t.TempDir())
	const u = "https://www.thegazette.co.uk/all-notices/notice/data.json?text=Knuckey"
	if _, ok := c.Get(u); ok {
		t.Fatal("an empty cache has nothing")
	}
	c.Put(u, []byte(`{"f:total":3}`))
	b, ok := c.Get(u)
	if !ok || string(b) != `{"f:total":3}` {
		t.Fatalf("Get after Put = %q, %v", b, ok)
	}
	if _, ok := c.Get(u + "&results-page=2"); ok {
		t.Error("a different URL is a different entry")
	}
	if filepath.Ext(c.Path(u)) != ".json" {
		t.Errorf("Path = %q", c.Path(u))
	}
}

func TestCacheDisabled(t *testing.T) {
	for _, c := range []*Cache{nil, NewCache("")} {
		c.Put("https://example.org/x", []byte("body"))
		if _, ok := c.Get("https://example.org/x"); ok {
			t.Error("a disabled cache stores nothing")
		}
		if c.Path("https://example.org/x") != "" {
			t.Error("a disabled cache has no path")
		}
	}
}
