package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// Store adalah worker satu goroutine yang nulis Event ke tabel requests.
// Enqueue non-blocking, sama pola kayak Hub.Broadcast: antrian penuh atau
// Postgres ngambek, event dibuang dan jalur proxy tetap jalan (aturan keras
// #1 — cuma target-nya di sini DB, bukan fan-out SSE).
type Store struct {
	db    *sql.DB
	queue chan Event
}

// ponytail: buffer 256, naikin kalau log sering kebuang pas trafik lagi ramai.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, queue: make(chan Event, 256)}
}

// Enqueue ngirim event buat ditulis ke Postgres. Non-blocking — antrian
// penuh, event dibuang, proxy jalan terus.
func (s *Store) Enqueue(ev Event) {
	select {
	case s.queue <- ev:
	default:
		slog.Warn("store queue penuh, request log dibuang")
	}
}

// Run harus dipanggil di goroutine terpisah (go store.Run(ctx)) dan hidup
// selama proses server jalan. Pas ctx dibatalin (shutdown), Run drain sisa
// antrian yang udah kepush sebelum return — bukan nunggu event baru lagi.
func (s *Store) Run(ctx context.Context) {
	for {
		select {
		case ev := <-s.queue:
			s.insert(ev)
		case <-ctx.Done():
			s.drain()
			return
		}
	}
}

func (s *Store) drain() {
	for {
		select {
		case ev := <-s.queue:
			s.insert(ev)
		default:
			return
		}
	}
}

func (s *Store) insert(ev Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO requests (
			created_at, key_id, model_requested, provider, model_resolved, stream,
			status_code, error, latency_ms, tokens_in, tokens_out,
			cost_micro_usd, cost_unknown, partial
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`,
		ev.Time, nullIfZero(ev.KeyID), ev.Model, ev.Provider, nullIfEmpty(ev.ModelResolved), ev.Stream,
		ev.StatusCode, nullIfEmpty(ev.Error), ev.LatencyMs, ev.TokensIn, ev.TokensOut,
		ev.CostMicroUSD, ev.CostUnknown, ev.Partial,
	)
	if err != nil {
		slog.Warn("gagal nulis request log ke db", "err", err)
	}
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullIfZero: key_id itu FK nullable — 0 (nggak ada key, mestinya nggak
// kejadian setelah auth) ditulis NULL biar nggak nabrak constraint.
func nullIfZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// RunRetention harus dipanggil di goroutine terpisah. Tiap kali ticker
// interval nembak, hapus baris requests yang lebih tua dari keep. Berhenti
// begitu ctx dibatalin.
func (s *Store) RunRetention(ctx context.Context, interval, keep time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.deleteOlderThan(keep)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Store) deleteOlderThan(keep time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := s.db.ExecContext(ctx, `DELETE FROM requests WHERE created_at < $1`, time.Now().Add(-keep))
	if err != nil {
		slog.Warn("gagal hapus request log lama", "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("retention: hapus request log lama", "rows", n)
	}
}
