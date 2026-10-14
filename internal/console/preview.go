package console

import (
	"context"
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/params"
	"github.com/lober/lober/internal/core/snapshot"
	"github.com/lober/lober/internal/render"
)

// BootConfig is supplied by assembly and reads immutable original ESXi media
// config. Console never substitutes a made-up config or opens arbitrary paths.
type BootConfig interface {
	ReadESXiBootConfig(context.Context, model.SnapshotISO) (string, error)
}
type PreviewEndpoints struct{ Gateway, Artifacts, SMB, SyslogAddr string }
type previewRequest struct {
	HostSN   string            `json:"hostSN,omitempty"`
	ISOID    string            `json:"isoID,omitempty"`
	Series   catalog.Series    `json:"series,omitempty"`
	Firmware catalog.Firmware  `json:"firmware,omitempty"`
	Entered  map[string]string `json:"entered,omitempty"`
}
type previewFile struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Sample bool   `json:"sample"`
	Body   string `json:"body"`
	Slot   string `json:"slot,omitempty"`
	Digest string `json:"digest,omitempty"`
}
type previewResponse struct {
	Files    []previewFile `json:"files"`
	Problems []problemJSON `json:"problems"`
	Sample   []string      `json:"sample"`
}

const previewToken = "sample-token"
const previewFinalizeToken = "sample-finalize-token"
const previewTask = "sample-task"

func (s *server) previewTemplate(c *gin.Context) {
	k, ok := templateKeyOf(c)
	if !ok {
		return
	}
	var req previewRequest
	if !Bind(c, &req) {
		return
	}
	if req.Series != "" && !req.Series.Valid() || req.Firmware != "" && !req.Firmware.Valid() {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	tpl, ok := Get(c, func() (model.Template, error) { return s.deps.Templates.GetTemplate(c.Request.Context(), k) })
	if !ok {
		return
	}
	hooks, ok := Get(c, func() (map[model.HookKey]model.Hook, error) {
		return s.templateHooks(c.Request.Context(), []model.Template{tpl})
	})
	if !ok {
		return
	}
	sample := []string{}
	var item model.ISO
	if req.ISOID != "" {
		item, ok = Get(c, func() (model.ISO, error) { return s.deps.ISOs.GetISO(c.Request.Context(), req.ISOID) })
		if !ok {
			return
		}
		if req.Series != "" && req.Series != item.Series {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
	} else {
		series := req.Series
		if series == "" && len(tpl.Series) > 0 {
			series = tpl.Series[0]
		}
		if series == catalog.SeriesESXi {
			Judge(c, []model.Problem{model.NewProblem("iso_version_missing", model.Subject{Kind: model.KindISO, ID: "sample-iso"}, model.ISOVersionLabel)})
			return
		}
		item = SampleISO(series)
		sample = append(sample, "ISO")
	}
	host := SampleHost()
	if req.HostSN != "" {
		sn, ok := normalizedHostSN(c, req.HostSN)
		if !ok {
			return
		}
		host, ok = Get(c, func() (model.Host, error) { return s.deps.Hosts.GetHost(c.Request.Context(), sn) })
		if !ok {
			return
		}
	} else {
		sample = append(sample, "主机")
	}
	firmware := req.Firmware
	if firmware == "" {
		firmware = catalog.FirmwareUEFI
		sample = append(sample, "firmware")
	}
	frozen, problems := snapshot.Build(params.Input{Template: tpl, Hooks: hooks, ISO: item, Host: host, Entered: enteredOf(req.Entered)})
	if !Judge(c, problems) {
		return
	}
	files, labels, problems, err := s.renderPreview(c.Request.Context(), frozen, firmware, previewTask)
	if err != nil {
		RespondError(c, err)
		return
	}
	if !Judge(c, problems) {
		return
	}
	sample = append(sample, labels...)
	for i := range files {
		files[i].Sample = len(sample) > 0
	}
	Ok(c, previewResponse{Files: files, Problems: []problemJSON{}, Sample: sample})
}
func enteredOf(values map[string]string) map[params.FieldKey]string {
	out := make(map[params.FieldKey]string, len(values))
	for key, value := range values {
		out[params.FieldKey(key)] = value
	}
	return out
}

// Rendering consumes frozen data and shared filenames. All errors discard the
// accumulated files; a client never sees a partially valid machine package.
func (s *server) renderPreview(ctx context.Context, frozen model.Snapshot, firmware catalog.Firmware, taskID string) ([]previewFile, []string, []model.Problem, error) {
	values, sample := s.previewValues(frozen.Host.SN, taskID)
	original := ""
	if frozen.Format == catalog.FormatESXiKS {
		if s.deps.BootConfig == nil {
			return nil, nil, nil, errors.New("ESXi original boot config reader missing")
		}
		var err error
		original, err = s.deps.BootConfig.ReadESXiBootConfig(ctx, frozen.ISO)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	files := make([]previewFile, 0, 4)
	request := render.Request{Snapshot: frozen, Firmware: firmware, Values: values, ESXiBootConfig: original}
	appendRender := func(kind catalog.FileKind, name, slot string) ([]model.Problem, error) {
		request.Kind, request.Slot = kind, slot
		bytes, problems, err := render.Render(request)
		if err != nil || len(problems) > 0 {
			return problems, err
		}
		files = append(files, previewFile{Kind: string(kind), Name: name, Slot: slot, Body: string(bytes)})
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
			files = append(files, previewFile{Kind: "hook", Name: name, Slot: slot.Name, Digest: instance.Digest, Body: instance.Body})
		}
	}
	return files, sample, nil, nil
}
func (s *server) previewValues(sn, taskID string) (map[catalog.SystemValue]string, []string) {
	values := map[catalog.SystemValue]string{catalog.SysSN: sn, catalog.SysTask: taskID, catalog.SysToken: previewToken, catalog.SysFinalizeToken: previewFinalizeToken}
	sample := []string{"token", "finalize-token"}
	if taskID == previewTask {
		sample = append(sample, "task")
	}
	endpoints := s.deps.PreviewEndpoints
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
func SampleISO(series catalog.Series) model.ISO {
	artifacts := map[string]string{}
	for _, role := range catalog.RequiredRoles(series) {
		artifacts[role] = "sample/" + role
	}
	// repo is addressable by {{sys:repo}} even for media whose mandatory boot
	// roles omit it (for example Debian netboot). Include catalog system-value
	// artifact roles used by that format, not a second list of media requirements.
	for _, value := range series.Format().SystemValues() {
		if value == catalog.SysRepo || value == catalog.SysISO {
			artifacts[string(value)] = "sample/" + string(value)
		}
	}
	offers := map[string][]model.Option{}
	switch series.Format() {
	case catalog.FormatAgama:
		offers["products"] = []model.Option{{Value: "ExampleProduct", Label: "示例产品"}}
	case catalog.FormatUnattend:
		offers["editions"] = []model.Option{{Value: "ExampleEdition", Label: "示例版本"}}
	}
	return model.ISO{ID: "sample-iso", Name: model.ConsoleSampleISO, Family: series.Family(), Series: series, Version: "0.0", Arch: catalog.ArchX8664, State: model.ISOReady, Artifacts: artifacts, Offers: offers}
}
func SampleHost() model.Host {
	return model.Host{SN: "SAMPLE-SN", Arch: catalog.ArchX8664, Firmware: catalog.FirmwareUEFI, Attrs: map[string]string{}}
}
