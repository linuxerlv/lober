package provider

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

func TestNotify_EmitAndPingDoNotError(t *testing.T) {
	n := NewBuiltinNotify()
	err := n.Emit(context.Background(), contract.LifecyclePayload{
		Event:  contract.LifecycleEventTaskReady,
		TaskID: "task-1",
		Detail: map[string]string{"state": "ready"},
	})
	if err != nil {
		t.Fatalf("内置 Notify Emit 不应返回错误: %v", err)
	}
	if err := n.Ping(context.Background()); err != nil {
		t.Fatalf("内置 Notify Ping 不应返回错误: %v", err)
	}
}
