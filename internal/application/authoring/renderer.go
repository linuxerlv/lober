package authoring

import (
	"context"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/authoring/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/render"
)

const previewToken = "sample-token"
const previewFinalizeToken = "sample-finalize-token"
const previewTask = "sample-task"

type previewRenderer struct {
	bootConfig port.BootConfig
	endpoints  ports.PreviewEndpoints
}

func NewPreviewRenderer(reader port.BootConfig, endpoints ports.PreviewEndpoints) ports.FrozenPreview {
	return &previewRenderer{bootConfig: reader, endpoints: endpoints}
}

// 生成失败时丢弃所有已生成文件，避免返回半截安装包。
func (s *previewRenderer) RenderFrozen(ctx context.Context, frozen model.Snapshot, firmware catalog.Firmware, taskID string) ([]ports.PreviewFile, []string, []model.Problem, error) {
	values, sample := s.previewValues(frozen.Host.SN, taskID)
	original := ""
	if frozen.Format == catalog.FormatESXiKS {
		if s.bootConfig == nil {
			return nil, nil, nil, errors.New("ESXi original boot config reader missing")
		}
		var err error
		original, err = s.bootConfig.ReadESXiBootConfig(ctx, frozen.ISO)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	files := make([]ports.PreviewFile, 0, 4)
	request := render.Request{Snapshot: frozen, Firmware: firmware, Values: values, ESXiBootConfig: original}
	appendRender := func(kind catalog.FileKind, name, slot string) ([]model.Problem, error) {
		request.Kind, request.Slot = kind, slot
		bytes, problems, err := render.Render(request)
		if err != nil || len(problems) > 0 {
			return problems, err
		}
		files = append(files, ports.PreviewFile{Kind: string(kind), Name: name, Slot: slot, Body: string(bytes)})
		return nil, nil
	}
	for _, kind := range catalog.DeliveryFiles(frozen.Format) {
		switch kind {
		case catalog.FileMenu:
			if ps, err := appendRender(kind, "menu.ipxe", ""); err != nil || len(ps) > 0 {
				return nil, nil, ps, err
			}
		case catalog.FileScript:
			for _, slot := range frozen.Slots {
				if slot.Exec {
					if ps, err := appendRender(kind, render.RuntimeFileName(frozen.Format, slot), slot.Name); err != nil || len(ps) > 0 {
						return nil, nil, ps, err
					}
				}
			}
		default:
			name, ok := catalog.DeliveryFileName(frozen.Format, kind)
			if !ok {
				return nil, nil, nil, fmt.Errorf("missing delivery filename for %s/%s", frozen.Format, kind)
			}
			if ps, err := appendRender(kind, name, ""); err != nil || len(ps) > 0 {
				return nil, nil, ps, err
			}
		}
	}
	seen := map[string]bool{}
	for _, slot := range frozen.Slots {
		if !slot.Exec {
			continue
		}
		for _, instance := range slot.Instances {
			if instance.Kind != model.ImplScript {
				continue
			}
			name := render.HookFileName(instance)
			if seen[name] {
				continue
			}
			seen[name] = true
			files = append(files, ports.PreviewFile{Kind: "hook", Name: name, Slot: slot.Name, Digest: instance.Digest, Body: instance.Body})
		}
	}
	return files, sample, nil, nil
}
func (s *previewRenderer) previewValues(sn, taskID string) (map[catalog.SystemValue]string, []string) {
	values := map[catalog.SystemValue]string{catalog.SysSN: sn, catalog.SysTask: taskID, catalog.SysToken: previewToken, catalog.SysFinalizeToken: previewFinalizeToken}
	sample := []string{"token", "finalize-token"}
	if taskID == previewTask {
		sample = append(sample, "task")
	}
	endpoints := s.endpoints
	for _, item := range []struct {
		key             catalog.SystemValue
		value, fallback string
	}{
		{catalog.SysGateway, endpoints.Gateway, "https://gateway.example.invalid"},
		{catalog.SysArtifacts, endpoints.Artifacts, "https://artifacts.example.invalid"},
		{catalog.SysSMB, endpoints.SMB, `\\smb.example.invalid\lober`},
		{catalog.SysSyslogAddr, endpoints.SyslogAddr, "udp://syslog.example.invalid:514"},
	} {
		value := item.value
		if value == "" {
			value = item.fallback
			sample = append(sample, string(item.key))
		}
		values[item.key] = value
	}
	return values, sample
}
