package web

import (
	"testing"
	"time"
)

func fired(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-time.After(20 * time.Millisecond):
		return false
	}
}

func TestPageHubNotifiesOnlyThePageAndCoalesces(t *testing.T) {
	var h PageHub
	a, stopA := h.Subscribe("metrics", "/service")
	b, stopB := h.Subscribe("metrics", "/service")
	other, stopOther := h.Subscribe("metrics", "/")
	elsewhere, stopElsewhere := h.Subscribe("hello", "/service")
	defer stopB()
	defer stopOther()
	defer stopElsewhere()

	h.Notify("metrics", "/service")
	h.Notify("metrics", "/service") // nobody read the first yet: it is still one event
	if !fired(a) || !fired(b) {
		t.Fatal("both viewers of the page are woken")
	}
	if fired(a) {
		t.Error("two events before a read are one")
	}
	if fired(other) || fired(elsewhere) {
		t.Error("another page or another module is not woken")
	}

	stopA()
	h.Notify("metrics", "/service")
	if fired(a) || !fired(b) {
		t.Error("a viewer that left is not woken, the others still are")
	}
	stopB()
	stopOther()
	stopElsewhere()
	if len(h.subs) != 0 {
		t.Errorf("subscriptions leak: %v", h.subs)
	}
	h.Notify("metrics", "/service") // nobody looking is not an error
}
