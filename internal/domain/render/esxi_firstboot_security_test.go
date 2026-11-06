package render

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
)

// ESXi 保留首次启动脚本，不能把凭据或可还原的钩子正文留在其中。
func TestESXiNativeFirstbootDoesNotPersistPrivatePayload(t *testing.T) {
	hookBody := "printf 'private-hook-fixture-71f2'\n"
	r := runtimeFixture("firstboot", runtimeInstance(hookBody, true, "sh", 2))
	r.Snapshot.Format, r.Snapshot.Family, r.Snapshot.Series = "esxi-ks", "esxi", "esxi"
	r.Kind = catalog.FileAnswer
	r.Values["finalize-token"] = "private-finalize-fixture-71f2"
	r.Snapshot.Template.Body = "vmaccepteula\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
	b, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("render failed: %v %v", problems, err)
	}
	_, native, ok := strings.Cut(string(b), "%firstboot --interpreter=busybox\n")
	if !ok {
		t.Fatal("missing native firstboot section")
	}
	decoded := native + "\n" + strings.Join(commandPayloads(native), "\n")
	for _, secret := range []string{r.Values["finalize-token"], hookBody, base64.StdEncoding.EncodeToString([]byte(hookBody))} {
		if strings.Contains(decoded, secret) || strings.Contains(native, base64.StdEncoding.EncodeToString([]byte(secret))) {
			t.Fatal("native firstboot persists private payload")
		}
	}
}

// 安装前下载、安装后保存，并在首次启动前校验同一份冻结载荷。
func TestESXiAnswerStagesVerifiedPayloadBeforeFirstboot(t *testing.T) {
	r := runtimeFixture("firstboot", runtimeInstance("printf 'stage fixture'\n", true, "sh", 2))
	r.Snapshot.Format, r.Snapshot.Family, r.Snapshot.Series = "esxi-ks", "esxi", "esxi"
	r.Values["finalize-token"] = "private-finalize-fixture-319c"
	r.Snapshot.Template.Body = "vmaccepteula\nreboot\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
	r.Kind = catalog.FileESXiStage
	stage, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("render stage failed: %v %v", problems, err)
	}
	r.Kind = catalog.FileAnswer
	answer, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("render answer failed: %v %v", problems, err)
	}
	text := string(answer)
	pre, post, firstboot := strings.Index(text, "%pre --interpreter=busybox\n"), strings.Index(text, "%post --interpreter=busybox\n"), strings.Index(text, "%firstboot --interpreter=busybox\n")
	if pre < 0 || post <= pre || firstboot <= post {
		t.Fatal("installer staging is missing or in the wrong section")
	}
	digest := sha256.Sum256(stage)
	if !strings.Contains(text, fmt.Sprintf("%x", digest)) {
		t.Fatal("installer does not verify the frozen stage")
	}
	if !strings.Contains(text[pre:post], "esxi-firstboot.sh") || strings.Contains(text[firstboot:], r.Values["token"]) {
		t.Fatal("stage download is not confined to the installer")
	}
}

// 受保护载荷仍须包含冻结的钩子与收尾凭据，不能只把它们从原生脚本中删掉。
func TestESXiPrivateStageContainsFrozenFirstbootData(t *testing.T) {
	hookBody := "printf 'private-hook-fixture-89ae'\n"
	r := runtimeFixture("firstboot", runtimeInstance(hookBody, true, "sh", 2))
	r.Snapshot.Format, r.Snapshot.Family, r.Snapshot.Series = "esxi-ks", "esxi", "esxi"
	r.Kind = catalog.FileESXiStage
	r.Values["finalize-token"] = "private-finalize-fixture-89ae"
	r.Snapshot.Template.Body = "vmaccepteula\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
	b, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("render private stage failed: %v %v", problems, err)
	}
	decoded := string(b) + "\n" + strings.Join(commandPayloads(string(b)), "\n")
	if !strings.Contains(decoded, r.Values["finalize-token"]) || !strings.Contains(decoded, base64.StdEncoding.EncodeToString([]byte(hookBody))) {
		t.Fatal("private stage is missing the frozen credential or hook")
	}
	if strings.Contains(decoded, r.Values["token"]) {
		t.Fatal("private firstboot stage must not retain the installation credential")
	}
}

func TestESXiRejectsMissingOrMultipleNativeFirstbootSections(t *testing.T) {
	for name, body := range map[string]string{
		"missing":  "vmaccepteula\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n",
		"multiple": "vmaccepteula\n%firstboot --interpreter=busybox\n{{lober:firstboot}}\n%firstboot --interpreter=busybox\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			r := runtimeFixture("firstboot", runtimeInstance("exit 0\n", true, "sh", 2))
			r.Snapshot.Format, r.Snapshot.Family, r.Snapshot.Series = "esxi-ks", "esxi", "esxi"
			r.Kind = catalog.FileAnswer
			r.Values["finalize-token"] = "private-finalize-fixture"
			r.Snapshot.Template.Body = body
			b, problems, err := Render(r)
			if err != nil || len(problems) == 0 || len(b) != 0 {
				t.Fatal("invalid native firstboot sections were accepted")
			}
		})
	}
}
