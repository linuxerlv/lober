package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	data "github.com/lober/lober/internal/domain/discovery"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type diskFixture struct {
	hosts, reports     []string
	hostErr, reportErr error
}

func (s *diskFixture) GetHost(_ context.Context, sn string) (model.Host, error) {
	s.hosts = append(s.hosts, sn)
	return model.Host{SN: sn, Attrs: map[string]string{"inv_disks": "vda:100:SSD"}}, s.hostErr
}
func (s *diskFixture) LatestDiscoveryReport(_ context.Context, sn string) (*data.Report, int64, error) {
	s.reports = append(s.reports, sn)
	return nil, 0, s.reportErr
}

func TestDiskReader(t *testing.T) {
	ctx := t.Context()
	st := new(diskFixture)
	r := NewDiskReader(st)
	viewer := ports.Actor{Username: "viewer", Role: model.RoleViewer}
	if _, err := r.Disks(ctx, ports.Actor{}, ""); !errors.Is(err, ports.ErrUnauthenticated) || len(st.hosts) != 0 {
		t.Fatal("应先检查身份", err)
	}
	if _, err := r.Disks(ctx, viewer, "/"); err == nil || len(st.hosts) != 0 {
		t.Fatal("无效主机序列号", err)
	}
	st.hostErr = repository.ErrNotFound
	if _, err := r.Disks(ctx, viewer, "sn-1"); !errors.Is(err, repository.ErrNotFound) || len(st.reports) != 0 {
		t.Fatal("主机不存在", err)
	}
	st.hostErr = nil
	st.reportErr = errors.New("报告读取失败")
	if _, err := r.Disks(ctx, viewer, "sn-1"); !errors.Is(err, st.reportErr) {
		t.Fatal("应允许客户端识别读取失败", err)
	}
	st.reportErr = nil
	got, err := r.Disks(ctx, viewer, "sn-1")
	if err != nil || got.Status != "partial" || len(got.Disks) != 1 || got.Disks[0].Device != "vda" {
		t.Fatal("重试应返回采集数据", got, err)
	}
	if len(st.hosts) != 3 || len(st.reports) != 2 {
		t.Fatalf("查询次数 %v %v", st.hosts, st.reports)
	}
	for _, sn := range append(st.hosts, st.reports...) {
		if sn != "SN-1" {
			t.Fatal("查询了其他主机", sn)
		}
	}
}
