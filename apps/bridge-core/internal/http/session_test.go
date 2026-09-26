package http

import (
	"fmt"
	"sync"
	"testing"
)

// TestSessionStoreBoundedByCap pins the new upper-bound on sessionStore:
// once len(s.browser) hits sessionStoreCap, one existing entry is evicted
// per insert so the map cannot grow without bound under MCP clients that
// mint fresh sessions in tight loops.
func TestSessionStoreBoundedByCap(t *testing.T) {
	store := newSessionStore()

	// Fill to exactly the cap; each insert is a fresh session id so the
	// "exists" branch in set is never taken.
	for i := 0; i < sessionStoreCap; i++ {
		store.set(sessionIDFor(i), "browser-a")
	}
	if got := len(store.browser); got != sessionStoreCap {
		t.Fatalf("len = %d, want %d", got, sessionStoreCap)
	}

	// One more insert must trigger eviction; total stays at the cap.
	store.set("overflow", "browser-b")
	if got := len(store.browser); got != sessionStoreCap {
		t.Fatalf("after overflow len = %d, want %d (cap)", got, sessionStoreCap)
	}

	// Re-setting an existing key never evicts, even at the cap.
	store.set("overflow", "browser-c")
	if got := store.get("overflow"); got != "browser-c" {
		t.Fatalf("re-set value = %q, want %q", got, "browser-c")
	}
	if got := len(store.browser); got != sessionStoreCap {
		t.Fatalf("after re-set len = %d, want %d", got, sessionStoreCap)
	}
}

// TestSessionStoreConcurrentSetDoesNotGrow is a sanity check that the
// eviction branch does not race with concurrent inserts: every set lands and
// the cap holds under -race.
func TestSessionStoreConcurrentSetDoesNotGrow(t *testing.T) {
	store := newSessionStore()
	const writers = 8
	const perWriter = sessionStoreCap * 2
	var wg sync.WaitGroup
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				store.set(sessionIDFor(w*perWriter+i), "browser")
			}
		}()
	}
	wg.Wait()
	if got := len(store.browser); got > sessionStoreCap {
		t.Fatalf("len = %d, want <= cap %d", got, sessionStoreCap)
	}
}

func sessionIDFor(i int) string { return fmt.Sprintf("session-%d", i) }
