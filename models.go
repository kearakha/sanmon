package main

import (
	"io"
	"net/http"
)

func newModelsHandler(client *http.Client, apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, geminiBaseURL+"/models", nil)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "gagal susun request upstream")
			return
		}
		upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := client.Do(upstreamReq)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "gagal hubungin upstream")
			return
		}
		defer resp.Body.Close()

		copyHeader(w, resp)
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}
