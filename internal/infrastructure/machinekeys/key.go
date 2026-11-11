// Package machinekeys 保存机器协议签名密钥的私密文件。
package machinekeys

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// LoadOrCreate 属于密钥文件实现，只供可信启动组装调用；不接受网页路径。
// 输入必须是绝对路径，由组装点选择默认路径；不规范化、不另加长度限制。
// 成功返回原文件或新建的32字节密钥；无列表、顺序或空结果。
// 错误依次为非绝对路径、建目录、检查文件、随机数、独占创建、写入，或原文件读取错误；
// 非私密普通32字节文件被拒绝，写入/同步/关闭错误统一遮蔽为 key write failed，无业务原因代码。
// 写入仅限缺失的父目录和独占创建的0600文件；新目录0700，已有父目录权限不变。
// 重复调用重读文件；创建竞争失败者重读，可能因对方尚未写完而失败，不等待也不加锁。
// 并发只依赖独占创建和当次文件检查；不承诺所有竞争者成功，不另作版本检查。
// 无审计、通知、上下文或超时；同步完成文件写入、同步与关闭后返回，不输出密钥。
func LoadOrCreate(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("key path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return LoadOrCreate(path)
			}
			return nil, err
		}
		_, writeErr := f.Write(key)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return nil, errors.New("key write failed")
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != 32 {
		return nil, errors.New("key must be a private regular 32-byte file")
	}
	return os.ReadFile(path)
}
