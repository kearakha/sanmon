package main

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// budgetTracker nyimpen pengeluaran bulan berjalan per key di memori, biar
// cek budget di jalur proxy nggak usah query DB (aturan keras #1). Di-seed
// sekali pas startup dari SUM(cost_micro_usd) bulan ini, ditambah tiap request
// kelar diproses (lewat emitEvent), di-nol-in pas ganti bulan.
//
// ponytail: cek "kepakai < budget" nggak atomik sama penambahannya — sekelompok
// request barengan bisa lolos bareng lalu total-nya nembus budget dikit.
// Cukup buat cap lunak: request BERIKUTNYA setelah nembus langsung ketolak.
// Juga: kalau event-nya kebuang gara-gara antrian Store penuh, biayanya nggak
// keitung — sama tenangnya kayak log yang boleh hilang.
type budgetTracker struct {
	db *sql.DB

	mu    sync.Mutex
	month time.Month
	spent map[int64]*atomic.Int64 // key_id -> micro-USD kepakai bulan ini
}

func newBudgetTracker(db *sql.DB) *budgetTracker {
	return &budgetTracker{
		db:    db,
		month: time.Now().UTC().Month(),
		spent: make(map[int64]*atomic.Int64),
	}
}

// seed muat pengeluaran bulan berjalan dari DB. Dipanggil sekali pas startup,
// sebelum server nerima trafik.
func (b *budgetTracker) seed(ctx context.Context) error {
	rows, err := b.db.QueryContext(ctx, `
		SELECT key_id, COALESCE(SUM(cost_micro_usd), 0)
		FROM requests
		WHERE key_id IS NOT NULL AND created_at >= date_trunc('month', now())
		GROUP BY key_id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	b.mu.Lock()
	defer b.mu.Unlock()
	for rows.Next() {
		var keyID, sum int64
		if err := rows.Scan(&keyID, &sum); err != nil {
			return err
		}
		v := &atomic.Int64{}
		v.Store(sum)
		b.spent[keyID] = v
	}
	return rows.Err()
}

func (b *budgetTracker) counter(keyID int64) *atomic.Int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.spent[keyID]
	if !ok {
		v = &atomic.Int64{}
		b.spent[keyID] = v
	}
	return v
}

// allow: budget <= 0 = tanpa batas. Selain itu tolak kalau yang udah kepakai
// bulan ini udah nyampe/lewat budget.
func (b *budgetTracker) allow(keyID, budgetMicroUSD int64) bool {
	if budgetMicroUSD <= 0 {
		return true
	}
	return b.counter(keyID).Load() < budgetMicroUSD
}

// add nambahin biaya satu request yang baru kelar. Dipanggil dari emitEvent.
func (b *budgetTracker) add(keyID, costMicroUSD int64) {
	if keyID == 0 || costMicroUSD == 0 {
		return
	}
	b.counter(keyID).Add(costMicroUSD)
}

// maybeRollMonth: kalau bulan sekarang beda dari yang lagi dilacak, nol-in
// semua counter dan pindah ke bulan baru.
func (b *budgetTracker) maybeRollMonth(now time.Month) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if now != b.month {
		b.month = now
		b.spent = make(map[int64]*atomic.Int64)
		slog.Info("budget tracker: bulan ganti, counter di-reset")
	}
}

// RunMonthlyReset ngecek tiap jam apakah bulan udah ganti. Berhenti pas ctx
// dibatalin.
func (b *budgetTracker) RunMonthlyReset(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			b.maybeRollMonth(time.Now().UTC().Month())
		case <-ctx.Done():
			return
		}
	}
}
