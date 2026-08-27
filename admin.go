package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// dashboardOrigin itu port dev Vite yang tetap (lihat CLAUDE.md — port
// nggak boleh ganti/fallback diam-diam), bukan wildcard biar CORS-nya nggak
// asal kebuka.
const dashboardOrigin = "http://localhost:8778"

// requireAdminQueryToken dipisah dari requireBearerToken (auth.go) karena
// sumber token-nya beda: EventSource (dipake buat SSE /admin/stream) nggak
// bisa kirim header Authorization custom, cuma bisa lewat query param.
func requireAdminQueryToken(expected string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	}
}

// newStreamHandler ngelayanin GET /admin/stream — SSE fan-out lewat hub.
// Klien putus (tab ditutup) → r.Context() ke-cancel → handler ini balik dan
// unsubscribe, jadi channel & entry-nya di hub nggak bocor.
func newStreamHandler(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "streaming nggak didukung")
			return
		}

		ch, unsubscribe := hub.Subscribe()
		defer unsubscribe()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", dashboardOrigin)
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				data, err := json.Marshal(ev)
				if err != nil {
					slog.Warn("gagal marshal event live feed", "err", err)
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()

			case <-r.Context().Done():
				return
			}
		}
	}
}
