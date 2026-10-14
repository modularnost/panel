package web

import "sync"

type pageKey struct{ module, path string }

// PageHub tells the browsers that look at a ui page that the module changed it
// (ADR 8). An event only says "refetch", so a subscriber that has not read the
// last one yet needs no second: the channel holds one and the rest coalesce.
// The zero value is ready to use.
type PageHub struct {
	mu   sync.Mutex
	subs map[pageKey]map[chan struct{}]struct{}
}

// Subscribe returns a channel that fires when the page changes, and a function
// that ends the subscription.
func (h *PageHub) Subscribe(module, path string) (<-chan struct{}, func()) {
	key, ch := pageKey{module, path}, make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[pageKey]map[chan struct{}]struct{}{}
	}
	if h.subs[key] == nil {
		h.subs[key] = map[chan struct{}]struct{}{}
	}
	h.subs[key][ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[key], ch)
		if len(h.subs[key]) == 0 {
			delete(h.subs, key)
		}
	}
}

// Notify wakes everyone looking at the page; nobody looking costs nothing.
func (h *PageHub) Notify(module, path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[pageKey{module, path}] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
