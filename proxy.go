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

func newChatCompletionsHandler(client *http.Client, apiKey string, hub *Hub) http.HandlerFunc {
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
				hub.Broadcast(Event{Time: time.Now(), Model: model, Stream: streaming, LatencyMs: time.Since(start).Milliseconds(), Partial: true, Error: "klien putus sebelum upstream jawab"})
				return
			}
			writeJSONError(w, http.StatusBadGateway, "gagal hubungin upstream")
			hub.Broadcast(Event{Time: time.Now(), Model: model, Stream: streaming, StatusCode: http.StatusBadGateway, LatencyMs: time.Since(start).Milliseconds(), Error: err.Error()})
			return
		}
		defer resp.Body.Close()

		copyHeader(w, resp)
		w.WriteHeader(resp.StatusCode)

		if !streaming {
			io.Copy(w, resp.Body)
			hub.Broadcast(Event{Time: time.Now(), Model: model, Stream: false, StatusCode: resp.StatusCode, LatencyMs: time.Since(start).Milliseconds()})
			return
		}

		partial, streamErr := streamBody(w, r, resp.Body)
		hub.Broadcast(Event{Time: time.Now(), Model: model, Stream: true, StatusCode: resp.StatusCode, LatencyMs: time.Since(start).Milliseconds(), Partial: partial, Error: streamErr})
	}
}

func newEmbeddingsHandler(client *http.Client, apiKey string, hub *Hub) http.HandlerFunc {
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
				hub.Broadcast(Event{Time: time.Now(), Model: model, LatencyMs: time.Since(start).Milliseconds(), Partial: true, Error: "klien putus sebelum upstream jawab"})
				return
			}
			writeJSONError(w, http.StatusBadGateway, "gagal hubungin upstream")
			hub.Broadcast(Event{Time: time.Now(), Model: model, StatusCode: http.StatusBadGateway, LatencyMs: time.Since(start).Milliseconds(), Error: err.Error()})
			return
		}
		defer resp.Body.Close()

		copyHeader(w, resp)
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		hub.Broadcast(Event{Time: time.Now(), Model: model, StatusCode: resp.StatusCode, LatencyMs: time.Since(start).Milliseconds()})
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
func streamBody(w http.ResponseWriter, r *http.Request, upstream io.Reader) (partial bool, errMsg string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		io.Copy(w, upstream)
		return false, ""
	}

	buf := make([]byte, 4096)
	for {
		n, err := upstream.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return true, ""
			}
			flusher.Flush()
		}
		if err != nil {
			if err == io.EOF {
				return false, ""
			}
			if r.Context().Err() != nil {
				slog.Info("stream dipotong: klien putus", "reason", r.Context().Err())
				return true, ""
			}
			slog.Warn("stream keputus di tengah jalan", "err", err)
			fmt.Fprint(w, "event: error\ndata: {\"error\":\"upstream stream interrupted\"}\n\n")
			flusher.Flush()
			return true, "upstream stream interrupted"
		}
	}
}
