package main

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// keyLimiters nyimpen satu token-bucket per key, dibikin lazy pas key itu
// pertama kali kepakai. rpm_limit sebuah key immutable (nggak ada PUT di
// /admin/keys — ganti limit = hapus lalu bikin ulang), jadi entry di sini
// nggak akan pernah basi.
//
// ponytail: map unbounded, sekali dibikin nggak dihapus. Satu user, segelintir
// key — tambahin eviction (TTL / LRU) kalau jumlah key udah ratusan.
type keyLimiters struct {
	mu sync.Mutex
	m  map[int64]*rate.Limiter
}

func newKeyLimiters() *keyLimiters {
	return &keyLimiters{m: make(map[int64]*rate.Limiter)}
}

// allow balik true kalau request buat keyID ini masih dalam jatah rpm.
// rpm <= 0 = tanpa batas → selalu true.
func (l *keyLimiters) allow(keyID int64, rpm int) bool {
	if rpm <= 0 {
		return true
	}
	l.mu.Lock()
	lim, ok := l.m[keyID]
	if !ok {
		// rate.Every(menit/rpm) = jeda antar token; burst rpm biar ledakan
		// pendek dalam satu menit masih kelayan.
		lim = rate.NewLimiter(rate.Every(time.Minute/time.Duration(rpm)), rpm)
		l.m[keyID] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}
