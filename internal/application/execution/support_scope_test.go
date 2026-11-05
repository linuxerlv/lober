package execution

import (
	"errors"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"strings"
	"testing"
)

func TestHistoricalUnsupportedTaskCannotExecute(t *testing.T) {
	for _, series := range []catalog.Series{"sles15", "leap15", "sles16", "leap16", "openeuler", "opencloudos", "removed"} {
		for _, op := range []string{"boot", "preseed", "answer", "file", "signal", "retry", "sweep"} {
			t.Run(string(series)+"/"+op, func(t *testing.T) {
				task := credentialTask()
				task.HostSN = "OLD-HOST"
				format := catalog.FormatKickstart
				switch series {
				case "sles15", "leap15":
					format = "autoyast"
				case "sles16", "leap16":
					format = "agama"
				}
				task.Snapshot = model.Snapshot{Family: "linux", Series: series, Format: format, ISO: model.SnapshotISO{Arch: "x86_64"}}
				task.Execution.Deadline = 1900
				if op == "sweep" {
					task.Execution.Deadline = 1001
				}
				if op == "boot" {
					task.State = model.TaskPending
					task.Execution = nil
				}
				if op == "retry" {
					task.State = model.TaskFailed
				}
				r := &clockRepo{task: task, pages: []repository.Page[model.Task]{{Items: []model.Task{task}}}}
				s, err := New(r, []byte(strings.Repeat("k", 32)), func() int64 { return 1100 }, nil)
				if err != nil {
					t.Fatal(err)
				}
				switch op {
				case "boot":
					_, _, err = s.Boot(t.Context(), task.HostSN, "x86_64", "bios")
				case "preseed":
					_, _, err = s.Preseed(t.Context(), task.HostSN)
				case "answer":
					_, err = s.Answer(t.Context(), task.ID, s.Token(task, Install))
				case "file":
					_, err = s.GetAuthorized(t.Context(), task.ID, s.Token(task, Install), false)
				case "signal":
					err = s.Signal(t.Context(), task.ID, s.Token(task, Install), Signal{Phase: "installed"})
				case "retry":
					err = s.Retry(t.Context(), task, repository.Audit{Actor: "operator", Action: "task.retry", At: 1100})
				case "sweep":
					err = s.Sweep(t.Context())
				}
				if !errors.Is(err, repository.ErrRejected) || !strings.Contains(err.Error(), string(series)) {
					t.Fatalf("不支持的旧任务应明确拒绝，得到 %v", err)
				}
				if r.writes != 0 {
					t.Fatalf("拒绝前改写了旧任务：%d", r.writes)
				}
				if op == "retry" && len(s.CanRetry(task)) == 0 {
					t.Fatal("旧任务仍显示可以重试")
				}
			})
		}
	}
}

func TestSupportedSystemsKeepMachineFileAccess(t *testing.T) {
	for _, series := range []catalog.Series{"rhel", "almalinux", "rocky", "ubuntu", "debian", "windows-server", "windows-desktop", "esxi"} {
		t.Run(string(series), func(t *testing.T) {
			task := credentialTask()
			task.Snapshot = model.Snapshot{Series: series, Family: series.Family(), Format: series.Format()}
			r := &clockRepo{task: task}
			s, err := New(r, []byte(strings.Repeat("k", 32)), func() int64 { return 1100 }, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.GetAuthorized(t.Context(), task.ID, s.Token(task, Install), false); err != nil {
				t.Fatal("保留系统的文件访问被拒绝", err)
			}
			if r.writes != 0 {
				t.Fatal("只读取文件时改写任务")
			}
		})
	}
}
func TestSupportedSeriesCannotUseRetiredInstallFormat(t *testing.T) {
	for _, format := range []catalog.Format{"autoyast", "agama", "removed"} {
		t.Run(string(format), func(t *testing.T) {
			task := credentialTask()
			task.Snapshot.Format = format
			r := &clockRepo{task: task}
			s, err := New(r, []byte(strings.Repeat("k", 32)), func() int64 { return 1100 }, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.GetAuthorized(t.Context(), task.ID, s.Token(task, Install), false); !errors.Is(err, repository.ErrRejected) {
				t.Fatal("只换系列就绕过了已停用格式限制", err)
			}
		})
	}
}
