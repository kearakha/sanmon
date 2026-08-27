package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequireAdminQueryToken(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := requireAdminQueryToken("admin-test-token", ok)

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"token benar", "?token=admin-test-token", http.StatusOK},
		{"token salah", "?token=salah", http.StatusUnauthorized},
		{"tanpa token", "", http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/stream"+tc.query, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestStreamHandler_ClientDisconnectCleansUpSubscriber(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/admin/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		newStreamHandler(hub).ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler nggak balik setelah klien putus")
	}
}

func TestStreamHandler_ForwardsBroadcastEvent(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/admin/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		newStreamHandler(hub).ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond) // biar subscriber sempet register ke hub
	hub.Broadcast(Event{Model: "gemini-flash", StatusCode: 200})
	time.Sleep(50 * time.Millisecond) // biar event sempet ke-flush ke rec.Body
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler nggak balik setelah klien putus")
	}

	// dibaca setelah <-done biar nggak race sama tulisan handler ke rec.Body
	if !strings.Contains(rec.Body.String(), "gemini-flash") {
		t.Errorf("body nggak ngandung event yang di-broadcast: %q", rec.Body.String())
	}
}
