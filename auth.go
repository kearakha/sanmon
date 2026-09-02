package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}

// authedKey itu data key yang lolos auth, dititipin lewat context ke handler
// proxy — dipakai buat ngisi requests.key_id (dan nanti rate limit + budget).
type authedKey struct {
	ID int64
}

type ctxKey int

const keyCtxKey ctxKey = 0

func keyFromContext(ctx context.Context) authedKey {
	k, _ := ctx.Value(keyCtxKey).(authedKey)
	return k
}

// requireVirtualKey auth buat /v1/* : ambil Bearer token, hash SHA-256, cocokin
// ke kolom token_hash (UNIQUE, jadi sekali query ber-index) yang belum
// disabled. Nggak ketemu → 401. Ketemu tapi udah lewat jatah rpm_limit → 429.
// Lolos → key-nya dititipin ke context.
func requireVirtualKey(db *sql.DB, lim *keyLimiters, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var k authedKey
		var rpm sql.NullInt64
		err := db.QueryRowContext(r.Context(),
			`SELECT id, rpm_limit FROM keys WHERE token_hash = $1 AND NOT disabled`,
			hashToken(token)).Scan(&k.ID, &rpm)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "gagal verifikasi key")
			return
		}

		if !lim.allow(k.ID, int(rpm.Int64)) {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, 60/int(rpm.Int64))))
			writeJSONError(w, http.StatusTooManyRequests, "rate limit key kelewat")
			return
		}

		ctx := context.WithValue(r.Context(), keyCtxKey, k)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
