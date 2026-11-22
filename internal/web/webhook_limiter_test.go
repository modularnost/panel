package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"modularnost/internal/store"
)

func TestWebhookRequestsAreLimitedByClientIP(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{DB: db}
	s.hookRequests.take("unused", time.Now().Add(time.Minute))
	handler := s.Routes()
	post := func(peer string, want int) {
		t.Helper()
		req := httptest.NewRequest("POST", "/hook/unknown", nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", "192.0.2.10")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("webhook status %d, want %d", rec.Code, want)
		}
		if want == http.StatusTooManyRequests && rec.Header().Get("Retry-After") == "" {
			t.Fatal("missing Retry-After")
		}
	}
	for range webhookRequestsPerMinute {
		post("192.0.2.20:1234", http.StatusNotFound)
	}
	post("192.0.2.20:4321", http.StatusTooManyRequests)
	post("192.0.2.30:1234", http.StatusNotFound)
}
