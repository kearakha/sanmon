package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// dashboardOrigin itu port dev Vite yang tetap (lihat CLAUDE.md — port
// nggak boleh ganti/fallback diam-diam), bukan wildcard biar CORS-nya nggak
// asal kebuka.
const dashboardOrigin = "http://localhost:8778"

// writeDashboardCORS ngizinin dashboard dev (port tetap :8778) baca respons
// cross-origin. GET tanpa header custom = simple request, jadi nggak perlu
// handle preflight OPTIONS.
func writeDashboardCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", dashboardOrigin)
}

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
		writeDashboardCORS(w)
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

// RequestRow satu baris tabel requests buat endpoint riwayat. Kolom yang bisa
// NULL (model_resolved, error, latency_ms, ttfb_ms, token, cost, body) pakai
// pointer biar NULL kebedain dari nol pas di-JSON-in — baris yang ditulis
// sebelum usage keparse emang belum punya angkanya.
type RequestRow struct {
	ID             int64     `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	ModelRequested string    `json:"model_requested"`
	Provider       string    `json:"provider"`
	ModelResolved  *string   `json:"model_resolved"`
	Stream         bool      `json:"stream"`
	StatusCode     *int      `json:"status_code"`
	Error          *string   `json:"error"`
	LatencyMs      *int64    `json:"latency_ms"`
	TokensIn       *int64    `json:"tokens_in"`
	TokensOut      *int64    `json:"tokens_out"`
	CostMicroUSD   *int64    `json:"cost_micro_usd"`
	CostUnknown    bool      `json:"cost_unknown"`
	Partial        bool      `json:"partial"`

	// Cuma diisi endpoint detail; NULL sampai log_bodies aktif di M4.
	TtfbMs       *int64  `json:"ttfb_ms,omitempty"`
	RequestBody  *string `json:"request_body,omitempty"`
	ResponseBody *string `json:"response_body,omitempty"`
}

const requestListCols = `id, created_at, model_requested, provider, model_resolved, stream,
	status_code, error, latency_ms, tokens_in, tokens_out, cost_micro_usd, cost_unknown, partial`

// newRequestsListHandler ngelayanin GET /admin/requests — daftar riwayat,
// urut terbaru dulu. Pagination keyset lewat ?before_id (bukan offset: tabel
// requests diisi terus tiap request masuk, offset bakal bikin baris
// dobel/kelewat). Filter opsional: ?model, ?status.
func newRequestsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		limit := 50
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				writeJSONError(w, http.StatusBadRequest, "limit harus angka positif")
				return
			}
			limit = min(n, 200)
		}

		where := "WHERE 1=1"
		var args []any
		if v := q.Get("before_id"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "before_id harus angka")
				return
			}
			args = append(args, n)
			where += fmt.Sprintf(" AND id < $%d", len(args))
		}
		if v := q.Get("model"); v != "" {
			args = append(args, v)
			where += fmt.Sprintf(" AND model_requested = $%d", len(args))
		}
		if v := q.Get("status"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "status harus angka")
				return
			}
			args = append(args, n)
			where += fmt.Sprintf(" AND status_code = $%d", len(args))
		}
		args = append(args, limit)

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		rows, err := db.QueryContext(ctx,
			"SELECT "+requestListCols+" FROM requests "+where+
				fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args)),
			args...)
		if err != nil {
			slog.Warn("query riwayat gagal", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "gagal baca riwayat")
			return
		}
		defer rows.Close()

		list := []RequestRow{}
		for rows.Next() {
			var rr RequestRow
			if err := rows.Scan(
				&rr.ID, &rr.CreatedAt, &rr.ModelRequested, &rr.Provider, &rr.ModelResolved, &rr.Stream,
				&rr.StatusCode, &rr.Error, &rr.LatencyMs, &rr.TokensIn, &rr.TokensOut,
				&rr.CostMicroUSD, &rr.CostUnknown, &rr.Partial,
			); err != nil {
				slog.Warn("scan baris riwayat gagal", "err", err)
				writeJSONError(w, http.StatusInternalServerError, "gagal baca riwayat")
				return
			}
			list = append(list, rr)
		}
		if err := rows.Err(); err != nil {
			slog.Warn("iterasi baris riwayat gagal", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "gagal baca riwayat")
			return
		}

		var nextBeforeID *int64
		if len(list) == limit {
			nextBeforeID = &list[len(list)-1].ID
		}

		writeDashboardCORS(w)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"requests":       list,
			"next_before_id": nextBeforeID,
		})
	}
}

// newRequestHandler ngelayanin GET /admin/requests/{id} — satu request lengkap
// termasuk body (NULL sampai M4). 404 kalau id-nya nggak ada.
func newRequestHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "id harus angka")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		var rr RequestRow
		err = db.QueryRowContext(ctx, `
			SELECT id, created_at, model_requested, provider, model_resolved, stream,
				status_code, error, latency_ms, ttfb_ms, tokens_in, tokens_out,
				cost_micro_usd, cost_unknown, partial, request_body, response_body
			FROM requests WHERE id = $1`, id).Scan(
			&rr.ID, &rr.CreatedAt, &rr.ModelRequested, &rr.Provider, &rr.ModelResolved, &rr.Stream,
			&rr.StatusCode, &rr.Error, &rr.LatencyMs, &rr.TtfbMs, &rr.TokensIn, &rr.TokensOut,
			&rr.CostMicroUSD, &rr.CostUnknown, &rr.Partial, &rr.RequestBody, &rr.ResponseBody,
		)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "request nggak ketemu")
			return
		}
		if err != nil {
			slog.Warn("query detail riwayat gagal", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "gagal baca detail")
			return
		}

		writeDashboardCORS(w)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rr)
	}
}
