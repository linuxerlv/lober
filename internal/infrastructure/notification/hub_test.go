package notification

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

func Test通知同一订阅并发重复退订(t *testing.T) {
	h := NewHub()
	ch, stop := h.Subscribe("a")
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(stop)
	}
	wg.Wait()
	if _, open := <-ch; open {
		t.Fatal("重复退订后通道仍未关闭")
	}
	raw := h.(*hub)
	raw.mu.Lock()
	defer raw.mu.Unlock()
	if len(raw.subs) != 0 {
		t.Fatal("退订后真实订阅表仍有残留", raw.subs)
	}
}

func Test通知同一订阅退订与发布并发(t *testing.T) {
	h := NewHub()
	for range 100 {
		ch, stop := h.Subscribe("a")
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			for range 100 {
				h.Publish("a")
			}
		})
		wg.Go(func() { <-start; stop(); stop() })
		close(start)
		wg.Wait()
		// 退订可能留下一个已合并的提示，读完后必须观察到关闭。
		for range ch {
		}
	}
	raw := h.(*hub)
	raw.mu.Lock()
	defer raw.mu.Unlock()
	if len(raw.subs) != 0 {
		t.Fatal("并发发布和退订后真实订阅表仍有残留", raw.subs)
	}
}
