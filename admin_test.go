package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func seedRequest(t *testing.T, db *sql.DB, model string, status int) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(
		`INSERT INTO requests (model_requested, provider, stream, status_code, tokens_in, tokens_out, cost_micro_usd)
		 VALUES ($1, 'gemini', false, $2, 10, 20, 5) RETURNING id`, model, status).Scan(&id)
	if err != nil {
		t.Fatalf("seed request: %v", err)
	}
	return id
}

func TestRequestsListHandler(t *testing.T) {
	db := openTestDB(t)

	marker := "admin-req-test"
	id1 := seedRequest(t, db, marker, 200)
	id2 := seedRequest(t, db, marker, 200)
	id3 := seedRequest(t, db, marker, 500)
	t.Cleanup(func() { db.Exec(`DELETE FROM requests WHERE model_requested = $1`, marker) })

	handler := newRequestsListHandler(db)
	get := func(query string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/admin/requests?"+query, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	t.Run("filter model, urut terbaru dulu", func(t *testing.T) {
		reqs := get("model=" + marker)["requests"].([]any)
		if len(reqs) != 3 {
			t.Fatalf("len = %d, want 3", len(reqs))
		}
		if got := int64(reqs[0].(map[string]any)["id"].(float64)); got != id3 {
			t.Errorf("baris pertama id = %d, want %d (terbaru dulu)", got, id3)
		}
	})

	t.Run("filter status", func(t *testing.T) {
		if n := len(get("model=" + marker + "&status=500")["requests"].([]any)); n != 1 {
			t.Errorf("status=500 → %d baris, want 1", n)
		}
	})

	t.Run("pagination keyset", func(t *testing.T) {
		out := get("model=" + marker + "&limit=2")
		if got := int64(out["next_before_id"].(float64)); got != id2 {
			t.Fatalf("next_before_id = %d, want %d", got, id2)
		}
		out = get("model=" + marker + "&limit=2&before_id=" + strconv.FormatInt(id2, 10))
		reqs := out["requests"].([]any)
		if len(reqs) != 1 || int64(reqs[0].(map[string]any)["id"].(float64)) != id1 {
			t.Errorf("halaman 2 = %v, want cuma id %d", reqs, id1)
		}
		if out["next_before_id"] != nil {
			t.Errorf("next_before_id halaman terakhir = %v, want null", out["next_before_id"])
		}
	})
}

func TestRequestHandler(t *testing.T) {
	db := openTestDB(t)
	marker := "admin-detail-test"
	id := seedRequest(t, db, marker, 200)
	t.Cleanup(func() { db.Exec(`DELETE FROM requests WHERE model_requested = $1`, marker) })

	handler := newRequestHandler(db)

	t.Run("ketemu", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/requests/x", nil)
		req.SetPathValue("id", strconv.FormatInt(id, 10))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var rr RequestRow
		if err := json.Unmarshal(rec.Body.Bytes(), &rr); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if rr.ID != id || rr.ModelRequested != marker {
			t.Errorf("row = %+v, want id %d model %s", rr, id, marker)
		}
	})

	t.Run("id nggak ada → 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/requests/x", nil)
		req.SetPathValue("id", "999999999")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("id bukan angka → 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/requests/x", nil)
		req.SetPathValue("id", "abc")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}
