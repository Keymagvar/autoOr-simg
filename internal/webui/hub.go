package webui

import "sync"

// Hub fans out broadcast events to every connected client — DESIGN.md §3's
// "pub/sub broadcaster that fans out state changes to every connected
// client." Each client owns a buffered channel; a slow/dead client is
// dropped rather than blocking every other client's broadcast.
type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newHub() *Hub {
	return &Hub{clients: map[chan []byte]struct{}{}}
}

func (h *Hub) subscribe() chan []byte {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *Hub) broadcast(e event) {
	msg := e.marshal()
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
			// Client is behind (its own goroutine isn't draining fast
			// enough, or is already gone) — drop the message rather than
			// block every other client's broadcast on one slow reader.
			// The next snapshot (reload, or a later full state push)
			// catches it back up; incremental events are a stream, not a
			// durable log.
		}
	}
}
