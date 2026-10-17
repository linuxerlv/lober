package console

import (
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"maps"
)

type snapshotValueDTO struct {
	Type  model.ParamType `json:"type"`
	List  bool            `json:"list"`
	Items []string        `json:"items"`
}
type snapshotHostDTO struct {
	SN       string            `json:"sn"`
	Arch     catalog.Arch      `json:"arch" openapi:"allowEmpty"`
	Firmware catalog.Firmware  `json:"firmware" openapi:"allowEmpty"`
	Attrs    map[string]string `json:"attrs"`
}
type snapshotISODTO struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Arch      catalog.Arch      `json:"arch"`
	Artifacts map[string]string `json:"artifacts"`
}
type snapshotTemplateDTO struct {
	ID      string                      `json:"id"`
	Version int                         `json:"version"`
	Name    string                      `json:"name"`
	Body    string                      `json:"body"`
	Params  map[string]snapshotValueDTO `json:"params"`
}
type snapshotInstanceDTO struct {
	HookID      string                      `json:"hookID"`
	HookVersion int                         `json:"hookVersion"`
	HookName    string                      `json:"hookName"`
	Kind        model.ImplKind              `json:"kind"`
	Body        string                      `json:"body"`
	Interpreter catalog.Interpreter         `json:"interpreter"`
	TimeoutSec  int                         `json:"timeoutSec"`
	Required    bool                        `json:"required"`
	Digest      string                      `json:"digest"`
	Params      map[string]snapshotValueDTO `json:"params"`
}
type snapshotSlotDTO struct {
	Seq       int                   `json:"seq"`
	Name      string                `json:"name"`
	Exec      bool                  `json:"exec"`
	Timing    catalog.Timing        `json:"timing"`
	Instances []snapshotInstanceDTO `json:"instances"`
}
type snapshotDTO struct {
	Schema   int                 `json:"schema"`
	Family   catalog.Family      `json:"family"`
	Series   catalog.Series      `json:"series"`
	Format   catalog.Format      `json:"format"`
	Host     snapshotHostDTO     `json:"host"`
	ISO      snapshotISODTO      `json:"iso"`
	Template snapshotTemplateDTO `json:"template"`
	Slots    []snapshotSlotDTO   `json:"slots"`
}
type taskItem struct {
	ID              string          `json:"id"`
	HostSN          string          `json:"hostSN"`
	ISOID           string          `json:"isoID"`
	TemplateID      string          `json:"templateID"`
	TemplateVersion int             `json:"templateVersion"`
	State           model.TaskState `json:"state"`
	Snapshot        snapshotDTO     `json:"snapshot"`
	CreatedBy       string          `json:"createdBy"`
	CreatedAt       int64           `json:"createdAt"`
	Rev             int64           `json:"rev"`
	CanCancel       []problemJSON   `json:"canCancel"`
	CanRetry        []problemJSON   `json:"canRetry"`
}

func snapshotValues(in map[string]model.SnapshotValue) map[string]snapshotValueDTO {
	out := make(map[string]snapshotValueDTO, len(in))
	for key, v := range in {
		out[key] = snapshotValueDTO{v.Type, v.List, append([]string{}, v.Items...)}
	}
	return out
}
func stringMap(in map[string]string) map[string]string {
	out := maps.Clone(in)
	if out == nil {
		out = map[string]string{}
	}
	return out
}
func snapshotToDTO(s model.Snapshot) snapshotDTO {
	slots := make([]snapshotSlotDTO, len(s.Slots))
	for i, slot := range s.Slots {
		instances := make([]snapshotInstanceDTO, len(slot.Instances))
		for j, v := range slot.Instances {
			instances[j] = snapshotInstanceDTO{v.HookID, v.HookVersion, v.HookName, v.Kind, v.Body, v.Interpreter, v.TimeoutSec, v.Required, v.Digest, snapshotValues(v.Params)}
		}
		slots[i] = snapshotSlotDTO{slot.Seq, slot.Name, slot.Exec, slot.Timing, instances}
	}
	return snapshotDTO{Schema: s.Schema, Family: s.Family, Series: s.Series, Format: s.Format, Host: snapshotHostDTO{s.Host.SN, s.Host.Arch, s.Host.Firmware, stringMap(s.Host.Attrs)}, ISO: snapshotISODTO{s.ISO.ID, s.ISO.Name, s.ISO.Version, s.ISO.Arch, stringMap(s.ISO.Artifacts)}, Template: snapshotTemplateDTO{s.Template.ID, s.Template.Version, s.Template.Name, s.Template.Body, snapshotValues(s.Template.Params)}, Slots: slots}
}
func toTaskItem(t model.Task) taskItem {
	return taskItem{t.ID, t.HostSN, t.ISOID, t.TemplateID, t.TemplateVersion, t.State, snapshotToDTO(t.Snapshot), t.CreatedBy, t.CreatedAt, t.Rev, []problemJSON{}, []problemJSON{}}
}
