package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// hashToken bikin sidik SHA-256 hex dari virtual key. Bukan bcrypt: virtual
// key itu token acak entropi-tinggi (bukan password), jadi nggak butuh
// key-stretching — dan hash deterministik bikin auth-nya sekali query
// ber-index (token_hash UNIQUE), bukan scan-lalu-bandingin sebaris-sebaris.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// genToken bikin virtual key baru: prefix sk-sanmon- + 26 char base32 acak
// (~130 bit) dari crypto/rand.Text.
func genToken() string {
	return "sk-sanmon-" + rand.Text()
}

// KeyRow satu baris tabel keys buat GET /admin/keys. token_hash sengaja
// nggak ikut — nggak ada gunanya dibalikin ke klien, cukup nambah permukaan
// bocor.
type KeyRow struct {
	ID                    int64     `json:"id"`
	Name                  string    `json:"name"`
	MonthlyBudgetMicroUSD *int64    `json:"monthly_budget_micro_usd"`
	RPMLimit              *int      `json:"rpm_limit"`
	LogBodies             bool      `json:"log_bodies"`
	Disabled              bool      `json:"disabled"`
	CreatedAt             time.Time `json:"created_at"`
}

// newKeysListHandler ngelayanin GET /admin/keys — semua key, termasuk yang
// udah disabled (soft delete), urut id naik.
func newKeysListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		rows, err := db.QueryContext(ctx, `
			SELECT id, name, monthly_budget_micro_usd, rpm_limit, log_bodies, disabled, created_at
			FROM keys ORDER BY id`)
		if err != nil {
			slog.Warn("query keys gagal", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "gagal baca keys")
			return
		}
		defer rows.Close()

		list := []KeyRow{}
		for rows.Next() {
			var k KeyRow
			if err := rows.Scan(&k.ID, &k.Name, &k.MonthlyBudgetMicroUSD, &k.RPMLimit,
				&k.LogBodies, &k.Disabled, &k.CreatedAt); err != nil {
				slog.Warn("scan key gagal", "err", err)
				writeJSONError(w, http.StatusInternalServerError, "gagal baca keys")
				return
			}
			list = append(list, k)
		}
		if err := rows.Err(); err != nil {
			slog.Warn("iterasi keys gagal", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "gagal baca keys")
			return
		}

		writeDashboardCORS(w)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"keys": list})
	}
}

type keyCreateReq struct {
	Name                  string `json:"name"`
	MonthlyBudgetMicroUSD *int64 `json:"monthly_budget_micro_usd"`
	RPMLimit              *int   `json:"rpm_limit"`
	LogBodies             bool   `json:"log_bodies"`
}

// newKeyCreateHandler ngelayanin POST /admin/keys — generate virtual key,
// simpan hash-nya, balikin token plaintext SEKALI di respons ini (nggak bisa
// diambil lagi setelahnya).
func newKeyCreateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req keyCreateReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "body bukan JSON valid")
			return
		}
		if req.Name == "" {
			writeJSONError(w, http.StatusBadRequest, "name wajib diisi")
			return
		}
		if req.RPMLimit != nil && *req.RPMLimit < 1 {
			writeJSONError(w, http.StatusBadRequest, "rpm_limit harus >= 1")
			return
		}
		if req.MonthlyBudgetMicroUSD != nil && *req.MonthlyBudgetMicroUSD < 0 {
			writeJSONError(w, http.StatusBadRequest, "monthly_budget_micro_usd nggak boleh negatif")
			return
		}

		token := genToken()

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		var id int64
		err := db.QueryRowContext(ctx, `
			INSERT INTO keys (name, token_hash, monthly_budget_micro_usd, rpm_limit, log_bodies)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			req.Name, hashToken(token), req.MonthlyBudgetMicroUSD, req.RPMLimit, req.LogBodies,
		).Scan(&id)
		if err != nil {
			slog.Warn("insert key gagal", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "gagal bikin key")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":    id,
			"name":  req.Name,
			"token": token, // plaintext — cuma muncul di sini, sekali
		})
	}
}

// newKeyDeleteHandler ngelayanin DELETE /admin/keys/{id} — soft delete
// (disabled = true), biar baris requests.key_id historis tetap nunjuk ke key
// ini. 404 kalau id-nya nggak ada.
func newKeyDeleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "id harus angka")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		res, err := db.ExecContext(ctx, `UPDATE keys SET disabled = true WHERE id = $1`, id)
		if err != nil {
			slog.Warn("disable key gagal", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "gagal hapus key")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeJSONError(w, http.StatusNotFound, "key nggak ketemu")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
