// 本文件测试 hook 正文落盘（HookPublisher）这条路径。
//
// **缺了它，任何引用了 hook 的模板都装不上机。** docs/contracts/filesystem.md
// 第 4 节明确规定：Hook.Body 存数据库，但机器侧不从数据库取 hook，而是从
// `/artifacts/hooks/<digest>/script.sh` 取（SPEC §6.3）——数据库记录与磁盘
// 文件是同一份内容的两个副本。而此前全仓**没有任何代码写那个文件**。
//
// 实测（2026-09-25，QEMU BIOS 目标机走完整链路）：anaconda 已启动、取到
// kickstart（5814 字节），%pre 段里渲染出的 run_hook 去取脚本：
//
//	15:03:03 10.20.20.112 "/artifacts/hooks/438ecbbf.../script.sh" 404 146
//
// required=true 的 hook 取不到脚本即失败，%pre 卡住，10 分钟后阶段超时：
//
//	failureReason = install_phase
//	logTail       = "阶段 pre 超时: 预算=10m0s 实际已过=613秒"
//
// 目标盘一个字节都没写入。
package console

import (
	"context"
	"net/http"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// fakeHookPublisher 记录落盘调用，供断言「谁被落盘、内容是什么」。
type fakeHookPublisher struct {
	published map[string]string // digest → body
	failWith  error
}

func newFakeHookPublisher() *fakeHookPublisher {
	return &fakeHookPublisher{published: map[string]string{}}
}

func (f *fakeHookPublisher) PublishHook(ctx context.Context, digest, body string) error {
	if f.failWith != nil {
		return f.failWith
	}
	f.published[digest] = body
	return nil
}

// TestHookPublish_OnCreate 锁住创建 hook 时就落盘。
//
// 落盘时机是**创建或产出新版本时**，不是审批通过时——契约文档给的理由是
// preview 端点（POST /templates/{id}/preview）需要在 hook 还是 draft 状态时
// 就能取到内容做假数据渲染。
func TestHookPublish_OnCreate(t *testing.T) {
	ts := newTestServer(t)
	pub := newFakeHookPublisher()
	ts.Server.deps.HookPublisher = pub
	tok := ts.login("carol-author")

	body := "#!/bin/bash\necho hello\n"
	rr := ts.doJSON("POST", "/console/v1/hooks", tok, map[string]any{
		"name": "h-publish", "stage": "pre", "interpreter": "bash", "body": body,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("创建 hook 失败: %d, body=%s", rr.Code, rr.Body.String())
	}

	digest := model.HookDigest(body)
	got, ok := pub.published[digest]
	if !ok {
		t.Fatalf("创建 hook 后没有落盘 digest=%s——机器侧从 "+
			"/artifacts/hooks/<digest>/script.sh 取脚本，不落盘就是 404，"+
			"required 的 hook 会让 pre 阶段卡到超时", digest)
	}
	if got != body {
		t.Errorf("落盘内容与 Body 不一致:\n落盘=%q\nBody=%q", got, body)
	}
}

// TestHookPublish_OnUpdateNewVersion 锁住 PUT 产出新版本时也落盘。
//
// digest 是内容寻址的，新正文就是新目录——旧目录保留（可能仍被 pin 住旧
// digest 的任务引用，SPEC §2.6 决策固化）。
func TestHookPublish_OnUpdateNewVersion(t *testing.T) {
	ts := newTestServer(t)
	pub := newFakeHookPublisher()
	ts.Server.deps.HookPublisher = pub
	tok := ts.login("carol-author")

	bodyV1 := "#!/bin/bash\necho v1\n"
	rr := ts.doJSON("POST", "/console/v1/hooks", tok, map[string]any{
		"name": "h-upd", "stage": "pre", "interpreter": "bash", "body": bodyV1,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("创建失败: %d, %s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	bodyV2 := "#!/bin/bash\necho v2\n"
	rr = ts.doJSON("PUT", "/console/v1/hooks/"+created.Data.ID, tok, map[string]any{
		"name": "h-upd", "stage": "pre", "interpreter": "bash", "body": bodyV2,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("更新失败: %d, %s", rr.Code, rr.Body.String())
	}

	if _, ok := pub.published[model.HookDigest(bodyV2)]; !ok {
		t.Error("PUT 产出新版本后没有落盘新 digest——引用新版本的模板会拿到 404")
	}
	// 旧 digest 仍在：已 pin 住它的任务还要用。
	if _, ok := pub.published[model.HookDigest(bodyV1)]; !ok {
		t.Error("旧 digest 的落盘不该被移除——已固化引用它的任务（SPEC §2.6）仍要取")
	}
}

// TestHookPublish_FailureRejectsCreate 锁住落盘失败时创建整体失败。
//
// 不能「库里有、盘上没有」：那正是本缺口的形态——控制台显示 hook 创建成功、
// 状态正常，而每一次引用它的装机都在 %pre 卡到超时，且失败信息里只有
// 「阶段 pre 超时」，看不出是 hook 脚本 404。
func TestHookPublish_FailureRejectsCreate(t *testing.T) {
	ts := newTestServer(t)
	pub := newFakeHookPublisher()
	pub.failWith = context.DeadlineExceeded
	ts.Server.deps.HookPublisher = pub

	rr := ts.doJSON("POST", "/console/v1/hooks", ts.login("carol-author"), map[string]any{
		"name": "h-fail", "stage": "pre", "interpreter": "bash", "body": "#!/bin/bash\ntrue\n",
	})
	if rr.Code == http.StatusOK {
		t.Error("落盘失败时创建必须整体失败——「库里有、盘上没有」会让每次引用它的" +
			"装机都在 pre 阶段卡到超时，而失败信息里看不出是 hook 脚本 404")
	}
}

// TestHookPublish_NilPublisherStillWorks 锁住 Deps.HookPublisher 为 nil 时
// 不 panic（与 ArtifactImporter 同一种按需注入、调用前判空的接缝设计）。
func TestHookPublish_NilPublisherStillWorks(t *testing.T) {
	ts := newTestServer(t)
	ts.Server.deps.HookPublisher = nil

	rr := ts.doJSON("POST", "/console/v1/hooks", ts.login("carol-author"), map[string]any{
		"name": "h-nil", "stage": "pre", "interpreter": "bash", "body": "#!/bin/bash\ntrue\n",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("HookPublisher 为 nil 时应照常创建（测试与某些部署形态不注入它），实际 %d", rr.Code)
	}
}
