package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

var ErrProtocol = errors.New("machine protocol rejected")

type Signal struct {
	Phase    string
	Slot     int
	Timing   catalog.Timing
	Instance int
	Digest   string
	RC       int
	Body     []byte
}

func clone(task model.Task) model.Task {
	if task.Execution != nil {
		r := *task.Execution
		r.Signals = maps.Clone(r.Signals)
		task.Execution = &r
	}
	return task
}

// InstallLife 是一次安装尝试从开始到结束允许的最长时间，4 小时。安装器下载软件包要经过公网，
// 网络不稳时几十分钟都可能，旧值 90 分钟会把正常装机误判成超时；超时后任务变成失败，
// 安装器最后一步取首启脚本又被拒，整台机器白装。
const InstallLife = 14400

// 各阶段自己的时长预算；pre、packages、post 三个安装阶段的实际期限还要受 InstallLife 封顶，
// 所以「总时长不超过 4 小时」这一条只写在 stage 里。
const (
	bootBudget       = 900         // 从下发任务到安装器真正开始装
	preBudget        = 600         // 装前脚本
	packagesBudget   = InstallLife // 装软件包是整个装机最慢的一步，直接给满
	postBudget       = 1200        // 装后脚本
	rebootBudget     = 480         // 装完重启进系统
	initializeBudget = 900         // 首启初始化
)

func runtime(at int64, attempt int) *model.Execution {
	return &model.Execution{Attempt: attempt, IssuedAt: at, PhaseAt: at, Deadline: at + bootBudget, Stage: "booting", Signals: map[string]string{}}
}
func stage(r *model.Execution, name string, at, budget int64) {
	r.Stage = name
	r.PhaseAt = at
	r.Deadline = at + budget
	if name == "pre" || name == "packages" || name == "post" {
		r.Deadline = min(r.Deadline, r.IssuedAt+InstallLife)
	}
}
func preComplete(task model.Task) bool {
	for _, slot := range task.Snapshot.Slots {
		if slot.Exec && slot.Timing == catalog.TimingPre && !seen(task.Execution, "slot-end", slot.Seq, 0) {
			return false
		}
	}
	return true
}

// Boot 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func Boot(task model.Task, arch catalog.Arch, firmware catalog.Firmware, at int64) (model.Task, error) {
	if !arch.Valid() || !firmware.Valid() || !CanBoot(task) {
		return task, ErrProtocol
	}
	task = clone(task)
	if task.Execution == nil {
		task.Execution = runtime(at, 1)
	}
	r := task.Execution
	if task.State == model.TaskPending && r.BootCount == 0 {
		task.Execution = runtime(at, r.Attempt)
		r = task.Execution
	}
	if r.ReachedReboot {
		return task, ErrProtocol
	}
	archOK, fwOK := catalog.PlatformOK(task.Snapshot.Series, task.Snapshot.ISO.Arch, arch, firmware)
	if !archOK || !fwOK || r.BootCount >= 3 || (task.State == model.TaskBooting && r.Deadline > 0 && at >= r.Deadline) {
		task.State = model.TaskFailed
		r.Deadline = 0
		return task, nil
	}
	r.BootCount++
	r.Firmware = firmware
	task.State = model.TaskBooting
	return task, nil
}

// SignalKey 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func SignalKey(sig Signal) string { return fmt.Sprintf("%s-%d-%d", sig.Phase, sig.Slot, sig.Instance) }
func fingerprint(sig Signal) string {
	b, _ := json.Marshal(sig)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func seen(r *model.Execution, phase string, slot, instance int) bool {
	return r.Signals[SignalKey(Signal{Phase: phase, Slot: slot, Instance: instance})] != ""
}
func slotsComplete(task model.Task, firstboot bool) bool {
	for _, slot := range task.Snapshot.Slots {
		if !slot.Exec || (slot.Timing == catalog.TimingFirstboot) != firstboot {
			continue
		}
		if !seen(task.Execution, "slot-end", slot.Seq, 0) {
			return false
		}
	}
	return true
}

// Advance 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
// 拒绝不改传入任务；错误返回的副本可能已做局部计算，调用方不得保存。
func Advance(task model.Task, sig Signal, at int64) (model.Task, bool, error) {
	if task.Execution == nil || len(sig.Body) > 4096 || at < task.Execution.IssuedAt {
		return task, false, ErrProtocol
	}
	task = clone(task)
	r := task.Execution
	key, value := SignalKey(sig), fingerprint(sig)
	if old := r.Signals[key]; old != "" {
		// Installer reports cannot be used to re-enter an earlier phase after reboot.
		if sig.Slot != 0 && sig.Timing != catalog.TimingFirstboot && r.ReachedReboot {
			return task, false, ErrProtocol
		}
		if old == value {
			return task, true, nil
		}
		return task, false, ErrProtocol
	}
	if task.State.Finished() || len(r.Signals) >= 16384 {
		return task, false, ErrProtocol
	}
	if r.Deadline > 0 && at >= r.Deadline {
		return task, false, ErrProtocol
	}
	if sig.Slot != 0 {
		var slot *model.SnapshotSlot
		for i := range task.Snapshot.Slots {
			if task.Snapshot.Slots[i].Seq == sig.Slot {
				slot = &task.Snapshot.Slots[i]
				break
			}
		}
		if slot == nil || !slot.Exec || slot.Timing != sig.Timing {
			return task, false, ErrProtocol
		}
		if sig.Timing == catalog.TimingFirstboot {
			if task.State != model.TaskInitializing || !seen(r, "init-start", 0, 0) {
				return task, false, ErrProtocol
			}
		} else if task.State != model.TaskInstalling || r.ReachedReboot {
			return task, false, ErrProtocol
		}
		switch sig.Phase {
		case "slot-start":
			if sig.Instance != 0 || sig.Digest != "" || sig.RC != 0 {
				return task, false, ErrProtocol
			}
			if sig.Timing == catalog.TimingPre && r.TimingRank == 0 {
				r.TimingRank = 1
				stage(r, "pre", at, preBudget)
			}
			if (sig.Timing == catalog.TimingPostInstaller || sig.Timing == catalog.TimingPostTarget) && r.TimingRank < 2 {
				r.TimingRank = 2
				stage(r, "post", at, postBudget)
			}
		case "hook-start", "hook-report":
			if !seen(r, "slot-start", sig.Slot, 0) || seen(r, "slot-end", sig.Slot, 0) || sig.Instance < 1 || sig.Instance > len(slot.Instances) {
				return task, false, ErrProtocol
			}
			hook := slot.Instances[sig.Instance-1]
			if sig.Digest != hook.Digest {
				return task, false, ErrProtocol
			}
			if sig.Phase == "hook-start" {
				if sig.RC != 0 || (sig.Instance > 1 && !seen(r, "hook-report", sig.Slot, sig.Instance-1)) {
					return task, false, ErrProtocol
				}
			} else {
				if !seen(r, "hook-start", sig.Slot, sig.Instance) {
					return task, false, ErrProtocol
				}
				if sig.RC != 0 && hook.Required {
					task.State = model.TaskFailed
					r.Deadline = 0
				}
			}
		case "slot-end":
			if sig.Instance != 0 || sig.Digest != "" || !seen(r, "slot-start", sig.Slot, 0) {
				return task, false, ErrProtocol
			}
			for i := range slot.Instances {
				if !seen(r, "hook-report", sig.Slot, i+1) {
					return task, false, ErrProtocol
				}
			}
			if sig.RC != 0 {
				task.State = model.TaskFailed
				r.Deadline = 0
			}
		default:
			return task, false, ErrProtocol
		}
	} else {
		if sig.Timing != "" || sig.Instance != 0 || sig.Digest != "" || sig.RC != 0 || len(sig.Body) != 0 {
			return task, false, ErrProtocol
		}
		switch sig.Phase {
		case "installed":
			if task.State != model.TaskInstalling || !slotsComplete(task, false) {
				return task, false, ErrProtocol
			}
			task.State = model.TaskRebooting
			r.ReachedReboot = true
			stage(r, "rebooting", at, rebootBudget)
		case "booted":
			if task.State != model.TaskRebooting && task.State != model.TaskInstalling {
				return task, false, ErrProtocol
			}
			if !slotsComplete(task, false) {
				return task, false, ErrProtocol
			}
			task.State = model.TaskInitializing
			r.ReachedReboot = true
			r.TimingRank = 3
			stage(r, "initializing", at, initializeBudget)
		case "init-start":
			if task.State != model.TaskInitializing {
				return task, false, ErrProtocol
			}
		case "init-done":
			if task.State != model.TaskInitializing || !seen(r, "init-start", 0, 0) || !slotsComplete(task, true) {
				return task, false, ErrProtocol
			}
			r.InitDone = true
		case "ready":
			if task.State != model.TaskInitializing || !r.InitDone || !seen(r, "init-done", 0, 0) || !slotsComplete(task, true) {
				return task, false, ErrProtocol
			}
			task.State = model.TaskDone
			r.Deadline = 0
		default:
			return task, false, ErrProtocol
		}
	}
	if r.Signals == nil {
		r.Signals = map[string]string{}
	}
	r.Signals[key] = value
	if sig.Phase == "slot-end" && sig.Timing == catalog.TimingPre && r.Stage == "pre" && preComplete(task) {
		stage(r, "packages", at, packagesBudget)
	}
	return task, false, nil
}
