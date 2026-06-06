package cache

import (
	"testing"
	"time"
)

func TestCache_SetAndGet(t *testing.T) {
	c := New(100, 10*time.Second)

	c.Set("key1", "value1")

	v, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if v != "value1" {
		t.Fatalf("expected 'value1', got %v", v)
	}
}

func TestCache_Miss(t *testing.T) {
	c := New(100, 10*time.Second)

	_, ok := c.Get("nonexistent")
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestCache_Expiry(t *testing.T) {
	c := New(100, 50*time.Millisecond)

	c.Set("key1", "value1")
	time.Sleep(100 * time.Millisecond)

	_, ok := c.Get("key1")
	if ok {
		t.Fatal("expected cache miss after expiry")
	}
}

func TestCache_Eviction(t *testing.T) {
	c := New(2, 10*time.Second)

	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3) // should evict oldest

	if c.Len() > 2 {
		t.Fatalf("expected at most 2 entries, got %d", c.Len())
	}
}

func TestCache_Delete(t *testing.T) {
	c := New(100, 10*time.Second)
	c.Set("key1", "value1")
	c.Delete("key1")

	_, ok := c.Get("key1")
	if ok {
		t.Fatal("expected cache miss after delete")
	}
}

func TestCache_Stats(t *testing.T) {
	c := New(100, 10*time.Second)

	c.Set("a", 1)
	c.Get("a") // hit
	c.Get("b") // miss

	hits, misses := c.Stats()
	if hits != 1 {
		t.Fatalf("expected 1 hit, got %d", hits)
	}
	if misses != 1 {
		t.Fatalf("expected 1 miss, got %d", misses)
	}
}
