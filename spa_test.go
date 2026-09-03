package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSPAHandler: aset asli dilayani apa adanya, path yang bukan file mana
// pun balik index.html (catch-all §8) — refresh di halaman dalam nggak 404.
func TestSPAHandler(t *testing.T) {
	h := newSPAHandler()

	cases := []struct {
		path        string
		wantStatus  int
		wantCT      string
		wantBodySub string
	}{
		{"/", http.StatusOK, "text/html", `id="root"`},
		{"/favicon.svg", http.StatusOK, "image/svg+xml", ""},
		{"/riwayat", http.StatusOK, "text/html", `id="root"`}, // fallback SPA
	}

	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", c.path, nil))

		if rec.Code != c.wantStatus {
			t.Errorf("%s: status = %d, want %d", c.path, rec.Code, c.wantStatus)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, c.wantCT) {
			t.Errorf("%s: Content-Type = %q, want prefix %q", c.path, ct, c.wantCT)
		}
		if c.wantBodySub != "" && !strings.Contains(rec.Body.String(), c.wantBodySub) {
			t.Errorf("%s: body nggak ada %q", c.path, c.wantBodySub)
		}
	}
}
