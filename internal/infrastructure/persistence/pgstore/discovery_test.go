package pgstore_test

import (
	"errors"
	"testing"

	"github.com/lober/lober/internal/domain/discovery"
	"github.com/lober/lober/internal/domain/model"
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
	newReport := discovery.Report{Sections: []discovery.Section{{Key: "memory", Title: "内存", Status: "unread", Reason: "未返回插槽"}}}
	host, err = st.UpdateInventory(ctx, host.SN, func(h model.Host, _ bool) (repository.InventoryChange, error) {
		return repository.InventoryChange{Attrs: map[string]string{"rack": "r1"}, Hardware: &newReport,
			Audit: repository.Audit{Actor: "fixture", Action: "host.inventory", At: 241}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved, at, err := st.LatestDiscoveryReport(ctx, host.SN); err != nil || at != 241 || saved.Sections[0].Key != "memory" {
		t.Fatal("应返回主动采集的新报告", at, err)
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

func TestDiscoveryHardwareUpgradeAndRestore(t *testing.T) {
	dsn := pgtest.Schema(t)
	st := pgtest.OpenAt(t, dsn)
	ctx := t.Context()
	h, err := st.CreateHost(ctx, model.Host{SN: "HISTORIC", Attrs: map[string]string{"rack": "R2"}}, anAudit("host.create"))
	if err != nil {
		t.Fatal(err)
	}
	report := discovery.Report{Sections: []discovery.Section{{Key: "power", Status: "unread", Reason: "未提供传感器", Title: "电源"}}}
	_, err = st.UpdateInventory(ctx, h.SN, func(host model.Host, _ bool) (repository.InventoryChange, error) {
		return repository.InventoryChange{Attrs: host.Attrs, Hardware: &report, Audit: anAudit("host.inventory")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	execAll(t, st, "CREATE TABLE saved_report AS SELECT * FROM host_hardware_reports", "DROP TABLE host_hardware_reports")
	reopened := pgtest.OpenAt(t, dsn)
	host, err := reopened.GetHost(ctx, h.SN)
	if err != nil || host.Attrs["rack"] != "R2" {
		t.Fatal("补建报告表后应保留原有主机", err)
	}
	got, _, err := reopened.LatestDiscoveryReport(ctx, h.SN)
	if err != nil || got != nil {
		t.Fatal("旧主机应显示尚无报告", err)
	}
	execAll(t, reopened, "INSERT INTO host_hardware_reports (sn, report, report_at) SELECT sn, report, report_at FROM saved_report")
	got, _, err = reopened.LatestDiscoveryReport(ctx, h.SN)
	if err != nil || got == nil || got.Sections[0].Reason != "未提供传感器" {
		t.Fatal("恢复后应读回原报告", err)
	}
}
