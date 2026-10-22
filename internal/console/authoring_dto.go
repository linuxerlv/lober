package console

import (
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

// Wire DTOs keep core's persisted/golden representation independent of HTTP.
type paramDeclDTO struct {
	Name     string           `json:"name"`
	Label    string           `json:"label"`
	Type     model.ParamType  `json:"type"`
	List     bool             `json:"list"`
	Required bool             `json:"required"`
	Default  string           `json:"default"`
	Options  []optionResponse `json:"options"`
	Min      *int64           `json:"min"`
	Max      *int64           `json:"max"`
	Offer    string           `json:"offer"`
}
type implDTO struct {
	Family      catalog.Family      `json:"family"`
	Series      []catalog.Series    `json:"series"`
	Kind        model.ImplKind      `json:"kind"`
	Body        string              `json:"body"`
	Interpreter catalog.Interpreter `json:"interpreter"`
	Timings     []catalog.Timing    `json:"timings"`
}
type bindingDTO struct {
	Source   model.BindingSource `json:"source"`
	Value    string              `json:"value"`
	Attr     string              `json:"attr"`
	Editable bool                `json:"editable"`
}
type instanceDTO struct {
	HookID      string                `json:"hookID"`
	HookVersion int                   `json:"hookVersion"`
	TimeoutSec  *int                  `json:"timeoutSec"`
	Required    *bool                 `json:"required"`
	Bindings    map[string]bindingDTO `json:"bindings"`
}
type templateKeyResponse struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

func declsFromDTO(in []paramDeclDTO) []model.ParamDecl {
	out := make([]model.ParamDecl, len(in))
	for i, d := range in {
		options := make([]model.Option, len(d.Options))
		for j, o := range d.Options {
			options[j] = model.Option(o)
		}
		out[i] = model.ParamDecl{Name: d.Name, Label: d.Label, Type: d.Type, List: d.List, Required: d.Required, Default: d.Default, Options: options, Min: d.Min, Max: d.Max, Offer: d.Offer}
	}
	return out
}
func declsToDTO(in []model.ParamDecl) []paramDeclDTO {
	out := make([]paramDeclDTO, len(in))
	for i, d := range in {
		options := make([]optionResponse, len(d.Options))
		for j, o := range d.Options {
			options[j] = optionResponse(o)
		}
		out[i] = paramDeclDTO{Name: d.Name, Label: d.Label, Type: d.Type, List: d.List, Required: d.Required, Default: d.Default, Options: options, Min: d.Min, Max: d.Max, Offer: d.Offer}
	}
	return out
}
func implsFromDTO(in []implDTO) []model.Impl {
	out := make([]model.Impl, len(in))
	for i, v := range in {
		out[i] = model.Impl(v)
	}
	return out
}
func implsToDTO(in []model.Impl) []implDTO {
	out := make([]implDTO, len(in))
	for i, v := range in {
		out[i] = implDTO(v)
		out[i].Series = append([]catalog.Series{}, v.Series...)
		out[i].Timings = append([]catalog.Timing{}, v.Timings...)
	}
	return out
}
func bindingsFromDTO(in map[string]bindingDTO) map[string]model.Binding {
	out := make(map[string]model.Binding, len(in))
	for key, v := range in {
		out[key] = model.Binding(v)
	}
	return out
}
func bindingsToDTO(in map[string]model.Binding) map[string]bindingDTO {
	out := make(map[string]bindingDTO, len(in))
	for key, v := range in {
		out[key] = bindingDTO(v)
	}
	return out
}
func assemblyFromDTO(in map[string][]instanceDTO) map[string][]model.Instance {
	out := make(map[string][]model.Instance, len(in))
	for name, instances := range in {
		items := make([]model.Instance, len(instances))
		for i, v := range instances {
			items[i] = model.Instance{HookID: v.HookID, HookVersion: v.HookVersion, TimeoutSec: v.TimeoutSec, Required: v.Required, Bindings: bindingsFromDTO(v.Bindings)}
		}
		out[name] = items
	}
	return out
}
func assemblyToDTO(in map[string][]model.Instance) map[string][]instanceDTO {
	out := make(map[string][]instanceDTO, len(in))
	for name, instances := range in {
		items := make([]instanceDTO, len(instances))
		for i, v := range instances {
			items[i] = instanceDTO{HookID: v.HookID, HookVersion: v.HookVersion, TimeoutSec: v.TimeoutSec, Required: v.Required, Bindings: bindingsToDTO(v.Bindings)}
		}
		out[name] = items
	}
	return out
}
