package sbtemplate

import (
	"sync"
	"testing"
)

func TestCacheReturnsSamePointer(t *testing.T) {
	c := NewCache()
	src := "hello {who}\n"

	a, err := c.Get("k1", src, NoCallback)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	b, err := c.Get("k1", src, NoCallback)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if a != b {
		t.Error("expected same *Template pointer on cache hit")
	}
}

func TestCacheDifferentKeysAreDifferentParsed(t *testing.T) {
	c := NewCache()
	a, _ := c.Get("k1", "hello\n", NoCallback)
	b, _ := c.Get("k2", "world\n", NoCallback)
	if a == b {
		t.Error("different keys must not return the same pointer")
	}
}

func TestCacheSameKeyIgnoresDifferentSrc(t *testing.T) {
	// Once a key is cached the src argument is ignored on subsequent calls.
	// Callers must change the key when the source changes.
	c := NewCache()
	first, _ := c.Get("k1", "first\n", NoCallback)
	second, _ := c.Get("k1", "second — should be ignored\n", NoCallback)
	if first != second {
		t.Error("same key must return cached pointer even when src differs")
	}
	rendered := first.New().Render()
	if rendered != "first\n" {
		t.Errorf("cached value should be the first parse result, got %q", rendered)
	}
}

func TestCacheConcurrentAccess(t *testing.T) {
	c := NewCache()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Get("shared", "{x}\n", NoCallback) //nolint:errcheck
		}()
	}
	wg.Wait()
}

func TestCacheStoresParseError(t *testing.T) {
	c := NewCache()
	// An unexpected END triggers a parse error.
	badSrc := "<!-- END -->\n"
	_, err1 := c.Get("bad", badSrc, NoCallback)
	if err1 == nil {
		t.Fatal("expected error for bad template")
	}
	_, err2 := c.Get("bad", badSrc, NoCallback)
	if err1 != err2 { //nolint:errorlint // checking that the cache returns the same error instance, not a wrapped one.
		t.Error("expected same error object to be returned from cache")
	}
}
