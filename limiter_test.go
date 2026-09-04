package main

import (
	"sync"
	"testing"
)

func TestKeyLimiters_AllowPerKey(t *testing.T) {
	l := newKeyLimiters()

	// rpm=2, burst=2: dua request pertama lolos, ketiga (dalam window yang
	// sama, tanpa nunggu refill) ketolak.
	for i := range 2 {
		if !l.allow(1, 2) {
			t.Fatalf("request key 1 ke-%d harusnya lolos", i+1)
		}
	}
	if l.allow(1, 2) {
		t.Error("request ke-3 key 1 harusnya ketolak")
	}

	// key lain punya bucket sendiri — nggak kena imbas key 1.
	if !l.allow(2, 2) {
		t.Error("key 2 harusnya lolos, bucket-nya kepisah")
	}

	// rpm <= 0 = tanpa batas.
	for range 100 {
		if !l.allow(3, 0) {
			t.Fatal("rpm=0 harusnya selalu lolos")
		}
	}
}

func TestKeyLimiters_ConcurrentAllow(t *testing.T) {
	l := newKeyLimiters()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			for range 20 {
				l.allow(id%5, 10) // 5 key beda, diadu barengan — cek race
			}
		}(int64(i))
	}
	wg.Wait()
}
