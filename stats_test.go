package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatsHandler_DailyAggregate(t *testing.T) {
	db := openTestDB(t)

	marker := "stats-agg-test"
	t.Cleanup(func() { db.Exec(`DELETE FROM requests WHERE model_requested = $1`, marker) })

	// Semua seed dalam WIB (UTC+7, nggak ada DST). Baris jam 01:00 WIB =
	// 18:00 UTC hari sebelumnya — sengaja, buat mastiin bucket dipotong di
	// zona Jakarta, bukan UTC.
	wib := time.FixedZone("WIB", 7*3600)
	now := time.Now().In(wib)
	atToday := func(h int) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, wib)
	}
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	seed := func(ts time.Time, status int, tokIn, tokOut, cost int, costUnknown bool, errMsg string) {
		t.Helper()
		var e any
		if errMsg != "" {
			e = errMsg
		}
		_, err := db.Exec(`
			INSERT INTO requests (created_at, model_requested, provider, stream,
				status_code, error, tokens_in, tokens_out, cost_micro_usd, cost_unknown)
			VALUES ($1, $2, 'gemini', false, $3, $4, $5, $6, $7, $8)`,
			ts, marker, status, e, tokIn, tokOut, cost, costUnknown)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Hari ini: 2 sukses (cost 10 + 20), 1 cost_unknown (cost col 999 harus
	// diabaikan), 1 error 500.
	seed(atToday(1), 200, 10, 20, 10, false, "") // 01:00 WIB — kemarin kalau UTC
	seed(atToday(9), 200, 20, 40, 20, false, "")
	seed(atToday(10), 200, 5, 5, 999, true, "")
	seed(atToday(11), 500, 0, 0, 0, false, "boom")
	// Kemarin: 1 sukses.
	seed(atToday(8).AddDate(0, 0, -1), 200, 1, 2, 3, false, "")

	handler := newStatsHandler(db)
	get := func(query string) map[string]DailyStat {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/admin/stats?"+query, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var out struct {
			Days []DailyStat `json:"days"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		byDay := map[string]DailyStat{}
		for _, d := range out.Days {
			byDay[d.Day] = d
		}
		return byDay
	}

	t.Run("bucket harian, cost_unknown dikecualiin dari biaya", func(t *testing.T) {
		days := get("model=" + marker)

		d, ok := days[today]
		if !ok {
			t.Fatalf("hari ini (%s) nggak ada di hasil: %+v", today, days)
		}
		if d.Requests != 4 {
			t.Errorf("Requests = %d, want 4", d.Requests)
		}
		if d.TokensIn != 35 || d.TokensOut != 65 {
			t.Errorf("token in/out = %d/%d, want 35/65", d.TokensIn, d.TokensOut)
		}
		if d.CostMicroUSD != 30 {
			t.Errorf("CostMicroUSD = %d, want 30 (999 dari baris cost_unknown harus diabaikan)", d.CostMicroUSD)
		}
		if d.CostUnknownCount != 1 {
			t.Errorf("CostUnknownCount = %d, want 1", d.CostUnknownCount)
		}
		if d.Errors != 1 {
			t.Errorf("Errors = %d, want 1", d.Errors)
		}

		y, ok := days[yesterday]
		if !ok {
			t.Fatalf("kemarin (%s) nggak ada di hasil: %+v", yesterday, days)
		}
		if y.Requests != 1 || y.CostMicroUSD != 3 || y.Errors != 0 {
			t.Errorf("kemarin = %+v, want Requests 1 / Cost 3 / Errors 0", y)
		}
	})

	t.Run("terbaru dulu", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/stats?model="+marker, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var out struct {
			Days []DailyStat `json:"days"`
		}
		json.Unmarshal(rec.Body.Bytes(), &out)
		if len(out.Days) < 2 || out.Days[0].Day <= out.Days[1].Day {
			t.Errorf("urutan hari = %+v, want DESC", out.Days)
		}
	})

	t.Run("days=1 buang kemarin", func(t *testing.T) {
		days := get("model=" + marker + "&days=1")
		if _, ok := days[yesterday]; ok {
			t.Errorf("days=1 masih ngeluarin kemarin (%s)", yesterday)
		}
		if _, ok := days[today]; !ok {
			t.Errorf("days=1 harusnya tetap ada hari ini (%s)", today)
		}
	})

	t.Run("days bukan angka → 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/stats?days=abc", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}
