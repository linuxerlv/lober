package console

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/builtin"
)

const inventoryFixtureKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA fixture"

func inventoryTaskBody(iso model.ISO, tpl model.Template, sn, accountUser, sshUser string) string {
	return wizardBody(iso, tpl, fmt.Sprintf(
		`[{"sn":%q,"entered":{"account/1/username":%q,"sshkey/1/username":%q,"account/1/pwhash":"$6$fixture$notARealPasswordHash","sshkey/1/key":%q}}]`,
		sn, accountUser, sshUser, inventoryFixtureKey))
}

func inventoryHost(t *testing.T, st repository.Store, sn, disk string) {
	t.Helper()
	if _, err := st.CreateHost(t.Context(), model.Host{SN: sn, Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{"inv_system_disk": disk}}, repository.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
}

func snapshotDisk(t *testing.T, item taskItem) string {
	t.Helper()
	for _, slot := range item.Snapshot.Slots {
		if slot.Name != "partition" {
			continue
		}
		for _, inst := range slot.Instances {
			if inst.HookID == "builtin-partition" {
				items := inst.Params["disk"].Items
				if len(items) != 1 {
					t.Fatalf("partition.disk = %v", items)
				}
				return items[0]
			}
		}
	}
	t.Fatal("快照里没有 partition 实例")
	return ""
}

func Test采集属性经内置与派生模板进入任务快照(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	admin := adminHTTP(t, s, st)
	reviewer := reviewerHTTP(t, s, st)
	seedBuiltinContent(t, st)
	iso := readyISO(t, st, "Debian", catalog.SeriesDebian)
	// Debian 的 ready ISO 必须有 repo 制品，否则准入会报 iso_artifact_missing。
	iso.Artifacts = map[string]string{"repo": "debian/repo"}
	var err error
	if iso, err = st.UpdateISO(t.Context(), iso); err != nil {
		t.Fatal(err)
	}

	var builtinTpl model.Template
	for _, candidate := range builtin.Templates() {
		if candidate.ID == "builtin-preseed-inventory" {
			builtinTpl = candidate
		}
	}
	if builtinTpl.ID == "" {
		t.Fatal("缺少内置采集模板")
	}

	inventoryHost(t, st, "SN-INV-1", "vda")
	result := responseData[wizardSubmitResult](t, httpCall(s, "POST", "/console/v1/wizard/tasks", inventoryTaskBody(iso, builtinTpl, "SN-INV-1", "lober", "lober"), admin))
	if len(result.Tasks) != 1 {
		t.Fatal(result)
	}
	if got := snapshotDisk(t, result.Tasks[0]); got != "vda" {
		t.Fatalf("内置模板没有使用采集盘：%s", got)
	}
	raw, err := json.Marshal(result.Tasks[0].Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("notARealPasswordHash")) {
		t.Fatal("任务响应泄露了口令散列")
	}

	derived := responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates/"+builtinTpl.ID+"/derive", `{"from":1,"name":"derived-inventory"}`, admin))
	derived = responseData[templateResponse](t, httpCall(s, "POST", lifecyclePath("templates", derived.ID, "submit", derived.Rev), "", admin))
	derived = responseData[templateResponse](t, httpCall(s, "POST", lifecyclePath("templates", derived.ID, "approve", derived.Rev), "", reviewer))
	if derived.State != model.VersionApproved {
		t.Fatal(derived)
	}
	inventoryHost(t, st, "SN-INV-2", "vdb")
	derivedTpl := model.Template{ID: derived.ID, Version: derived.Version}
	result = responseData[wizardSubmitResult](t, httpCall(s, "POST", "/console/v1/wizard/tasks", inventoryTaskBody(iso, derivedTpl, "SN-INV-2", "lober", "lober"), admin))
	if len(result.Tasks) != 1 || result.Tasks[0].TemplateID != derived.ID {
		t.Fatal(result)
	}
	if got := snapshotDisk(t, result.Tasks[0]); got != "vdb" {
		t.Fatalf("派生模板没有使用采集盘：%s", got)
	}

	inventoryHost(t, st, "SN-INV-3", "vdc")
	w := httpCall(s, "POST", "/console/v1/wizard/tasks", inventoryTaskBody(iso, builtinTpl, "SN-INV-3", "alice", "bob"), admin)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "param_hook_mismatch") {
		t.Fatal("用户名不一致没有在提交阶段拦下", w.Code, w.Body.String())
	}
}
