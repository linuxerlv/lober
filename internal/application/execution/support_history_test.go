package execution_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

func TestHistoricalUnsupportedTaskSurvivesStoreReopenWithoutExecution(t *testing.T) {
	dsn := pgtest.Schema(t)
	st := pgtest.OpenAt(t, dsn)
	task := fixture(t, st)
	// 直接构造旧版本已写入的记录，当前新建入口已经禁止这类系统。
	task.Snapshot.Series = "openeuler"
	raw, err := json.Marshal(task.Snapshot)
	must(t, err)
	conn, err := pgx.Connect(t.Context(), dsn)
	must(t, err)
	defer conn.Close(t.Context())
	_, err = conn.Exec(t.Context(), "UPDATE tasks SET snapshot=$1::jsonb WHERE id=$2", string(raw), task.ID)
	must(t, err)
	must(t, st.Close())
	st = pgtest.OpenAt(t, dsn)
	stored, err := st.GetTask(t.Context(), task.ID)
	must(t, err)
	if !reflect.DeepEqual(stored, task) {
		t.Fatal("旧快照不能原样读取", stored)
	}
	s, err := execution.New(st, []byte(strings.Repeat("k", 32)), func() int64 { return 1100 }, nil)
	must(t, err)
	if _, _, err = s.Boot(t.Context(), task.HostSN, "x86_64", "bios"); !errors.Is(err, repository.ErrRejected) {
		t.Fatal("旧等待任务仍能引导", err)
	}
	if err = execution.CheckUnfinishedTasks(t.Context(), st); err == nil || !strings.Contains(err.Error(), task.ID) {
		t.Fatal("重启未阻止旧任务", err)
	}
	after, err := st.GetTask(t.Context(), task.ID)
	must(t, err)
	if !reflect.DeepEqual(stored, after) {
		t.Fatal("拒绝启动时改写了旧任务", after)
	}
	_, err = conn.Exec(t.Context(), "UPDATE tasks SET state='failed' WHERE id=$1", task.ID)
	must(t, err)
	failed, err := st.GetTask(t.Context(), task.ID)
	must(t, err)
	if err = s.Retry(t.Context(), failed, repository.Audit{Actor: "operator", Action: "task.retry", At: 1100}); !errors.Is(err, repository.ErrRejected) {
		t.Fatal("旧失败任务仍可重试", err)
	}
	if err = execution.CheckUnfinishedTasks(t.Context(), st); err != nil {
		t.Fatal("已结束旧任务阻止了启动", err)
	}
	after, err = st.GetTask(t.Context(), task.ID)
	must(t, err)
	if after.State != model.TaskFailed || !reflect.DeepEqual(failed, after) {
		t.Fatal("拒绝重试时改写了历史任务", after)
	}
}
