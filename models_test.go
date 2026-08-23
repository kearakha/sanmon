package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelsHandler_Passthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("upstream nerima method %s, want GET", r.Method)
		}
		if r.URL.Path != "/models" {
			t.Errorf("upstream nerima path %s, want /models", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":[{"id":"gemini-3.6-flash"}]}`)
	}))
	defer upstream.Close()
	pointUpstream(t, upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	newModelsHandler(http.DefaultClient, "test-key").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	want := `{"data":[{"id":"gemini-3.6-flash"}]}`
	if rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}
