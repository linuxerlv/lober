package pgstore_test

import (
	"errors"
	"testing"

	"github.com/lober/lober/internal/domain/discovery"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

func TestDiscoveryPersistence(t *testing.T) {
	st := pgtest.OpenStore(t)
	ctx := t.Context()
	mac := "52:54:00:12:34:56"
	a := discovery.Attempt{MAC: mac, SN: "SN-PXE", Arch: "x86_64", Firmware: "uefi", At: 100, ExpiresAt: 200, ID: "first"}
	if _, err := st.ObserveDiscovery(ctx, a); err != nil {
		t.Fatal(err)
	}
	b := a
	b.At = 110
	b.ID = "concurrent"
	if active, err := st.ObserveDiscovery(ctx, b); err != nil || active.ID != a.ID {
		t.Fatalf("应复用等待中的批次: %+v %v", active, err)
	}
	report := discovery.Report{Sections: []discovery.Section{{Key: "storage", Title: "磁盘", Status: "unread", Reason: "缺驱动"}}}
	if err := st.SaveDiscoveryReport(ctx, mac, a.ID, 120, report); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveDiscoveryReport(ctx, mac, a.ID, 125, report); err != nil {
		t.Fatal(err)
	}
	b.At = 130
	b.ExpiresAt = 230
	if _, err := st.ObserveDiscovery(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveDiscoveryReport(ctx, mac, a.ID, 131, report); !errors.Is(err, discovery.ErrToken) {
		t.Fatalf("旧报告不能覆盖: %v", err)
	}
	rows, err := st.ListDiscoveries(ctx, "", "", 10, 235)
	if err != nil || len(rows) != 1 || rows[0].FirstSeenAt != 100 || rows[0].LastSeenAt != 130 || rows[0].ReportAt != 120 || rows[0].Report != nil || rows[0].Status != "timed_out" {
		t.Fatalf("发现与旧报告应保留: %+v %v", rows, err)
	}
	detail, err := st.GetDiscovery(ctx, mac, 235)
	if err != nil || detail.Report == nil || detail.Report.Sections[0].Reason != "缺驱动" {
		t.Fatalf("详情应含报告: %+v %v", detail, err)
	}
	audit := repository.Audit{Actor: "admin", Action: "host.create", At: 240}
	host, err := st.RegisterDiscovery(ctx, mac, "CONFIRMED", audit)
	if err != nil || host.SN != "CONFIRMED" || host.Rev != 1 {
		t.Fatalf("确认登记失败: %+v %v", host, err)
	}
	if _, err = st.RegisterDiscovery(ctx, mac, "DIFFERENT", audit); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("重复登记应冲突: %v", err)
	}
	rows, err = st.ListDiscoveries(ctx, "", "", 10, 240)
	if err != nil || len(rows) != 1 || rows[0].RegisteredSN != "CONFIRMED" {
		t.Fatalf("登记结果应保存: %+v %v", rows, err)
	}
	if observed, selected, err := st.RegisteredDiscoverySN(ctx, mac); err != nil || selected != "CONFIRMED" || observed != "SN-PXE" {
		t.Fatalf("正式登记后的网口应可查到确认的序列号: %q %v", selected, err)
	}
	if report, at, err := st.LatestDiscoveryReport(ctx, host.SN); err != nil || at != 120 || report == nil || report.Sections[0].Reason != "缺驱动" {
		t.Fatalf("最近登记报告 %v %d %v", report, at, err)
	}
	audit.Action = "host.delete"
	if err := st.DeleteHost(ctx, host.SN, host.Rev, audit); err != nil {
		t.Fatal(err)
	}
	if report, at, err := st.LatestDiscoveryReport(ctx, host.SN); err != nil || report != nil || at != 0 {
		t.Fatalf("误返回已解绑报告 %v %d %v", report, at, err)
	}
	if _, selected, err := st.RegisteredDiscoverySN(ctx, mac); err != nil || selected != "" {
		t.Fatalf("删除主机后不能再按网口关联: %q %v", selected, err)
	}

}
