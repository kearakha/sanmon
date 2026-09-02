package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireVirtualKey(t *testing.T) {
	db := openTestDB(t)

	token := genToken()
	name := "auth-test-" + token
	var keyID int64
	if err := db.QueryRow(
		`INSERT INTO keys (name, token_hash) VALUES ($1, $2) RETURNING id`,
		name, hashToken(token),
	).Scan(&keyID); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	disabledToken := genToken()
	if _, err := db.Exec(
		`INSERT INTO keys (name, token_hash, disabled) VALUES ($1, $2, true)`,
		name+"-off", hashToken(disabledToken),
	); err != nil {
		t.Fatalf("seed disabled key: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM keys WHERE name LIKE $1`, "auth-test-%") })

	var gotKeyID int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeyID = keyFromContext(r.Context()).ID
		w.WriteHeader(http.StatusOK)
	})
	handler := requireVirtualKey(db, newKeyLimiters(), next)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"token benar", "Bearer " + token, http.StatusOK},
		{"token nggak dikenal", "Bearer sk-sanmon-ngasal", http.StatusUnauthorized},
		{"key disabled", "Bearer " + disabledToken, http.StatusUnauthorized},
		{"tanpa header", "", http.StatusUnauthorized},
		{"tanpa prefix Bearer", token, http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotKeyID = 0
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusOK && gotKeyID != keyID {
				t.Errorf("key_id di context = %d, want %d", gotKeyID, keyID)
			}
		})
	}
}
