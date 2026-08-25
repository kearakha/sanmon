package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireBearerToken(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := requireBearerToken("sk-sanmon-test", ok)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"token benar", "Bearer sk-sanmon-test", http.StatusOK},
		{"token salah", "Bearer sk-sanmon-salah", http.StatusUnauthorized},
		{"tanpa header", "", http.StatusUnauthorized},
		{"tanpa Bearer", "sk-sanmon-test", http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
