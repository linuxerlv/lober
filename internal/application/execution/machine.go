package execution

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"

	"github.com/lober/lober/internal/domain/catalog"
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// Machine 只负责机器协议：引导、取应答、上报信号，以及签发和校验这一面的凭据。
type Machine struct {
	core
	auth *Credentials
}

// Token 原样委托凭据签发，供可信机器入口使用。
func (m *Machine) Token(task model.Task, scope Scope) string { return m.auth.Token(task, scope) }

// Authorize 原样委托凭据校验，供可信机器入口使用。
func (m *Machine) Authorize(task model.Task, token string, scope Scope) error {
	return m.auth.Authorize(task, token, scope)
}

// Credentials 原样返回安装与收尾两项凭据，供网关拼系统值。
func (m *Machine) Credentials(task model.Task) (string, string) { return m.auth.Credentials(task) }

// Boot 的八项应用约定唯一连接 docs/contracts/machine-execution.md「引导和应答」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (m *Machine) Boot(ctx context.Context, sn string, arch catalog.Arch, firmware catalog.Firmware) (model.Task, bool, error) {
	normalized, p := model.NormalizeSN(sn)
	if p != nil {
		return model.Task{}, false, nil
	}
	for range 8 {
		task, found, err := m.repo.FindOpenTask(ctx, normalized)
		if err != nil || !found {
			return task, found, err
		}
		// An installed or running installer must never be booted into installation again.
		if !executionrules.CanBoot(task) {
			return model.Task{}, false, nil
		}
		at := m.clock.Now()
		next, err := executionrules.Boot(task, arch, firmware, at)
		if err != nil {
			return model.Task{}, false, err
		}
		next, err = m.commit(ctx, task, next, repository.NewEvent{At: at, Kind: "machine.boot", Key: attemptKey(next, "boot-"+strconv.Itoa(next.Execution.BootCount)) + "-" + string(next.State), Fields: map[string]string{"arch": string(arch), "firmware": string(firmware)}}, repository.Audit{})
		if errors.Is(err, repository.ErrConflict) {
			return model.Task{}, false, nil
		}
		return next, err == nil && executionrules.BootDeliverable(next), err
	}
	return model.Task{}, false, repository.ErrConflict
}

// Preseed 按 SN 找可以经 DHCP bootfile 地址下发应答的任务：只有 iPXE 已引导过的
// （booting/installing）才给；pending 的机器上还没有安装器在跑，发过去只会提前推
// 状态机，boot 都进不去；已结束的按 FindOpenTask 的口径本来就不算开放任务。
// Debian 的安装器在 auto 模式下会把 DHCP option 67 的 bootfile 地址当 preseed 下载，
// 并覆盖内核命令行上的 preseed/url，所以网关需要按 SN 在这个阶段给出真实应答（D1）。
// Preseed 的八项应用约定唯一连接 docs/contracts/machine-execution.md「引导和应答」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (m *Machine) Preseed(ctx context.Context, sn string) (model.Task, bool, error) {
	normalized, p := model.NormalizeSN(sn)
	if p != nil {
		return model.Task{}, false, nil
	}
	task, found, err := m.repo.FindOpenTask(ctx, normalized)
	if err != nil || !found {
		return model.Task{}, false, err
	}
	if !executionrules.PreseedAvailable(task) {
		return model.Task{}, false, nil
	}
	return task, true, nil
}

// GetAuthorized 的八项应用约定唯一连接 docs/contracts/machine-execution.md「构造、凭据与授权」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (m *Machine) GetAuthorized(ctx context.Context, id, token string, finalize bool) (model.Task, error) {
	task, err := m.repo.GetTask(ctx, id)
	if err != nil {
		return model.Task{}, err
	}
	scope := Install
	if finalize {
		scope = Finalize
	}
	if err := m.auth.Authorize(task, token, scope); err != nil {
		return model.Task{}, err
	}
	return task, nil
}

// Answer 的八项应用约定唯一连接 docs/contracts/machine-execution.md「引导和应答」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (m *Machine) Answer(ctx context.Context, id, token string) (model.Task, error) {
	for range 8 {
		task, err := m.GetAuthorized(ctx, id, token, false)
		if err != nil {
			return model.Task{}, err
		}
		if !executionrules.InstallingNeeded(task) {
			return task, nil
		}
		at := m.clock.Now()
		next, _ := executionrules.StartInstalling(task, at)
		next, err = m.commit(ctx, task, next, repository.NewEvent{At: at, Kind: "machine.installing", Key: attemptKey(next, "installing")}, repository.Audit{})
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		return next, err
	}
	return model.Task{}, repository.ErrConflict
}

// Signal 的八项应用约定唯一连接 docs/contracts/machine-execution.md「信号与超时扫描」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (m *Machine) Signal(ctx context.Context, id, token string, sig Signal) error {
	for range 8 {
		task, err := m.repo.GetTask(ctx, id)
		if err != nil {
			return err
		}
		scope := Install
		if executionrules.FinalSignal(sig) {
			scope = Finalize
		}
		if err := m.auth.authorizeCredential(task, token, scope); err != nil {
			return err
		}
		if !executionrules.SignalAccessAllowed(task, sig) {
			return ErrCredential
		}
		at := m.clock.Now()
		next, duplicate, err := executionrules.Advance(task, sig, at)
		if err != nil || duplicate {
			return err
		}
		event := repository.NewEvent{At: at, Kind: "machine." + string(sig.Phase), Key: attemptKey(next, executionrules.SignalKey(sig)), Fields: map[string]string{"slot": strconv.Itoa(sig.Slot), "timing": string(sig.Timing), "instance": strconv.Itoa(sig.Instance), "digest": sig.Digest, "rc": strconv.Itoa(sig.RC), "log_base64": base64.StdEncoding.EncodeToString(model.SafeLogTail(sig.Body))}}
		// Event kinds use dot identifiers; phases are canonical HTTP hyphen words.
		switch sig.Phase {
		case model.PhaseSlotStart:
			event.Kind = "slot.start"
		case model.PhaseSlotEnd:
			event.Kind = "slot.end"
		case model.PhaseHookStart:
			event.Kind = "hook.start"
		case model.PhaseHookReport:
			event.Kind = "hook.report"
		case model.PhaseInitStart:
			event.Kind = "machine.init_start"
		case model.PhaseInitDone:
			event.Kind = "machine.init_done"
		}
		_, err = m.commit(ctx, task, next, event, repository.Audit{})
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		return err
	}
	return repository.ErrConflict
}
