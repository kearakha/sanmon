package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestBudgetTracker_AllowAndAdd(t *testing.T) {
	b := newBudgetTracker(nil)

	// budget <= 0 = tanpa batas
	for range 50 {
		if !b.allow(1, 0) {
			t.Fatal("budget 0 harusnya tanpa batas")
		}
	}

	// di bawah budget → lolos; nyampe budget → tolak
	b.add(2, 900)
	if !b.allow(2, 1000) {
		t.Error("kepakai 900 < budget 1000 harusnya lolos")
	}
	b.add(2, 100)
	if b.allow(2, 1000) {
		t.Error("kepakai 1000 >= budget 1000 harusnya ketolak")
	}

	// key lain nggak kena imbas
	if !b.allow(3, 1000) {
		t.Error("key 3 belum kepakai apa-apa, harusnya lolos")
	}

	// add nol / keyID nol diabaikan
	b.add(0, 500)
	b.add(4, 0)
	if !b.allow(4, 1) {
		t.Error("key 4 belum kepakai, harusnya lolos")
	}
}

func TestBudgetTracker_MonthRollResets(t *testing.T) {
	b := newBudgetTracker(nil)
	b.add(1, 5000)
	if b.allow(1, 4000) {
		t.Fatal("kepakai 5000 >= 4000 harusnya ketolak sebelum ganti bulan")
	}

	b.maybeRollMonth((b.month % 12) + 1) // bulan mana pun yang beda

	if !b.allow(1, 4000) {
		t.Error("abis ganti bulan, counter harusnya balik nol")
	}
}

func TestBudgetTracker_SeedFromDB(t *testing.T) {
	db := openTestDB(t)

	var keyID int64
	if err := db.QueryRow(
		`INSERT INTO keys (name, token_hash) VALUES ($1, $2) RETURNING id`,
		"budget-test", hashToken(genToken()),
	).Scan(&keyID); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM requests WHERE key_id = $1`, keyID)
		db.Exec(`DELETE FROM keys WHERE id = $1`, keyID)
	})

	// dua baris bulan ini + satu baris bulan lalu (harus nggak keitung)
	db.Exec(`INSERT INTO requests (created_at, key_id, model_requested, provider, cost_micro_usd) VALUES (now(), $1, 'm', 'gemini', 700)`, keyID)
	db.Exec(`INSERT INTO requests (created_at, key_id, model_requested, provider, cost_micro_usd) VALUES (now(), $1, 'm', 'gemini', 300)`, keyID)
	db.Exec(`INSERT INTO requests (created_at, key_id, model_requested, provider, cost_micro_usd) VALUES (now() - interval '40 days', $1, 'm', 'gemini', 999999)`, keyID)

	b := newBudgetTracker(db)
	if err := b.seed(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// kepakai bulan ini = 1000
	if !b.allow(keyID, 1001) {
		t.Error("seed harusnya 1000, < 1001 → lolos")
	}
	if b.allow(keyID, 1000) {
		t.Error("seed harusnya 1000, >= 1000 → ketolak (baris bulan lalu nggak boleh keitung)")
	}
}

func TestBudgetTracker_Concurrent(t *testing.T) {
	b := newBudgetTracker(nil)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			for range 20 {
				b.add(id%5, 10)
				b.allow(id%5, 1000)
			}
		}(int64(i))
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 10 {
			b.maybeRollMonth(b.month) // no-op tapi ngambil lock, adu sama add/allow
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Wait()
}
