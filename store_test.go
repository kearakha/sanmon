package main

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// testDatabaseURL nunjuk ke Postgres lokal dari docker-compose.yml. Test
// yang butuh DB di-skip kalau nggak konek, bukan gagal — mesin lain/CI belum
// tentu udah nyalain docker compose.
const testDatabaseURL = "postgres://sanmon:sanmon@localhost:5435/sanmon?sslmode=disable"

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openDB(testDatabaseURL)
	if err != nil {
		t.Skipf("postgres lokal nggak konek, skip: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestStore_EnqueueNonBlockingWhenQueueFull(t *testing.T) {
	store := &Store{queue: make(chan Event, 4)}
	// store.Run sengaja nggak dipanggil — Enqueue harus tetep nggak nge-hang
	// walau nggak ada yang narik dari queue.

	done := make(chan struct{})
	go func() {
		// Cuma 20 kali — cukup buat mastiin Enqueue nggak nge-hang pas queue
		// penuh, tanpa nge-spam log "queue penuh" tiap kali dites.
		for range 20 {
			store.Enqueue(Event{Model: "gemini-flash"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Enqueue blocking padahal harusnya non-blocking")
	}
}

func TestStore_InsertThenDrainOnShutdown(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		store.Run(ctx)
		close(runDone)
	}()

	marker := "store-test-marker"
	store.Enqueue(Event{Time: time.Now(), Model: marker, Provider: "gemini", Stream: false, StatusCode: 200, LatencyMs: 42})
	cancel() // shutdown: Run harus drain event di atas sebelum return

	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("Run nggak return setelah ctx dibatalin")
	}

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM requests WHERE model_requested = $1`, marker).Scan(&count); err != nil {
		t.Fatalf("query verifikasi: %v", err)
	}
	if count != 1 {
		t.Errorf("baris ke-insert = %d, want 1 (event harusnya sempet ke-drain sebelum Run return)", count)
	}

	db.Exec(`DELETE FROM requests WHERE model_requested = $1`, marker)
}

func TestStore_DeleteOlderThanRemovesOnlyOldRows(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	oldMarker := "store-test-old"
	newMarker := "store-test-new"
	db.Exec(`INSERT INTO requests (created_at, model_requested, provider, stream) VALUES ($1, $2, 'gemini', false)`, time.Now().Add(-40*24*time.Hour), oldMarker)
	db.Exec(`INSERT INTO requests (created_at, model_requested, provider, stream) VALUES ($1, $2, 'gemini', false)`, time.Now(), newMarker)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM requests WHERE model_requested IN ($1, $2)`, oldMarker, newMarker)
	})

	store.deleteOlderThan(30 * 24 * time.Hour)

	var oldCount, newCount int
	db.QueryRow(`SELECT count(*) FROM requests WHERE model_requested = $1`, oldMarker).Scan(&oldCount)
	db.QueryRow(`SELECT count(*) FROM requests WHERE model_requested = $1`, newMarker).Scan(&newCount)

	if oldCount != 0 {
		t.Errorf("baris lama (40 hari) = %d, want 0 (harusnya kehapus)", oldCount)
	}
	if newCount != 1 {
		t.Errorf("baris baru = %d, want 1 (harusnya nggak kehapus)", newCount)
	}
}
