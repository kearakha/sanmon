package main

import (
	"testing"
	"time"
)

func TestHub_BroadcastFanOutToAllSubscribers(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	ch1, unsub1 := hub.Subscribe()
	defer unsub1()
	ch2, unsub2 := hub.Subscribe()
	defer unsub2()

	ev := Event{Model: "gemini-flash", StatusCode: 200}
	hub.Broadcast(ev)

	for i, ch := range []chan Event{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Model != ev.Model {
				t.Errorf("subscriber %d: model = %q, want %q", i, got.Model, ev.Model)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d nggak nerima event", i)
		}
	}
}

func TestHub_UnsubscribeClosesChannel(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	ch, unsubscribe := hub.Subscribe()
	unsubscribe()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel harusnya udah ketutup, malah masih ngirim data")
		}
	case <-time.After(time.Second):
		t.Fatal("channel nggak ketutup setelah unsubscribe")
	}
}

func TestHub_BroadcastNonBlockingWithoutSubscribers(t *testing.T) {
	hub := NewHub()
	// hub.Run() sengaja nggak dipanggil — Broadcast harus tetep nggak nge-hang
	// walau nggak ada yang baca broadcast channel.

	done := make(chan struct{})
	go func() {
		for range 200 {
			hub.Broadcast(Event{Model: "gemini-flash"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Broadcast blocking padahal harusnya non-blocking")
	}
}
