package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// pointUpstream ngarahin geminiBaseURL ke server test, terus balikin ke aslinya
// setelah test kelar.
func pointUpstream(t *testing.T, url string) {
	t.Helper()
	orig := geminiBaseURL
	geminiBaseURL = url
	t.Cleanup(func() { geminiBaseURL = orig })
}

// testStore balikin Store tanpa DB — Run sengaja nggak dipanggil, jadi Enqueue
// cuma numpuk di buffer / kebuang, insert nggak pernah kesentuh.
func testStore() *Store {
	return &Store{queue: make(chan Event, 16)}
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

	newChatCompletionsHandler(http.DefaultClient, "test-key", nil, NewHub(), testStore()).ServeHTTP(rec, req)

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

	newChatCompletionsHandler(http.DefaultClient, "test-key", nil, NewHub(), testStore()).ServeHTTP(rec, req)

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

	newChatCompletionsHandler(http.DefaultClient, "test-key", nil, NewHub(), testStore()).ServeHTTP(rec, req)

	var payload map[string]any
	json.Unmarshal(receivedBody, &payload)
	streamOptions, _ := payload["stream_options"].(map[string]any)
	if streamOptions["include_usage"] != false {
		t.Errorf("include_usage yang udah disetel klien ketimpa, upstream nerima: %s", receivedBody)
	}
}

func TestCalcCost(t *testing.T) {
	models := map[string]ModelConfig{
		"gemini-flash": {Provider: "gemini", Model: "gemini-3.6-flash", PriceInPerMillion: 150000, PriceOutPerMillion: 600000},
	}

	t.Run("model dikenal", func(t *testing.T) {
		provider, resolved, cost, unknown := calcCost(models, "gemini-flash", 1_000_000, 500_000)
		// 1e6*150000/1e6 + 5e5*600000/1e6 = 150000 + 300000
		if cost != 450000 {
			t.Errorf("cost = %d, want 450000", cost)
		}
		if provider != "gemini" || resolved != "gemini-3.6-flash" {
			t.Errorf("provider/resolved = %q/%q, want gemini/gemini-3.6-flash", provider, resolved)
		}
		if unknown {
			t.Error("cost_unknown = true, want false")
		}
	})

	t.Run("model tanpa harga di config", func(t *testing.T) {
		provider, resolved, cost, unknown := calcCost(models, "gpt-9", 1000, 1000)
		if !unknown {
			t.Error("cost_unknown = false, want true (aturan keras #3)")
		}
		if cost != 0 {
			t.Errorf("cost = %d, want 0", cost)
		}
		if provider != "gemini" || resolved != "" {
			t.Errorf("provider/resolved = %q/%q, want gemini/empty", provider, resolved)
		}
	})

	t.Run("token nol", func(t *testing.T) {
		if _, _, cost, _ := calcCost(models, "gemini-flash", 0, 0); cost != 0 {
			t.Errorf("cost = %d, want 0", cost)
		}
	})
}

func TestChatCompletionsHandler_ParsesNonStreamUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":7}}`)
	}))
	defer upstream.Close()
	pointUpstream(t, upstream.URL)

	hub := NewHub()
	go hub.Run()
	ch, unsub := hub.Subscribe()
	defer unsub()

	models := map[string]ModelConfig{"m": {Provider: "gemini", Model: "m-real", PriceInPerMillion: 2_000_000, PriceOutPerMillion: 4_000_000}}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	rec := httptest.NewRecorder()
	newChatCompletionsHandler(http.DefaultClient, "k", models, hub, testStore()).ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), `"content":"hi"`) {
		t.Errorf("respons upstream nggak diteruskan utuh ke klien: %q", rec.Body.String())
	}

	select {
	case ev := <-ch:
		if ev.TokensIn != 5 || ev.TokensOut != 7 {
			t.Errorf("tokens = %d/%d, want 5/7", ev.TokensIn, ev.TokensOut)
		}
		// 5*2000000/1e6 + 7*4000000/1e6 = 10 + 28
		if ev.CostMicroUSD != 38 {
			t.Errorf("cost = %d, want 38", ev.CostMicroUSD)
		}
		if ev.CostUnknown {
			t.Error("cost_unknown = true, want false")
		}
	case <-time.After(time.Second):
		t.Fatal("nggak ada event ke-broadcast")
	}
}

func TestChatCompletionsHandler_ParsesStreamUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, chunk := range []string{
			`data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n",
			`data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":22}}` + "\n\n",
			"data: [DONE]\n\n",
		} {
			fmt.Fprint(w, chunk)
			flusher.Flush()
		}
	}))
	defer upstream.Close()
	pointUpstream(t, upstream.URL)

	hub := NewHub()
	go hub.Run()
	ch, unsub := hub.Subscribe()
	defer unsub()

	models := map[string]ModelConfig{"m": {Provider: "gemini", Model: "m-real", PriceInPerMillion: 1_000_000, PriceOutPerMillion: 1_000_000}}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":true,"model":"m"}`))
	rec := httptest.NewRecorder()
	newChatCompletionsHandler(http.DefaultClient, "k", models, hub, testStore()).ServeHTTP(rec, req)

	select {
	case ev := <-ch:
		if ev.TokensIn != 11 || ev.TokensOut != 22 {
			t.Errorf("tokens = %d/%d, want 11/22", ev.TokensIn, ev.TokensOut)
		}
		// 11*1e6/1e6 + 22*1e6/1e6
		if ev.CostMicroUSD != 33 {
			t.Errorf("cost = %d, want 33", ev.CostMicroUSD)
		}
	case <-time.After(time.Second):
		t.Fatal("nggak ada event ke-broadcast")
	}
}
