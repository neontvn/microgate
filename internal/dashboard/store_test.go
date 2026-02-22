package dashboard

import (
	"strconv"
	"testing"
	"time"
)

func TestLogStoreRingBuffer(t *testing.T) {
	store := NewLogStore(3)

	// Add 1st item
	store.Add(RequestLog{ID: "1"})
	if store.count != 1 {
		t.Errorf("Expected count 1, got %d", store.count)
	}

	// Add 2nd and 3rd items
	store.Add(RequestLog{ID: "2"})
	store.Add(RequestLog{ID: "3"})

	if store.count != 3 {
		t.Errorf("Expected count 3, got %d", store.count)
	}

	recent := store.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(recent))
	}
	if recent[0].ID != "3" || recent[1].ID != "2" || recent[2].ID != "1" {
		t.Errorf("Expected order 3, 2, 1, got %s, %s, %s", recent[0].ID, recent[1].ID, recent[2].ID)
	}

	// Add 4th item (should evict 1st)
	store.Add(RequestLog{ID: "4"})

	if store.count != 3 {
		t.Errorf("Expected count to stay at 3, got %d", store.count)
	}

	recent = store.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(recent))
	}
	if recent[0].ID != "4" || recent[1].ID != "3" || recent[2].ID != "2" {
		t.Errorf("Expected order 4, 3, 2, got %s, %s, %s", recent[0].ID, recent[1].ID, recent[2].ID)
	}
}

func TestLogStoreSearchAndGet(t *testing.T) {
	store := NewLogStore(10)

	store.Add(RequestLog{ID: "1", Status: 200, Path: "/api/users", Latency: 50 * time.Millisecond})
	store.Add(RequestLog{ID: "2", Status: 500, Path: "/api/orders", Latency: 120 * time.Millisecond})
	store.Add(RequestLog{ID: "3", Status: 200, Path: "/api/users/1", Latency: 45 * time.Millisecond})
	store.Add(RequestLog{ID: "4", Status: 404, Path: "/unknown", Latency: 10 * time.Millisecond})

	// Test GetByID
	log, found := store.GetByID("2")
	if !found || log.Path != "/api/orders" {
		t.Errorf("GetByID failed: found=%v, path=%s", found, log.Path)
	}

	_, found = store.GetByID("999")
	if found {
		t.Errorf("GetByID should not find non-existent ID")
	}

	// Test Search by status
	results := store.Search(10, 200, "")
	if len(results) != 2 {
		t.Errorf("Expected 2 results with status 200, got %d", len(results))
	}

	// Test Search by path exact
	results = store.Search(10, 0, "/api/orders")
	if len(results) != 1 {
		t.Errorf("Expected 1 result with path /api/orders, got %d", len(results))
	}

	// Test Search with limit
	for i := 5; i < 15; i++ {
		store.Add(RequestLog{ID: strconv.Itoa(i), Status: 200, Path: "/spam"})
	}

	results = store.Search(5, 200, "/spam")
	if len(results) != 5 {
		t.Errorf("Expected limit 5 to be respected, got %d", len(results))
	}
}
