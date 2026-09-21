package model

import "testing"

// TestAnswerFileServable 锁住应答文件可服务状态集合恰好是
// {booting, installing}（SPEC §4.4）。这个集合被网关与控制台共同引用，
// 错误地扩大它会让终态任务仍能取到应答文件，错误地缩小则会掐死正常重试
// （installer 可能重复取应答文件，例如 cloud-init 取 user-data 与 meta-data
// 两个文件）。
func TestAnswerFileServable(t *testing.T) {
	servable := []TaskState{TaskStateBooting, TaskStateInstalling}
	for _, s := range servable {
		if !AnswerFileServable(s) {
			t.Errorf("AnswerFileServable(%q) = false, want true —— "+
				"SPEC §4.4 明确应答文件在 booting/installing 状态下必须可服务，"+
				"否则 installer 的正常重试会被掐死", s)
		}
	}

	notServable := []TaskState{
		TaskStatePending, TaskStateRebooting, TaskStateInitializing,
		TaskStateDone, TaskStateFailed,
	}
	for _, s := range notServable {
		if AnswerFileServable(s) {
			t.Errorf("AnswerFileServable(%q) = true, want false —— "+
				"该状态不在 SPEC §4.4 的可服务集合内，错误放开会扩大 token 的暴露面",
				s)
		}
	}
}
