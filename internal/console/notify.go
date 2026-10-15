package console

import "sync"

// Notifications are coalesced hints; consumers reread committed events.
type Notifier interface {
	Publish(string)
	Subscribe(string) (<-chan struct{}, func())
}
type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

func NewHub() Notifier { return &hub{subs: make(map[string]map[chan struct{}]struct{})} }
func (h *hub) Publish(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[id] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (h *hub) Subscribe(id string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[id] == nil {
		h.subs[id] = make(map[chan struct{}]struct{})
	}
	h.subs[id][ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			delete(h.subs[id], ch)
			if len(h.subs[id]) == 0 {
				delete(h.subs, id)
			}
			close(ch)
		})
	}
}
