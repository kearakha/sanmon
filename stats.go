package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// dashboardTZ zona waktu operator buat motong batas hari di agregat. Konstanta,
// bukan config: ini "di mana Rakha lihat dashboard", nggak berubah-ubah. Tanpa
// ini bucket-nya kepotong di tengah malam UTC — request jam 6 pagi WIB nyasar
// ke "kemarin".
const dashboardTZ = "Asia/Jakarta"

// DailyStat satu hari agregat dari tabel requests. Day dipotong di dashboardTZ.
// CostMicroUSD cuma nge-sum baris cost_unknown=false — nol yang jujur, bukan
// nol yang bohong (aturan keras #3); baris tanpa harga kehitung di
// CostUnknownCount doang.
type DailyStat struct {
	Day              string `json:"day"` // YYYY-MM-DD di dashboardTZ
	Requests         int64  `json:"requests"`
	TokensIn         int64  `json:"tokens_in"`
	TokensOut        int64  `json:"tokens_out"`
	CostMicroUSD     int64  `json:"cost_micro_usd"`
	CostUnknownCount int64  `json:"cost_unknown_count"`
	Errors           int64  `json:"errors"`
}

// newStatsHandler ngelayanin GET /admin/stats — agregat harian tabel requests,
// hari terbaru dulu. Window default 30 hari (nyamain retention M3); ?days=N
// (1..90) buat mempersempit. Filter opsional ?model (exact, kayak
// /admin/requests).
func newStatsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		days := 30
		if v := q.Get("days"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				writeJSONError(w, http.StatusBadRequest, "days harus angka positif")
				return
			}
			days = min(n, 90)
		}

		// $1 = dashboardTZ, $2 = days-1 (offset hari dari hari ini, lokal).
		// ::timestamp sebelum AT TIME ZONE penting: tanpa itu `date AT TIME ZONE`
		// nge-convert (bukan nge-interpret) dan batas window-nya meleset 7 jam.
		args := []any{dashboardTZ, days - 1}
		where := `created_at >= ((now() AT TIME ZONE $1)::date - $2::int)::timestamp AT TIME ZONE $1`
		if v := q.Get("model"); v != "" {
			args = append(args, v)
			where += fmt.Sprintf(" AND model_requested = $%d", len(args))
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		rows, err := db.QueryContext(ctx, `
			SELECT to_char(date(created_at AT TIME ZONE $1), 'YYYY-MM-DD') AS day,
			       count(*) AS requests,
			       coalesce(sum(tokens_in), 0) AS tokens_in,
			       coalesce(sum(tokens_out), 0) AS tokens_out,
			       coalesce(sum(cost_micro_usd) FILTER (WHERE NOT cost_unknown), 0) AS cost_micro_usd,
			       count(*) FILTER (WHERE cost_unknown) AS cost_unknown_count,
			       count(*) FILTER (WHERE status_code >= 400 OR error IS NOT NULL) AS errors
			FROM requests
			WHERE `+where+`
			GROUP BY day
			ORDER BY day DESC`, args...)
		if err != nil {
			slog.Warn("query agregat harian gagal", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "gagal baca agregat")
			return
		}
		defer rows.Close()

		list := []DailyStat{}
		for rows.Next() {
			var d DailyStat
			if err := rows.Scan(
				&d.Day, &d.Requests, &d.TokensIn, &d.TokensOut,
				&d.CostMicroUSD, &d.CostUnknownCount, &d.Errors,
			); err != nil {
				slog.Warn("scan baris agregat gagal", "err", err)
				writeJSONError(w, http.StatusInternalServerError, "gagal baca agregat")
				return
			}
			list = append(list, d)
		}
		if err := rows.Err(); err != nil {
			slog.Warn("iterasi baris agregat gagal", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "gagal baca agregat")
			return
		}

		writeDashboardCORS(w)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"days": list})
	}
}
