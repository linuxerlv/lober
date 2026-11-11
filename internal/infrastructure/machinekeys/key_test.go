package machinekeys

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func Test机器密钥创建重读与权限保持(t *testing.T) {
	t.Run("创建重读", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "private")
		path := filepath.Join(parent, "key")
		key, err := LoadOrCreate(path)
		if err != nil || len(key) != 32 {
			t.Fatal("创建失败", err)
		}
		for _, tc := range []struct {
			p    string
			mode os.FileMode
		}{{parent, 0700}, {path, 0600}} {
			info, e := os.Stat(tc.p)
			if e != nil || info.Mode().Perm() != tc.mode {
				t.Fatal("权限改变", e)
			}
		}
		again, e := LoadOrCreate(path)
		if e != nil || !bytes.Equal(key, again) {
			t.Fatal("重读改变密钥", e)
		}
	})
	t.Run("已有父目录", func(t *testing.T) {
		parent := t.TempDir()
		if e := os.Chmod(parent, 0750); e != nil {
			t.Fatal(e)
		}
		_, e := LoadOrCreate(filepath.Join(parent, "key"))
		if e != nil {
			t.Fatal(e)
		}
		info, _ := os.Stat(parent)
		if info.Mode().Perm() != 0750 {
			t.Fatal("修改已有父目录")
		}
	})
	t.Run("拒绝相对路径", func(t *testing.T) {
		if _, e := LoadOrCreate("key"); e == nil || e.Error() != "key path must be absolute" {
			t.Fatal(e)
		}
	})
	for _, kind := range []string{"partial", "short", "long", "public", "symlink", "directory", "parent-file"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "key")
			data := bytes.Repeat([]byte("k"), 32)
			switch kind {
			case "partial":
				data = nil
			case "short":
				data = data[:31]
			case "long":
				data = append(data, 'k')
			}
			if kind == "directory" {
				if e := os.Mkdir(path, 0700); e != nil {
					t.Fatal(e)
				}
			} else if kind == "parent-file" {
				if e := os.WriteFile(path, data, 0600); e != nil {
					t.Fatal(e)
				}
				path = filepath.Join(path, "key")
			} else {
				target := path
				if kind == "symlink" {
					target = path + ".target"
				}
				if e := os.WriteFile(target, data, 0600); e != nil {
					t.Fatal(e)
				}
				if kind == "symlink" {
					if e := os.Symlink(target, path); e != nil {
						t.Fatal(e)
					}
				}
				if kind == "public" {
					if e := os.Chmod(path, 0644); e != nil {
						t.Fatal(e)
					}
				}
			}
			if _, e := LoadOrCreate(path); e == nil {
				t.Fatal("接受不安全路径", kind)
			}
		})
	}
	t.Run("已有密钥保持原字节", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "key")
		want := bytes.Repeat([]byte("z"), 32)
		if e := os.WriteFile(path, want, 0600); e != nil {
			t.Fatal(e)
		}
		got, e := LoadOrCreate(path)
		if e != nil || !bytes.Equal(got, want) {
			t.Fatal("替换了已有密钥", e)
		}
	})
	t.Run("父目录权限错误", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "denied")
		if e := os.Mkdir(parent, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.Chmod(parent, 0000); e != nil {
			t.Fatal(e)
		}
		defer os.Chmod(parent, 0700)
		if _, e := LoadOrCreate(filepath.Join(parent, "key")); e == nil {
			t.Fatal("无权限父目录仍创建成功")
		}
	})

	t.Run("竞争创建", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "key")
		type result struct {
			key []byte
			err error
		}
		results := make(chan result, 24)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range 24 {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; k, e := LoadOrCreate(path); results <- result{k, e} }()
		}
		close(start)
		wg.Wait()
		close(results)
		persisted, e := os.ReadFile(path)
		if e != nil || len(persisted) != 32 {
			t.Fatal("未持久化", e)
		}
		success := 0
		for r := range results {
			if r.err != nil {
				if r.err.Error() != "key must be a private regular 32-byte file" {
					t.Fatal("竞争错误改变", r.err)
				}
				continue
			}
			success++
			if !bytes.Equal(r.key, persisted) {
				t.Fatal("成功者读到不同密钥")
			}
		}
		if success == 0 {
			t.Fatal("没有成功创建者")
		}
	})
}
