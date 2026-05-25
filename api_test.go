package main

import "testing"

func TestCacheKeyForAvoidsPathFlatteningCollisions(t *testing.T) {
	first := cacheKeyFor("a/b.jpg")
	second := cacheKeyFor("a_b.jpg")

	if first == second {
		t.Fatalf("cacheKeyFor produced colliding keys: %q", first)
	}
}

func TestCacheKeyForNormalizesSeparators(t *testing.T) {
	first := cacheKeyFor("a/b.jpg")
	second := cacheKeyFor("a\\b.jpg")

	if first != second {
		t.Fatalf("cacheKeyFor should normalize path separators: %q != %q", first, second)
	}
}
