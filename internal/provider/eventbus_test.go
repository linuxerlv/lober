package provider

import (
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

func TestEventBus_PublishedEventReachesSubscriber(t *testing.T) {
	bus := NewEventBus()

	var received model.Task
	got := false
	bus.Subscribe(contract.LifecycleEventTaskReady, func(tk model.Task) {
		received = tk
		got = true
	})

	bus.Publish(contract.LifecycleEventTaskReady, model.Task{ID: "task-1"})

	if !got {
		t.Fatal("已订阅的事件发布后订阅者应被调用")
	}
	if received.ID != "task-1" {
		t.Fatalf("got task id %s, want task-1", received.ID)
	}
}

func TestEventBus_SubscriberPanicDoesNotAffectPublisher(t *testing.T) {
	bus := NewEventBus()

	bus.Subscribe(contract.LifecycleEventTaskReady, func(tk model.Task) {
		panic("boom")
	})

	secondCalled := false
	bus.Subscribe(contract.LifecycleEventTaskReady, func(tk model.Task) {
		secondCalled = true
	})

	// Publish 本身不应向调用方传播 panic——若这里真的 panic 了，测试本身
	// 就会以「panic 导致测试进程崩溃」的方式失败，不需要额外的 recover。
	bus.Publish(contract.LifecycleEventTaskReady, model.Task{ID: "task-1"})

	if !secondCalled {
		t.Fatal("第一个订阅者 panic 后，第二个订阅者仍应被正常调用" +
			"（SPEC §7.5：订阅者的 panic 不能带走发布者，也不能连累其它订阅者）")
	}
}

func TestEventBus_MultipleSubscribersAllReceive(t *testing.T) {
	bus := NewEventBus()

	count := 0
	bus.Subscribe(contract.LifecycleEventTaskReady, func(tk model.Task) { count++ })
	bus.Subscribe(contract.LifecycleEventTaskReady, func(tk model.Task) { count++ })
	bus.Subscribe(contract.LifecycleEventTaskReady, func(tk model.Task) { count++ })

	bus.Publish(contract.LifecycleEventTaskReady, model.Task{ID: "task-1"})

	if count != 3 {
		t.Fatalf("三个订阅者都应收到，got count=%d", count)
	}
}

func TestEventBus_OnlySubscribedEventTypeTriggersCallback(t *testing.T) {
	bus := NewEventBus()

	readyCalled := false
	bus.Subscribe(contract.LifecycleEventTaskReady, func(tk model.Task) { readyCalled = true })

	bus.Publish(contract.LifecycleEventTaskCreated, model.Task{ID: "task-1"})

	if readyCalled {
		t.Fatal("发布 task.created 事件不应触发只订阅了 task.ready 的回调")
	}
}
