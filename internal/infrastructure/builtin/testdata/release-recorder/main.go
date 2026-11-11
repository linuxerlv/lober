// 发布快照写入工具，必须显式指定目录和写入开关。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/builtin"
)

type release struct {
	Templates []model.Template
	Hooks     []model.Hook
}

func writeNew(name string, data []byte) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	return errors.Join(writeErr, f.Close())
}

func run(dir string, write bool) error {
	if dir == "" || !write {
		return errors.New("必须指定 -dir 和 -write")
	}
	files, err := filepath.Glob(filepath.Join(dir, "releases", "v*.json"))
	if err != nil || len(files) == 0 {
		return errors.New("没有历史发布记录")
	}
	next := 1
	for _, file := range files {
		var n int
		if _, err := fmt.Sscanf(filepath.Base(file), "v%d.json", &n); err != nil {
			return err
		}
		if n >= next {
			next = n + 1
		}
	}
	r := release{Templates: builtin.Templates(), Hooks: builtin.Hooks()}
	pending := map[string][]byte{}
	add := func(kind, id string, version int, value any) error {
		name := filepath.Join(dir, "fingerprints", fmt.Sprintf("%s-%s-v%d.sha256", kind, id, version))
		want := []byte(builtin.Fingerprint(value) + "\n")
		old, err := os.ReadFile(name)
		if err == nil {
			if string(old) != string(want) {
				return fmt.Errorf("已有指纹不匹配：%s", name)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		pending[name] = want
		return nil
	}
	for _, tpl := range r.Templates {
		if err := add("template", tpl.ID, tpl.Version, tpl); err != nil {
			return err
		}
	}
	for _, hook := range r.Hooks {
		if err := add("hook", hook.ID, hook.Version, hook); err != nil {
			return err
		}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	for name, content := range pending {
		if err := writeNew(name, content); err != nil {
			return err
		}
	}
	return writeNew(filepath.Join(dir, "releases", fmt.Sprintf("v%d.json", next)), append(data, '\n'))
}

func main() {
	dir := flag.String("dir", "", "发布记录目录")
	write := flag.Bool("write", false, "确认写入")
	flag.Parse()
	if err := run(*dir, *write); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
