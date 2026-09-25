// 本文件是 hook 正文落盘的生产实现：把 console.HookPublisher 这个窄接口接到
// 真实文件系统上，供 buildAppWithDB 注入 console.Deps.HookPublisher。
//
// **它补的是一条断掉的链路。** docs/contracts/filesystem.md 第 4 节规定
// Hook.Body 存数据库、机器侧从 /artifacts/hooks/<digest>/script.sh 取
// （SPEC §6.3），两者是同一份内容的两个副本。而在本文件出现之前，全仓没有
// 任何代码写那个文件——hook 能创建、能审批、能被模板引用、任务能建出来，
// 然后装机在 %pre 阶段卡到超时。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// hookPublisher 把 hook 正文写到 <artifactRoot>/hooks/<digest>/script.sh。
//
// 路径形态由 nginx 侧决定：deploy/nginx/lober.conf 把 /artifacts/ alias 到
// artifactRoot，所以机器请求的 /artifacts/hooks/<digest>/script.sh 对应
// 磁盘上的 <artifactRoot>/hooks/<digest>/script.sh。**hooks/ 与各制品集的
// <artifact_set_id>/ 目录同级**，不在任何制品集内部——hook 的生命周期与
// 制品集无关（同一个 hook 可被引用不同 ISO 的多个模板引用）。
type hookPublisher struct {
	artifactRoot string
}

func newHookPublisher(artifactRoot string) *hookPublisher {
	return &hookPublisher{artifactRoot: artifactRoot}
}

// PublishHook 实现 console.HookPublisher。
func (p *hookPublisher) PublishHook(ctx context.Context, digest, body string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cmd/lober: 落盘 hook 前 context 已取消: %w", err)
	}
	if digest == "" {
		return fmt.Errorf("cmd/lober: hook digest 为空，无法确定落盘目录")
	}

	// 校验 digest 确实是 body 的 sha256。目录名与内容不符会让机器侧的
	// `sha256sum -c` 失败（render/clientlib.go 的 run_hook 取到脚本后会核对
	// 一遍），而那个失败发生在目标机上、只能到 BMC 控制台看——在这里挡住
	// 成本低得多。
	sum := sha256.Sum256([]byte(body))
	if want := hex.EncodeToString(sum[:]); want != digest {
		return fmt.Errorf("cmd/lober: hook digest 与正文不符（digest=%s，正文实际 sha256=%s）"+
			"——目录名与内容不一致会让机器侧的 sha256sum -c 失败，那个失败只能到 BMC "+
			"控制台看，因此在这里拒绝", digest, want)
	}

	dir := filepath.Join(p.artifactRoot, "hooks", digest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cmd/lober: 创建 hook 目录 %q 失败: %w", dir, err)
	}

	// 0644 而不是 0755：nginx worker 只需读，机器侧取到后自己 chmod 再执行
	// （run_hook 是 `bash /tmp/h.sh`，不依赖执行位）。目录 0755 是因为 nginx
	// 要能进去。
	//
	// 先写临时文件再 rename：同一个 digest 可能被并发发布（多个模板引用同一
	// hook、或两个请求同时到），直接覆盖写会让并发的读方看到半截文件——而
	// 读方是机器侧的 curl，半截文件能通过 curl 但过不了 sha256sum -c，表现
	// 成一次莫名的 hook 校验失败。rename 在同一文件系统内是原子的。
	final := filepath.Join(dir, "script.sh")
	tmp, err := os.CreateTemp(dir, ".script-*.tmp")
	if err != nil {
		return fmt.Errorf("cmd/lober: 创建 hook 临时文件失败（目录 %q）: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后这次 Remove 是 no-op

	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return fmt.Errorf("cmd/lober: 写 hook 正文失败（%q）: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("cmd/lober: 设置 hook 文件权限失败（%q）: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cmd/lober: 关闭 hook 临时文件失败（%q）: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		return fmt.Errorf("cmd/lober: 落盘 hook 到 %q 失败: %w", final, err)
	}
	return nil
}
