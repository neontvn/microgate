package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tanmay/gateway/internal/dashboard"
)

func TestCaptureMiddleware(t *testing.T) {
	store := dashboard.NewLogStore(10)
	captureMiddleware := Capture(store)

	handler := captureMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("Hello Dashboard"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/test/path", nil)
	req.RemoteAddr = "192.168.1.1:54321"

	ctx := ContextWithRequestID(req.Context(), "req-1234")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()

	// Simulate request
	handler.ServeHTTP(rr, req)

	// Since the Capture middleware uses a goroutine channel, we need a slight delay
	// to allow the worker to pick it up in tests.
	time.Sleep(10 * time.Millisecond)

	logs := store.Recent(1)
	if len(logs) == 0 {
		t.Fatalf("Expected 1 log in store, got 0")
	}

	log := logs[0]
	if log.ID != "req-1234" {
		t.Errorf("Expected Request ID req-1234, got %s", log.ID)
	}
	if log.Status != http.StatusCreated {
		t.Errorf("Expected Status 201, got %d", log.Status)
	}
	if log.Path != "/test/path" {
		t.Errorf("Expected Path /test/path, got %s", log.Path)
	}
	if log.ClientIP != "192.168.1.1" {
		t.Errorf("Expected ClientIP 192.168.1.1, got %s", log.ClientIP)
	}
	if log.Method != http.MethodPost {
		t.Errorf("Expected Method POST, got %s", log.Method)
	}

	expectedBytes := int64(len("Hello Dashboard"))
	if log.BytesOut != expectedBytes {
		t.Errorf("Expected BytesOut %d, got %d", expectedBytes, log.BytesOut)
	}
}
