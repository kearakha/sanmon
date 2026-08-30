package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallChatCompletion_ParsesContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"# match.md\n\nStrong fit."}}]}`))
	}))
	defer server.Close()

	got, err := callChatCompletion(server.URL, "test-key", "gemini-flash", "prompt apapun")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "Strong fit.") {
		t.Errorf("content = %q, want to contain %q", got, "Strong fit.")
	}
}

func TestCallChatCompletion_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	_, err := callChatCompletion(server.URL, "salah", "gemini-flash", "prompt apapun")
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}
