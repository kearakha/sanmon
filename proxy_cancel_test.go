package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatCompletionsHandler_ClientCancelPropagatesToUpstream(t *testing.T) {
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: chunk1\n\n")
		flusher.Flush()

		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	defer upstream.Close()
	pointUpstream(t, upstream.URL)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":true}`)).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		newChatCompletionsHandler(http.DefaultClient, "test-key").ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-upstreamCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream nggak nerima context cancel dari klien")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler nggak balik setelah klien putus")
	}
}

func TestEmbeddingsHandler_Passthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":[{"embedding":[0.1,0.2]}]}`)
	}))
	defer upstream.Close()
	pointUpstream(t, upstream.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"input":"halo"}`))
	rec := httptest.NewRecorder()

	newEmbeddingsHandler(http.DefaultClient, "test-key").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	want := `{"data":[{"embedding":[0.1,0.2]}]}`
	if rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}
