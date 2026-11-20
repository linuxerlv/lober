package model

import "testing"

// 这些词会进 HTTP 路径、事件键与数据库，发布后不能改名。
func Test机器信号与执行阶段词表固定(t *testing.T) {
	phases := []struct {
		got  SignalPhase
		want string
	}{
		{PhaseBooted, "booted"},
		{PhaseInstalled, "installed"},
		{PhaseInitStart, "init-start"},
		{PhaseInitDone, "init-done"},
		{PhaseReady, "ready"},
		{PhaseSlotStart, "slot-start"},
		{PhaseSlotEnd, "slot-end"},
		{PhaseHookStart, "hook-start"},
		{PhaseHookReport, "hook-report"},
	}
	seen := map[string]bool{}
	for _, tc := range phases {
		if string(tc.got) != tc.want {
			t.Fatalf("信号阶段 %q 应为 %q", tc.got, tc.want)
		}
		if seen[tc.want] {
			t.Fatalf("信号阶段重复：%q", tc.want)
		}
		seen[tc.want] = true
	}

	stages := []struct {
		got  Stage
		want string
	}{
		{StageBooting, "booting"},
		{StagePre, "pre"},
		{StagePackages, "packages"},
		{StagePost, "post"},
		{StageRebooting, "rebooting"},
		{StageInitializing, "initializing"},
	}
	seen = map[string]bool{}
	for _, tc := range stages {
		if string(tc.got) != tc.want {
			t.Fatalf("执行阶段 %q 应为 %q", tc.got, tc.want)
		}
		if seen[tc.want] {
			t.Fatalf("执行阶段重复：%q", tc.want)
		}
		seen[tc.want] = true
	}
}
