package pgstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

func TestInventorySerializesWithTaskRetry(t *testing.T) {
	s := pgtest.OpenStore(t)
	f := newFixture(t, s)
	task := f.mustCreate(t, s, mustTaskHosts(t, s, "INV-RETRY")...)[0]
	mustTransition(t, s, task, model.TaskFailed)
	task = mustGetTask(t, s, task.ID)
	retry := func(ctx context.Context) error {
		_, _, err := s.UpdateTaskExecution(ctx, task.ID, repository.TaskExpect{State: task.State, Rev: task.Rev}, model.TaskPending,
			&model.Execution{Attempt: 1, IssuedAt: at0, PhaseAt: at0}, nil, anAudit("task.retry"))
		return err
	}
	_, err := s.UpdateInventory(t.Context(), task.HostSN, func(h model.Host, busy bool) (repository.InventoryChange, error) {
		if busy {
			return repository.InventoryChange{}, errors.New("已失败的任务不应占用主机")
		}
		// 另一连接尝试重开任务。采集拿着主机锁时，重试只能等待。
		ctx, cancel := context.WithTimeout(t.Context(), 350*time.Millisecond)
		defer cancel()
		if err := retry(ctx); !errors.Is(err, context.DeadlineExceeded) {
			return repository.InventoryChange{}, errors.New("重试未等待采集释放主机锁")
		}
		return repository.InventoryChange{Attrs: map[string]string{"inv_request_id": "fixture"}, Audit: anAudit("host.inventory_request")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := retry(t.Context()); err != nil {
		t.Fatal("采集结束后应能重试", err)
	}
	_, err = s.UpdateInventory(t.Context(), task.HostSN, func(h model.Host, busy bool) (repository.InventoryChange, error) {
		if !busy || h.Attrs["inv_request_id"] != "fixture" {
			return repository.InventoryChange{}, errors.New("采集写入或重试结果不可见")
		}
		return repository.InventoryChange{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
