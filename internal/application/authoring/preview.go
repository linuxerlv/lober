package authoring

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/snapshot"
)

type resolvedPreview struct {
	Input    params.Input
	Firmware catalog.Firmware
	Sample   []string
}

func (s *editor) previewFacts(ctx context.Context, a ports.Actor, req PreviewRequest) (resolvedPreview, error) {
	if err := a.Authorize(access.PermTemplateRead); err != nil {
		return resolvedPreview{}, err
	}
	if err := repository.CheckVersionKey(req.Key.ID, req.Key.Version); err != nil {
		return resolvedPreview{}, err
	}
	if req.Series != "" && !req.Series.Valid() || req.Firmware != "" && !req.Firmware.Valid() {
		return resolvedPreview{}, repository.ErrInvalid
	}
	tpl, err := s.deps.Templates.GetTemplate(ctx, req.Key)
	if err != nil {
		return resolvedPreview{}, err
	}
	hooks, err := s.deps.Hooks.GetHooks(ctx, repository.TemplateHookKeys([]model.Template{tpl}))
	if err != nil {
		return resolvedPreview{}, err
	}
	sample := []string{}
	var item model.ISO
	if req.ISOID != "" {
		item, err = s.deps.ISOs.GetISO(ctx, req.ISOID)
		if err != nil {
			return resolvedPreview{}, err
		}
		if req.Series != "" && req.Series != item.Series {
			return resolvedPreview{}, repository.ErrInvalid
		}
	} else {
		series := req.Series
		if series == "" && len(tpl.Series) > 0 {
			series = tpl.Series[0]
		}
		if series == catalog.SeriesESXi {
			return resolvedPreview{}, &ProblemsError{Problems: []model.Problem{model.NewProblem("iso_version_missing", model.Subject{Kind: model.KindISO, ID: "sample-iso"}, model.ISOVersionLabel)}}
		}
		item = sampleISO(series)
		sample = append(sample, "ISO")
	}
	host := sampleHost()
	if req.HostSN != "" {
		sn, err := model.NormalizeSN(req.HostSN)
		if err != nil {
			return resolvedPreview{}, err
		}
		host, err = s.deps.Hosts.GetHost(ctx, sn)
		if err != nil {
			return resolvedPreview{}, err
		}
	} else {
		sample = append(sample, "主机")
	}
	firmware := req.Firmware
	if firmware == "" {
		firmware = catalog.FirmwareUEFI
		sample = append(sample, "firmware")
	}
	return resolvedPreview{Input: params.Input{Template: tpl, Hooks: hooks, ISO: item, Host: host, Entered: req.Entered}, Firmware: firmware, Sample: sample}, nil
}
func (s *editor) PreviewParams(ctx context.Context, a ports.Actor, req PreviewRequest) (PreviewParamsResult, error) {
	facts, err := s.previewFacts(ctx, a, req)
	if err != nil {
		return PreviewParamsResult{}, err
	}
	result := params.Evaluate(facts.Input)
	return PreviewParamsResult{Fields: result.Fields, Problems: result.Problems, Sample: facts.Sample}, nil
}
func (s *editor) Preview(ctx context.Context, a ports.Actor, req PreviewRequest) (PreviewResult, error) {
	facts, err := s.previewFacts(ctx, a, req)
	if err != nil {
		return PreviewResult{}, err
	}
	frozen, ps := snapshot.Build(facts.Input)
	if len(ps) > 0 {
		return PreviewResult{}, &ProblemsError{Problems: ps}
	}
	files, labels, ps, err := s.deps.Renderer.RenderFrozen(ctx, frozen, facts.Firmware, previewTask)
	if err != nil {
		return PreviewResult{}, err
	}
	if len(ps) > 0 {
		return PreviewResult{}, &ProblemsError{Problems: ps}
	}
	sample := append(facts.Sample, labels...)
	for i := range files {
		files[i].Sample = len(sample) > 0
	}
	return PreviewResult{Files: files, Problems: []model.Problem{}, Sample: sample}, nil
}
func sampleISO(series catalog.Series) model.ISO {
	artifacts := map[string]string{}
	for _, role := range catalog.RequiredRoles(series) {
		artifacts[role] = "sample/" + role
	}
	// 示例也提供正文能引用的制品角色。必需制品仍由原目录规则决定。
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
func sampleHost() model.Host {
	return model.Host{SN: "SAMPLE-SN", Arch: catalog.ArchX8664, Firmware: catalog.FirmwareUEFI, Attrs: map[string]string{}}
}
