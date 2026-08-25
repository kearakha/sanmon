package main

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
)

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}

// requireBearerToken dipakai buat /v1/* (virtual key) dan /admin/* yang
// baca token dari header (bukan query param — itu jalur SSE di admin.go).
func requireBearerToken(expected string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
