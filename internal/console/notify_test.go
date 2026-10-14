package console

import (
	"sync"
	"testing"
)

func Test通知按任务隔离合并并可退订(t *testing.T) {
	h := NewHub()
	a, stopA := h.Subscribe("a")
	b, stopB := h.Subscribe("b")
	defer stopB()
	for range 100 {
		h.Publish("a")
	}
	select {
	case <-a:
	default:
		t.Fatal("missing hint")
	}
	select {
	case <-a:
		t.Fatal("not coalesced")
	default:
	}
	select {
	case <-b:
		t.Fatal("cross-task hint")
	default:
	}
	stopA()
	stopA()
	h.Publish("a")
	if _, ok := <-a; ok {
		t.Fatal("subscription still open")
	}
	h.Publish("b")
	select {
	case <-b:
	default:
		t.Fatal("other subscription lost")
	}
}
func Test通知并发发布退订不竞态(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 100 {
				_, stop := h.Subscribe("a")
				h.Publish("a")
				stop()
			}
		})
	}
	wg.Wait()
}
