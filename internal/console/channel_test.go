// 本文件测试模板层的通道选择与建任务时的通道校验（P3-03 任务 5）。
package console

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// seedChannelFixtures 建一台主机、两个制品集（两条通道各一个）与两个模板。
// 返回主机 id 与两个模板 id。
func seedChannelFixtures(t *testing.T, ts *testServer) (hostSN, installerTplID, imageTplID string) {
	t.Helper()
	ctx := context.Background()

	host, err := ts.Store.UpsertHost(ctx, model.Host{
		SN: "SN-CHAN", MAC: "aa:bb:cc:dd:ee:10",
		Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI,
	})
	if err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}

	// 两个制品集：同一 OS 版本、两条通道。
	if _, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "art-chan-installer", Name: "rocky9.4-installer",
		OSFamily: model.OSFamilyRHEL, OSVersion: "9.4", Arch: model.ArchX86_64,
		Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
	}); err != nil {
		t.Fatalf("CreateArtifactSet(installer) 失败: %v", err)
	}
	if _, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "art-chan-image", Name: "rocky9.4-image",
		OSFamily: model.OSFamilyRHEL, OSVersion: "9.4", Arch: model.ArchX86_64,
		Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
		Channel: model.ChannelImageWrite,
		Image: &model.ImageArtifact{
			ImagePath: "image.raw.zst", SHA256: "aa", UncompressedSHA256: "bb",
			PartitionLayout: model.PartitionLayout{
				TableType:  "gpt",
				Partitions: []model.Partition{{Number: 2, Filesystem: "xfs", Role: model.PartitionRoleRoot}},
			},
		},
	}); err != nil {
		t.Fatalf("CreateArtifactSet(image-write) 失败: %v", err)
	}

	for _, spec := range []struct {
		id      string
		artID   string
		channel model.DeliveryChannel
	}{
		{"tpl-chan-installer", "art-chan-installer", model.ChannelInstaller},
		{"tpl-chan-image", "art-chan-image", model.ChannelImageWrite},
	} {
		if _, err := ts.Store.CreateTemplate(ctx, model.Template{
			ID: spec.id, Name: spec.id, ArtifactSetID: spec.artID,
			AnswerTemplate: "ks", DiskPolicy: model.DiskPolicyLargest,
			OnFailure: model.OnFailureHalt, Version: 1,
			State: model.TemplateStateActive, Channel: spec.channel,
		}); err != nil {
			t.Fatalf("CreateTemplate(%s) 失败: %v", spec.id, err)
		}
	}

	return host.SN, "tpl-chan-installer", "tpl-chan-image"
}

// TestTemplateChannel_DefaultsToInstaller 验证模板不指定通道时缺省为
// installer 驱动。
//
// **默认值的选择是 installer 驱动**（P3-03 任务 5）：它是兜底通道，对新 OS
// 版本与特殊机型总是可用；而镜像直写要求那个版本已经封装过。默认选一个可能
// 没有制品的通道会让建任务失败，而运维不一定知道为什么。
func TestTemplateChannel_DefaultsToInstaller(t *testing.T) {
	ts := newTestServer(t)
	seedChannelFixtures(t, ts) // 建模板要求 artifactSetId 真实存在
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name":          "无通道字段的模板",
		"artifactSetId": "art-chan-installer",
		"diskPolicy":    "largest",
		"onFailure":     "halt",
		// channel 刻意不传。
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建模板状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &resp)
	if resp.Data.Channel != string(model.ChannelInstaller) {
		t.Errorf("未指定通道时 Channel = %q, want %q —— 默认必须是兜底通道；"+
			"默认选镜像直写会让「那个版本还没封装镜像」的建任务失败，而运维不知道为什么",
			resp.Data.Channel, model.ChannelInstaller)
	}
}

// TestTemplateChannel_RejectsInvalidValue 验证非法通道取值被拒绝。
func TestTemplateChannel_RejectsInvalidValue(t *testing.T) {
	ts := newTestServer(t)
	seedChannelFixtures(t, ts)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name":          "通道拼错的模板",
		"artifactSetId": "art-chan-installer",
		"diskPolicy":    "largest",
		"onFailure":     "halt",
		"channel":       "image",
	})
	if rr.Code == http.StatusOK {
		t.Error("非法通道取值 \"image\"（正确的是 image-write）被接受 —— " +
			"通道取值来自用户输入，必须白名单校验；放过一个拼错的值会让建任务" +
			"走到一条不存在的通道上，而错误要到渲染阶段才暴露")
	}
}

// TestCreateTask_ChannelMismatchGivesActionableError 验证模板声明的通道与
// 它 pin 的制品集不符时，错误文案给出**可行的下一步**。
//
// P3-03 任务 5 的原文要求：选了镜像直写但该版本没有对应制品集时，错误文案要
// 说明「该版本尚未封装镜像，可改用 installer 驱动」——给出可行的下一步，而不
// 只是报错。这条是易用性要求，但它决定运维是自己解决还是来找开发。
func TestCreateTask_ChannelMismatchGivesActionableError(t *testing.T) {
	ts := newTestServer(t)
	hostID, _, _ := seedChannelFixtures(t, ts)
	ctx := context.Background()

	// 一个声明镜像直写、但 pin 了 installer 制品集的模板——等价于「运维选了
	// 镜像直写，而那个版本只封装过 installer 制品」。
	if _, err := ts.Store.CreateTemplate(ctx, model.Template{
		ID: "tpl-chan-mismatch", Name: "错配的模板",
		ArtifactSetID: "art-chan-installer", AnswerTemplate: "ks",
		DiskPolicy: model.DiskPolicyLargest, OnFailure: model.OnFailureHalt,
		Version: 1, State: model.TemplateStateActive,
		Channel: model.ChannelImageWrite,
	}); err != nil {
		t.Fatalf("CreateTemplate 失败: %v", err)
	}

	tok := ts.login("eve-admin")
	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId":     hostID,
		"templateId": "tpl-chan-mismatch",
		"formData":   map[string]string{},
	})
	if rr.Code == http.StatusOK {
		t.Fatal("通道错配的模板仍然建出了任务 —— 机器会拿到一份 installer 的应答" +
			"文件，而它引导的是一个没有 installer 的最小环境，症状是卡住直到超时")
	}
	body := rr.Body.String()
	if !strings.Contains(body, "installer") {
		t.Errorf("错误文案 = %q, want 提到可改用 installer 驱动 —— "+
			"计划要求给出可行的下一步而不只是报错；只说「通道不符」运维不知道该怎么办",
			body)
	}
}

// TestCreateTask_ImageWriteChannelSucceeds 验证通道一致时建任务正常。
func TestCreateTask_ImageWriteChannelSucceeds(t *testing.T) {
	ts := newTestServer(t)
	hostID, _, imageTplID := seedChannelFixtures(t, ts)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId":     hostID,
		"templateId": imageTplID,
		"formData":   map[string]string{},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("镜像直写通道建任务状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}

// TestCreateTask_ImageWriteRejectsIncompleteImageArtifact 验证镜像直写通道的
// 制品集缺 Image 三项时，建任务就被拒绝，而不是等到装机时才发现。
//
// 建任务时拦下来的价值：装机时发现意味着机器已经上电、已经走到写盘环节，而那
// 时报错只会留下一台状态不明的机器（盘可能已经被动过）。建任务时拦下来的成本
// 是零。
func TestCreateTask_ImageWriteRejectsIncompleteImageArtifact(t *testing.T) {
	ts := newTestServer(t)
	hostID, _, _ := seedChannelFixtures(t, ts)
	ctx := context.Background()

	// 标为镜像直写但 Image 为 nil 的制品集——等价于「导入没走完就被人手工
	// 改成了 ready」。
	if _, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "art-chan-broken", Name: "rocky9.4-image-broken",
		OSFamily: model.OSFamilyRHEL, OSVersion: "9.4", Arch: model.ArchX86_64,
		Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
		Channel: model.ChannelImageWrite,
	}); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}
	if _, err := ts.Store.CreateTemplate(ctx, model.Template{
		ID: "tpl-chan-broken", Name: "指向残缺镜像制品的模板",
		ArtifactSetID: "art-chan-broken", AnswerTemplate: "ks",
		DiskPolicy: model.DiskPolicyLargest, OnFailure: model.OnFailureHalt,
		Version: 1, State: model.TemplateStateActive,
		Channel: model.ChannelImageWrite,
	}); err != nil {
		t.Fatalf("CreateTemplate 失败: %v", err)
	}

	tok := ts.login("eve-admin")
	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId":     hostID,
		"templateId": "tpl-chan-broken",
		"formData":   map[string]string{},
	})
	if rr.Code == http.StatusOK {
		t.Error("镜像三项缺失的制品集仍然建出了任务 —— 装机时才发现意味着机器" +
			"已经上电、已经走到写盘环节，那时报错只留下一台状态不明的机器（盘可能" +
			"已经被动过）；建任务时拦下来的成本是零")
	}
}
