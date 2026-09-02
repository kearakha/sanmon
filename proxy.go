package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// var, bukan const — dites di proxy_test.go dengan diarahin ke httptest.Server palsu.
var geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"

var usageMarker = []byte(`"usage"`)

// emitEvent nyebar satu Event ke live feed (Hub) sekaligus antrian persist
// (Store). Dua-duanya non-blocking — jalur proxy nggak pernah nunggu (aturan
// keras #1). Provider di-default ke "gemini" (satu-satunya upstream sampai
// OpenRouter masuk di M4) biar kolom NOT NULL di tabel requests kepenuhin
// walau jalur error nggak sempet resolve lewat calcCost.
func emitEvent(hub *Hub, store *Store, ev Event) {
	if ev.Provider == "" {
		ev.Provider = "gemini"
	}
	hub.Broadcast(ev)
	store.Enqueue(ev)
}

// calcCost nyocokin model yang diminta klien ke tabel harga di config.
// Ketemu → biaya integer micro-USD (aturan keras #2, no float). Nggak ketemu →
// cost_unknown, biaya 0 (aturan keras #3 — nol jujur, bukan nol bohong).
func calcCost(models map[string]ModelConfig, model string, tokensIn, tokensOut int) (provider, modelResolved string, costMicroUSD int64, costUnknown bool) {
	mc, ok := models[model]
	if !ok {
		return "gemini", "", 0, true
	}
	cost := int64(tokensIn)*mc.PriceInPerMillion/1_000_000 + int64(tokensOut)*mc.PriceOutPerMillion/1_000_000
	return mc.Provider, mc.Model, cost, false
}

// parseUsage baca objek "usage" dari body respons JSON (non-stream chat atau
// embeddings). Best-effort — body bukan JSON / nggak ada usage → 0, 0.
func parseUsage(body []byte) (tokensIn, tokensOut int) {
	var r struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	json.Unmarshal(body, &r)
	return r.Usage.PromptTokens, r.Usage.CompletionTokens
}

// parseSSEUsage baca objek usage dari satu event SSE ("data: {...}").
func parseSSEUsage(event []byte) (tokensIn, tokensOut int) {
	_, after, found := bytes.Cut(event, []byte("data:"))
	if !found {
		return 0, 0
	}
	return parseUsage(bytes.TrimSpace(after))
}

func newChatCompletionsHandler(client *http.Client, apiKey string, models map[string]ModelConfig, hub *Hub, store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "gagal baca request body")
			return
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			writeJSONError(w, http.StatusBadRequest, "request body bukan JSON valid")
			return
		}

		model, _ := payload["model"].(string)
		streaming, _ := payload["stream"].(bool)
		if streaming {
			streamOptions, ok := payload["stream_options"].(map[string]any)
			if !ok {
				streamOptions = map[string]any{}
			}
			if _, has := streamOptions["include_usage"]; !has {
				streamOptions["include_usage"] = true
				payload["stream_options"] = streamOptions
				body, err = json.Marshal(payload)
				if err != nil {
					writeJSONError(w, http.StatusInternalServerError, "gagal susun ulang request body")
					return
				}
			}
		}

		upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, geminiBaseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "gagal susun request upstream")
			return
		}
		upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
		upstreamReq.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(upstreamReq)
		if err != nil {
			if r.Context().Err() != nil {
				slog.Info("chat completions dibatalin", "reason", r.Context().Err())
				emitEvent(hub, store, Event{Time: time.Now(), Model: model, Stream: streaming, LatencyMs: time.Since(start).Milliseconds(), Partial: true, Error: "klien putus sebelum upstream jawab"})
				return
			}
			writeJSONError(w, http.StatusBadGateway, "gagal hubungin upstream")
			emitEvent(hub, store, Event{Time: time.Now(), Model: model, Stream: streaming, StatusCode: http.StatusBadGateway, LatencyMs: time.Since(start).Milliseconds(), Error: err.Error()})
			return
		}
		defer resp.Body.Close()

		copyHeader(w, resp)
		w.WriteHeader(resp.StatusCode)

		if !streaming {
			respBody, _ := io.ReadAll(resp.Body)
			w.Write(respBody)
			tokensIn, tokensOut := parseUsage(respBody)
			provider, resolved, cost, unknown := calcCost(models, model, tokensIn, tokensOut)
			emitEvent(hub, store, Event{
				Time: time.Now(), Model: model, Provider: provider, ModelResolved: resolved,
				Stream: false, StatusCode: resp.StatusCode, LatencyMs: time.Since(start).Milliseconds(),
				TokensIn: tokensIn, TokensOut: tokensOut, CostMicroUSD: cost, CostUnknown: unknown,
			})
			return
		}

		partial, streamErr, tokensIn, tokensOut := streamBody(w, r, resp.Body)
		provider, resolved, cost, unknown := calcCost(models, model, tokensIn, tokensOut)
		emitEvent(hub, store, Event{
			Time: time.Now(), Model: model, Provider: provider, ModelResolved: resolved,
			Stream: true, StatusCode: resp.StatusCode, LatencyMs: time.Since(start).Milliseconds(),
			TokensIn: tokensIn, TokensOut: tokensOut, CostMicroUSD: cost, CostUnknown: unknown,
			Partial: partial, Error: streamErr,
		})
	}
}

func newEmbeddingsHandler(client *http.Client, apiKey string, models map[string]ModelConfig, hub *Hub, store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "gagal baca request body")
			return
		}

		var payload map[string]any
		json.Unmarshal(body, &payload) // best-effort, model cuma buat live feed
		model, _ := payload["model"].(string)

		upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, geminiBaseURL+"/embeddings", bytes.NewReader(body))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "gagal susun request upstream")
			return
		}
		upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
		upstreamReq.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(upstreamReq)
		if err != nil {
			if r.Context().Err() != nil {
				slog.Info("embeddings dibatalin", "reason", r.Context().Err())
				emitEvent(hub, store, Event{Time: time.Now(), Model: model, LatencyMs: time.Since(start).Milliseconds(), Partial: true, Error: "klien putus sebelum upstream jawab"})
				return
			}
			writeJSONError(w, http.StatusBadGateway, "gagal hubungin upstream")
			emitEvent(hub, store, Event{Time: time.Now(), Model: model, StatusCode: http.StatusBadGateway, LatencyMs: time.Since(start).Milliseconds(), Error: err.Error()})
			return
		}
		defer resp.Body.Close()

		copyHeader(w, resp)
		w.WriteHeader(resp.StatusCode)
		respBody, _ := io.ReadAll(resp.Body)
		w.Write(respBody)

		tokensIn, _ := parseUsage(respBody) // embeddings: cuma input token, nggak ada output
		provider, resolved, cost, unknown := calcCost(models, model, tokensIn, 0)
		emitEvent(hub, store, Event{
			Time: time.Now(), Model: model, Provider: provider, ModelResolved: resolved,
			StatusCode: resp.StatusCode, LatencyMs: time.Since(start).Milliseconds(),
			TokensIn: tokensIn, CostMicroUSD: cost, CostUnknown: unknown,
		})
	}
}

func copyHeader(w http.ResponseWriter, resp *http.Response) {
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
}

// streamBody nyalurin SSE chunk demi chunk (nggak numpuk di memori) dan
// motong upstream begitu klien putus lewat context propagation. Balikin
// partial=true kalau stream nggak nyampe EOF wajar (klien putus / error
// upstream di tengah), buat ngisi Event.Partial di live feed.
//
// Sambil nyalurin, tiap event SSE lengkap discan: yang punya "usage" disimpan,
// lalu di-parse pas stream kelar → tokensIn/tokensOut. Buffer tetap bounded
// (~satu chunk + satu baris usage), bukan numpuk seluruh stream.
func streamBody(w http.ResponseWriter, r *http.Request, upstream io.Reader) (partial bool, errMsg string, tokensIn, tokensOut int) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		io.Copy(w, upstream)
		return false, "", 0, 0
	}

	buf := make([]byte, 4096)
	var pending []byte   // byte SSE yang belum ketemu batas "\n\n"
	var lastUsage []byte // event terakhir yang ngandung "usage"
	for {
		n, err := upstream.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return true, "", 0, 0
			}
			flusher.Flush()

			pending = append(pending, buf[:n]...)
			for {
				i := bytes.Index(pending, []byte("\n\n"))
				if i < 0 {
					break
				}
				if event := pending[:i]; bytes.Contains(event, usageMarker) {
					lastUsage = append(lastUsage[:0], event...)
				}
				pending = pending[i+2:]
			}
		}
		if err != nil {
			if err == io.EOF {
				ti, to := parseSSEUsage(lastUsage)
				return false, "", ti, to
			}
			if r.Context().Err() != nil {
				slog.Info("stream dipotong: klien putus", "reason", r.Context().Err())
				return true, "", 0, 0
			}
			slog.Warn("stream keputus di tengah jalan", "err", err)
			fmt.Fprint(w, "event: error\ndata: {\"error\":\"upstream stream interrupted\"}\n\n")
			flusher.Flush()
			return true, "upstream stream interrupted", 0, 0
		}
	}
}
