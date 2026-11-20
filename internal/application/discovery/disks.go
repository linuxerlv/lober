package discovery

import (
	"context"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	data "github.com/lober/lober/internal/domain/discovery"
	"github.com/lober/lober/internal/domain/model"
)

// DiskReader 也服务于关闭机器引导功能的控制台。
type DiskReader interface {
	Disks(context.Context, ports.Actor, string) (data.HostDiskReport, error)
	Hardware(context.Context, ports.Actor, string) (HardwareSnapshot, error)
}

type HardwareSnapshot struct {
	Report   *data.Report `json:"report,omitempty"`
	ReportAt int64        `json:"report_at"`
}

func (r *diskReader) Hardware(ctx context.Context, actor ports.Actor, rawSN string) (HardwareSnapshot, error) {
	if err := actor.Authorize(access.PermHostRead); err != nil {
		return HardwareSnapshot{}, err
	}
	sn, err := model.NormalizeSN(rawSN)
	if err != nil {
		return HardwareSnapshot{}, err
	}
	if _, err := r.store.GetHost(ctx, sn); err != nil {
		return HardwareSnapshot{}, err
	}
	report, at, err := r.store.LatestDiscoveryReport(ctx, sn)
	return HardwareSnapshot{Report: report, ReportAt: at}, err
}

type diskStore interface {
	GetHost(context.Context, string) (model.Host, error)
	LatestDiscoveryReport(context.Context, string) (*data.Report, int64, error)
}

type diskReader struct{ store diskStore }

func NewDiskReader(store diskStore) DiskReader { return &diskReader{store} }

func (r *diskReader) Disks(ctx context.Context, actor ports.Actor, rawSN string) (data.HostDiskReport, error) {
	if err := actor.Authorize(access.PermHostRead); err != nil {
		return data.HostDiskReport{}, err
	}
	sn, err := model.NormalizeSN(rawSN)
	if err != nil {
		return data.HostDiskReport{}, err
	}
	host, err := r.store.GetHost(ctx, sn)
	if err != nil {
		return data.HostDiskReport{}, err
	}
	report, at, err := r.store.LatestDiscoveryReport(ctx, sn)
	if err != nil {
		return data.HostDiskReport{}, err
	}
	return data.HostDisks(host.Attrs, report, at), nil
}
