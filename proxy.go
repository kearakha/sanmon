package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// var, bukan const — dites di proxy_test.go dengan diarahin ke httptest.Server palsu.
var geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"

func newChatCompletionsHandler(client *http.Client, apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
				return
			}
			writeJSONError(w, http.StatusBadGateway, "gagal hubungin upstream")
			return
		}
		defer resp.Body.Close()

		copyHeader(w, resp)
		w.WriteHeader(resp.StatusCode)

		if !streaming {
			io.Copy(w, resp.Body)
			return
		}

		streamBody(w, r, resp.Body)
	}
}

func newEmbeddingsHandler(client *http.Client, apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, geminiBaseURL+"/embeddings", r.Body)
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
				return
			}
			writeJSONError(w, http.StatusBadGateway, "gagal hubungin upstream")
			return
		}
		defer resp.Body.Close()

		copyHeader(w, resp)
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
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
// motong upstream begitu klien putus lewat context propagation.
func streamBody(w http.ResponseWriter, r *http.Request, upstream io.Reader) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		io.Copy(w, upstream)
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := upstream.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			flusher.Flush()
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			if r.Context().Err() != nil {
				slog.Info("stream dipotong: klien putus", "reason", r.Context().Err())
				return
			}
			slog.Warn("stream keputus di tengah jalan", "err", err)
			fmt.Fprint(w, "event: error\ndata: {\"error\":\"upstream stream interrupted\"}\n\n")
			flusher.Flush()
			return
		}
	}
}
