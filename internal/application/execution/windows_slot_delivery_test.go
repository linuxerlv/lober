package execution_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/model"
)

func TestWindowsSlotDownloadFromGeneratedStartup(t *testing.T) {
	f, _ := gatewayFixture(t)
	f.task.State = model.TaskInstalling
	s := &f.task.Snapshot
	s.Family, s.Series, s.Format = "windows", "windows-server", "unattend"
	s.ISO.Artifacts = map[string]string{
		"wimboot": "win/wimboot", "bcd": "win/bcd", "boot-sdi": "win/boot.sdi",
		"boot-wim": "win/boot.wim", "winpeshl": "win/winpeshl.ini",
		"winpe-busybox": "win/busybox.exe", "winpe-curl": "win/curl.exe",
	}
	s.Slots = []model.SnapshotSlot{
		{Seq: 1, Name: "pre", Exec: true, Timing: "pre"},
		{Seq: 2, Name: "init", Exec: true, Timing: "firstboot"},
		{Seq: 3, Name: "disabled"},
	}
	files := authoring.NewMachineRenderer(nil)
	h := f.handler(t, port.RenderFunc(func(ctx context.Context, r port.RenderRequest) ([]byte, error) {
		return files.Render(ctx, authoring.MachineRenderRequest{Task: r.Task, Kind: r.Kind, Slot: r.Slot, Values: r.Values})
	}), nil)
	prefix := "/v1/answer/task-one/" + f.install + "/"
	startup := call(h, http.MethodGet, prefix+"startnet.cmd", "")
	if startup.Code != http.StatusOK {
		t.Fatalf("下载启动文件得到 %d：%s", startup.Code, startup.Body)
	}
	var encoded strings.Builder
	chunks := regexp.MustCompile(`(?m)^>{1,2} "%LOBER_DIR%/startup\.b64" echo ([A-Za-z0-9+/=]+)\r?$`)
	for _, match := range chunks.FindAllStringSubmatch(startup.Body.String(), -1) {
		encoded.WriteString(match[1])
	}
	shell, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil || len(shell) == 0 {
		t.Fatalf("启动文件中的命令无法解码：%v", err)
	}
	downloads := regexp.MustCompile(`'http://gateway(/v1/answer/[^']+/scripts/2\.[^']+)'`)
	match := downloads.FindSubmatch(shell)
	if match == nil {
		t.Fatal("启动文件未给第二个插槽写下载地址")
	}
	path := string(match[1])
	script := call(h, http.MethodGet, path, "")
	if script.Code != http.StatusOK {
		t.Fatalf("按启动文件中的路径下载脚本得到 %d，预期 200", script.Code)
	}
	if path != prefix+"scripts/2.cmd" ||
		!strings.Contains(script.Body.String(), `"%LOBER_DIR%/runtime-2.rc"`) ||
		!strings.Contains(script.Body.String(), `set "LOBER_DIR=%SystemDrive%/ProgramData/lober"`) {
		t.Fatal("启动文件未下载第二个插槽的 Windows 首启脚本")
	}
	legacy := call(h, http.MethodGet, prefix+"scripts/2.sh", "")
	if legacy.Code != http.StatusOK || legacy.Body.String() != script.Body.String() {
		t.Fatalf("旧地址得到 %d，或正文和新地址不同", legacy.Code)
	}
	for _, name := range []string{"scripts/2.cmd", "scripts/2.sh"} {
		if w := call(h, http.MethodGet, "/v1/answer/task-one/"+f.final+"/"+name, ""); w.Code != http.StatusForbidden {
			t.Errorf("用首启凭据下载 %s 得到 %d，预期 403", name, w.Code)
		}
	}
	for _, name := range []string{"scripts/2.ps1", "scripts/3.cmd", "scripts/3.sh", "scripts/99.cmd", "scripts/99.sh"} {
		if w := call(h, http.MethodGet, prefix+name, ""); w.Code != http.StatusNotFound {
			t.Errorf("下载 %s 得到 %d，预期 404", name, w.Code)
		}
	}
}
