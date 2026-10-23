package notification

import (
	"github.com/lober/lober/internal/application/ports"
	"sync"
)

type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

// NewHub 创建进程内的合并提示；任务事实仍以提交后的仓储记录为准。
func NewHub() ports.Notifier { return &hub{subs: make(map[string]map[chan struct{}]struct{})} }
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
