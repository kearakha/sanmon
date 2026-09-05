package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestHashToken_StableAndDistinct(t *testing.T) {
	a := hashToken("sk-sanmon-aaa")
	if a != hashToken("sk-sanmon-aaa") {
		t.Error("hash token nggak deterministik")
	}
	if a == hashToken("sk-sanmon-bbb") {
		t.Error("dua token beda ngasih hash sama")
	}
	if len(a) != 64 {
		t.Errorf("panjang hash hex = %d, want 64", len(a))
	}
}

func TestGenToken_PrefixAndUnique(t *testing.T) {
	t1, t2 := genToken(), genToken()
	if !strings.HasPrefix(t1, "sk-sanmon-") {
		t.Errorf("token = %q, harusnya diawali sk-sanmon-", t1)
	}
	if t1 == t2 {
		t.Error("genToken ngasih dua token identik")
	}
}

// TestKeysCRUD nembak ketiga handler lewat Postgres lokal asli: POST bikin
// key + balik token plaintext, GET list-nya nggak bocorin hash, DELETE
// nge-soft-delete (disabled=true, baris tetap ada).
func TestKeysCRUD(t *testing.T) {
	db := openTestDB(t)
	name := "keys-test-" + genToken()
	t.Cleanup(func() { db.Exec(`DELETE FROM keys WHERE name = $1`, name) })

	// POST
	body := `{"name":"` + name + `","rpm_limit":60,"monthly_budget_micro_usd":5000000}`
	rec := httptest.NewRecorder()
	newKeyCreateHandler(db).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/keys", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}
	var created struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode respons POST: %v", err)
	}
	if !strings.HasPrefix(created.Token, "sk-sanmon-") || created.ID == 0 {
		t.Fatalf("respons POST janggal: %+v", created)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("respons POST nggak set Access-Control-Allow-Origin, dashboard :8778 nggak bisa baca")
	}

	// hash di DB harus cocok sama token yang dibalikin, plaintext-nya nggak disimpan
	var stored string
	if err := db.QueryRow(`SELECT token_hash FROM keys WHERE id = $1`, created.ID).Scan(&stored); err != nil {
		t.Fatalf("baca token_hash: %v", err)
	}
	if stored != hashToken(created.Token) {
		t.Error("token_hash di DB nggak cocok sama hash token yang dibalikin")
	}
	if strings.Contains(stored, created.Token) {
		t.Error("token plaintext keliatan di kolom token_hash")
	}

	// GET — key baru ada, tanpa field token/token_hash
	rec = httptest.NewRecorder()
	newKeysListHandler(db).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/keys", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "token_hash") || strings.Contains(rec.Body.String(), created.Token) {
		t.Errorf("GET /admin/keys bocorin hash/token: %s", rec.Body)
	}
	var listResp struct {
		Keys []KeyRow `json:"keys"`
	}
	json.Unmarshal(rec.Body.Bytes(), &listResp)
	found := false
	for _, k := range listResp.Keys {
		if k.ID == created.ID {
			found = true
			if k.Disabled {
				t.Error("key baru kok udah disabled")
			}
		}
	}
	if !found {
		t.Fatal("key yang baru dibikin nggak muncul di GET")
	}

	// DELETE — soft delete, baris tetap ada tapi disabled
	idStr := strconv.FormatInt(created.ID, 10)
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/keys/"+idStr, nil)
	req.SetPathValue("id", idStr)
	newKeyDeleteHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("respons DELETE nggak set Access-Control-Allow-Origin")
	}
	var disabled bool
	if err := db.QueryRow(`SELECT disabled FROM keys WHERE id = $1`, created.ID).Scan(&disabled); err != nil {
		t.Fatalf("baris kehapus beneran, harusnya cuma disabled: %v", err)
	}
	if !disabled {
		t.Error("DELETE nggak nge-set disabled = true")
	}

	// DELETE id yang nggak ada → 404
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/admin/keys/99999999", nil)
	req.SetPathValue("id", "99999999")
	newKeyDeleteHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("DELETE id nggak ada status = %d, want 404", rec.Code)
	}
}

func TestKeyCreate_RejectsBadInput(t *testing.T) {
	db := openTestDB(t)
	cases := map[string]string{
		"tanpa name":     `{"rpm_limit":10}`,
		"rpm_limit nol":  `{"name":"x","rpm_limit":0}`,
		"budget negatif": `{"name":"x","monthly_budget_micro_usd":-1}`,
		"bukan json":     `{`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newKeyCreateHandler(db).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/keys", strings.NewReader(body)))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}
