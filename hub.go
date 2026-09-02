package main

import (
	"log/slog"
	"runtime"
	"time"
)

// Event adalah satu baris live feed: ringkasan satu request yang udah kelar
// diproses proxy. Dipakai dua arah — di-broadcast ke Hub (live feed SSE) dan
// di-Enqueue ke Store (persist Postgres) — biar nggak ada dua struct buat
// satu konsep yang sama. Field yang belum keisi (mis. token/biaya sebelum
// usage di-parse) sengaja omitempty, bukan diisi 0 palsu (aturan keras #3).
type Event struct {
	Time          time.Time `json:"time"`
	Model         string    `json:"model"`
	Provider      string    `json:"provider,omitempty"`
	ModelResolved string    `json:"model_resolved,omitempty"`
	Stream        bool      `json:"stream"`
	StatusCode    int       `json:"status_code"`
	LatencyMs     int64     `json:"latency_ms"`
	TokensIn      int       `json:"tokens_in,omitempty"`
	TokensOut     int       `json:"tokens_out,omitempty"`
	CostMicroUSD  int64     `json:"cost_micro_usd,omitempty"`
	CostUnknown   bool      `json:"cost_unknown,omitempty"`
	Partial       bool      `json:"partial,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// Hub itu fan-out SSE: satu goroutine (Run) jadi pemilik tunggal map
// subscribers, jadi nggak butuh mutex buat baca/tulisnya.
type Hub struct {
	register    chan chan Event
	unregister  chan chan Event
	broadcast   chan Event
	subscribers map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{
		register:    make(chan chan Event),
		unregister:  make(chan chan Event),
		broadcast:   make(chan Event, 64),
		subscribers: make(map[chan Event]struct{}),
	}
}

// Run harus dipanggil di goroutine terpisah (go hub.Run()) dan hidup selama
// proses server jalan.
func (h *Hub) Run() {
	for {
		select {
		case ch := <-h.register:
			h.subscribers[ch] = struct{}{}
			slog.Debug("admin stream subscriber nambah", "total", len(h.subscribers), "num_goroutine", runtime.NumGoroutine())

		case ch := <-h.unregister:
			if _, ok := h.subscribers[ch]; ok {
				delete(h.subscribers, ch)
				close(ch)
				slog.Debug("admin stream subscriber ilang", "total", len(h.subscribers), "num_goroutine", runtime.NumGoroutine())
			}

		case ev := <-h.broadcast:
			for ch := range h.subscribers {
				select {
				case ch <- ev:
				default:
					// subscriber lambat/penuh — dilewatin, jangan nge-block hub
				}
			}
		}
	}
}

// Subscribe daftarin channel pelanggan baru dan balikin closure buat
// unsubscribe. Panggil unsubscribe() (biasanya lewat defer) begitu klien
// putus, biar channel & entry di map-nya nggak bocor.
func (h *Hub) Subscribe() (ch chan Event, unsubscribe func()) {
	ch = make(chan Event, 16)
	h.register <- ch
	return ch, func() { h.unregister <- ch }
}

// Broadcast ngirim event ke hub buat di-fan-out. Non-blocking — kalau hub
// lagi sibuk (buffer broadcast penuh), event ini dibuang dan proxy jalan
// terus, sama prinsipnya kayak aturan keras #1 (jalur proxy nggak nunggu),
// cuma target-nya di sini hub, bukan DB.
// ponytail: buffer 64, naikin kalau live feed sering "kelewat baris" pas
// banyak request bareng.
func (h *Hub) Broadcast(ev Event) {
	select {
	case h.broadcast <- ev:
	default:
	}
}
