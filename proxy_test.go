package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pointUpstream ngarahin geminiBaseURL ke server test, terus balikin ke aslinya
// setelah test kelar.
func pointUpstream(t *testing.T, url string) {
	t.Helper()
	orig := geminiBaseURL
	geminiBaseURL = url
	t.Cleanup(func() { geminiBaseURL = orig })
}

func TestChatCompletionsHandler_StreamingPassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, chunk := range []string{"data: a\n\n", "data: b\n\n", "data: [DONE]\n\n"} {
			fmt.Fprint(w, chunk)
			flusher.Flush()
		}
	}))
	defer upstream.Close()
	pointUpstream(t, upstream.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	rec := httptest.NewRecorder()

	newChatCompletionsHandler(http.DefaultClient, "test-key").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	want := "data: a\n\ndata: b\n\ndata: [DONE]\n\n"
	if rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}

func TestChatCompletionsHandler_InjectsIncludeUsage(t *testing.T) {
	var receivedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	pointUpstream(t, upstream.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	rec := httptest.NewRecorder()

	newChatCompletionsHandler(http.DefaultClient, "test-key").ServeHTTP(rec, req)

	var payload map[string]any
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("upstream nerima body bukan JSON valid: %s", receivedBody)
	}
	streamOptions, _ := payload["stream_options"].(map[string]any)
	if streamOptions["include_usage"] != true {
		t.Errorf("stream_options.include_usage nggak kesisip, upstream nerima: %s", receivedBody)
	}
}

func TestChatCompletionsHandler_DoesNotOverrideExistingIncludeUsage(t *testing.T) {
	var receivedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	pointUpstream(t, upstream.URL)

	body := `{"stream":true,"stream_options":{"include_usage":false}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	newChatCompletionsHandler(http.DefaultClient, "test-key").ServeHTTP(rec, req)

	var payload map[string]any
	json.Unmarshal(receivedBody, &payload)
	streamOptions, _ := payload["stream_options"].(map[string]any)
	if streamOptions["include_usage"] != false {
		t.Errorf("include_usage yang udah disetel klien ketimpa, upstream nerima: %s", receivedBody)
	}
}
