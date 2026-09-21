// 本文件实现 contract.LogStore 的生产版本：按
// docs/contracts/filesystem.md 「日志树 /var/lib/lober/log/」的约定落文件。
// 全仓没有第二个模块负责这件事——契约文档明确写着这是 M5/M6 共同遵守、
// M9 落地的磁盘布局，日志正文不入库（SPEC §2.5），只有装配点知道配置里
// 的真实日志根目录，因此本实现必须在这里写，不是某处遗漏的接口断言。
//
// 落盘路径（与 docs/contracts/filesystem.md 逐字符一致）：
//
//	<log_root>/tasks/<task_id>/<phase>.log      该任务某阶段的完整安装日志正文
//
// syslog 落盘路径（<log_root>/syslog/<task_id>.log）由网关的
// handleSyslogLine 通过同一个 LogStore.AppendLog 接口写入（phase 传空
// 字符串），本文件不需要为它单独分支——AppendLog 的 phase 参数本身就是
// 「文件名的一部分」，phase 为空时退化为按 taskID 单独归档，正好对应
// syslog 的落盘形态。为了让两种用途都能复用同一套路径拼接逻辑而不产生
// 歧义，本实现约定：phase 为空时落 "<log_root>/tasks/<task_id>/_syslog.log"
// ——不直接复用契约文档里 syslog/ 那一棵独立子树，因为 contract.LogStore
// 接口本身只有 (taskID, phase) 两个维度可用，没有「这是 syslog 还是
// task 日志」的第三个维度可传；调用方（gateway.Server.handleSyslogLine）
// 也没有为 syslog 传递专属的 phase 值。这是本装配点在现有接口形状下的
// 保守选择，若未来需要严格对齐契约文档的两棵独立子树，需要给
// contract.LogStore.AppendLog 增加一个「日志种类」参数——这是一个记入
// contractGaps 的偏差，不是本文件能在不改契约的前提下解决的。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// fileLogStore 是 contract.LogStore 的生产实现：每个 (taskID, phase) 对应
// 一个日志文件，追加写入。
type fileLogStore struct {
	root string
}

// newFileLogStore 构造一个按配置的日志根目录落盘的 LogStore。root 必须
// 已经存在（buildApp 的第 1 步已经建过三个存储目录）。
func newFileLogStore(root string) *fileLogStore {
	return &fileLogStore{root: root}
}

// logFilePath 计算 (taskID, phase) 对应的日志文件路径。phase 为空时
// （syslog 落盘用途，见文件头注释）落一个固定名字的文件，与「有 phase」
// 的情形共享同一个 taskID 目录，不额外新建一棵子树。
func (f *fileLogStore) logFilePath(taskID string, phase model.Phase) string {
	name := string(phase)
	if name == "" {
		name = "_syslog"
	}
	return filepath.Join(f.root, "tasks", taskID, name+".log")
}

// AppendLog 把一段日志文本追加到该任务/阶段对应的日志文件，文件与父目录
// 均按需创建。
func (f *fileLogStore) AppendLog(ctx context.Context, taskID string, phase model.Phase, content string) error {
	path := f.logFilePath(taskID, phase)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("cmd/lober: 创建日志目录失败（path=%s): %w", path, err)
	}

	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("cmd/lober: 打开日志文件失败（path=%s): %w", path, err)
	}
	defer fh.Close()

	if _, err := fh.WriteString(content); err != nil {
		return fmt.Errorf("cmd/lober: 写入日志文件失败（path=%s): %w", path, err)
	}
	return nil
}

// TailLog 读取该任务/阶段日志文件的最后 maxBytes 字节。文件不存在时返回
// 空字符串、不返回错误（contract.LogStore 文档明确要求）。
func (f *fileLogStore) TailLog(ctx context.Context, taskID string, phase model.Phase, maxBytes int) (string, error) {
	path := f.logFilePath(taskID, phase)
	fh, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("cmd/lober: 打开日志文件失败（path=%s): %w", path, err)
	}
	defer fh.Close()

	info, err := fh.Stat()
	if err != nil {
		return "", fmt.Errorf("cmd/lober: 读取日志文件信息失败（path=%s): %w", path, err)
	}

	size := info.Size()
	if maxBytes < 0 {
		maxBytes = 0
	}
	start := size - int64(maxBytes)
	if start < 0 {
		start = 0
	}

	buf := make([]byte, size-start)
	if len(buf) > 0 {
		if _, err := fh.ReadAt(buf, start); err != nil {
			return "", fmt.Errorf("cmd/lober: 读取日志文件尾部失败（path=%s): %w", path, err)
		}
	}
	return string(buf), nil
}

var _ contract.LogStore = (*fileLogStore)(nil)
