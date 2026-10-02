package model

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

// 状态的字面量会存进数据库，发布后不能改名。
func TestTaskState的字面量(t *testing.T) {
	got := []TaskState{TaskPending, TaskBooting, TaskInstalling, TaskRebooting, TaskInitializing, TaskDone, TaskFailed, TaskCancelled}
	want := []TaskState{"pending", "booting", "installing", "rebooting", "initializing", "done", "failed", "cancelled"}
	if !slices.Equal(got, want) {
		t.Errorf("任务状态的字面量是 %q，应为 %q", got, want)
	}
}

// 状态会进数据库：第 2 层建 CHECK 约束、第 7 层推进状态之前，都只调 Valid，不另抄取值表（T7-2）。
func TestTaskStateValid只认八个状态(t *testing.T) {
	for _, s := range []TaskState{TaskPending, TaskBooting, TaskInstalling, TaskRebooting, TaskInitializing, TaskDone, TaskFailed, TaskCancelled} {
		if !s.Valid() {
			t.Errorf("%q.Valid() 应为 true", s)
		}
	}
	for _, s := range []TaskState{"", "Done", "canceled", "running", "pending "} {
		if s.Valid() {
			t.Errorf("%q.Valid() 应为 false：只认原样的写法", s)
		}
	}
}

// 只有 done、failed、cancelled 算结束：一台主机同一时刻最多有一个未结束的任务（C31），结束了
// 才能再建。不认识的写法算未结束，宁可挡住这台主机，也不让它同时有两个任务在装。
func TestTaskStateFinished只认三个结束状态(t *testing.T) {
	cases := map[TaskState]bool{
		"pending": false, "booting": false, "installing": false, "rebooting": false, "initializing": false,
		"done": true, "failed": true, "cancelled": true,
		"": false, "Done": false, "canceled": false,
	}
	for s, want := range cases {
		if got := s.Finished(); got != want {
			t.Errorf("%q.Finished() = %v，应为 %v", s, got, want)
		}
	}
}

// 第 2 层按 JSON 存任务和它的快照：每个字段都得导出、能原样存取，快照里不能藏着 Value 这类
// 内部结构。字段名照计划写全，改名在这里就编译不过。
func TestTask连同快照经JSON存取后不变(t *testing.T) {
	script := "#!/bin/sh\necho ok\n"
	task := Task{
		ID: "t-0001", HostSN: "SN-0001", ISOID: "rocky-9.6", TemplateID: "baseline", TemplateVersion: 3,
		State: TaskPending, CreatedBy: "alice", CreatedAt: 1790000000,
		Snapshot: Snapshot{
			Schema: SnapshotSchema, Family: "linux", Series: "rocky", Format: "kickstart",
			Host: SnapshotHost{SN: "SN-0001", Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{"disk": "/dev/sda"}},
			ISO: SnapshotISO{ID: "rocky-9.6", Name: "Rocky Linux 9.6", Version: "9.6", Arch: "x86_64",
				Artifacts: map[string]string{"kernel": "rocky/9.6/x86_64/images/pxeboot/vmlinuz"}},
			Template: SnapshotTemplate{ID: "baseline", Version: 3, Name: "Linux 基线", Body: "text\n{{slot:装后配置@post-target}}\n",
				Params: map[string]SnapshotValue{"hostname": {Type: "hostname", Items: []string{"web-01"}}}},
			Slots: []SnapshotSlot{{Seq: 1, Name: "装后配置", Exec: true, Timing: "post-target", Instances: []SnapshotInstance{{
				HookID: "dns", HookVersion: 2, HookName: "DNS", Kind: ImplScript, Body: script,
				Interpreter: "sh", TimeoutSec: 300, Required: true, Digest: Impl{Body: script}.Digest(),
				Params: map[string]SnapshotValue{"servers": {Type: "ip", List: true, Items: []string{"10.0.0.53", "10.0.1.53"}}},
			}}}},
		},
	}
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("JSON 编码失败：%v", err)
	}
	var back Task
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("JSON 解码失败：%v", err)
	}
	if !reflect.DeepEqual(back, task) {
		t.Errorf("经 JSON 存取后变了：\n%+v\n应为\n%+v", back, task)
	}
}
