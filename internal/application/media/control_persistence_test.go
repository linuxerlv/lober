package media_test

import (
	"bytes"
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"testing"
)

type observeWake struct {
	calls   int
	observe func()
}

func (w *observeWake) Wake() { w.calls++; w.observe() }
func Test控制器只在保存成功后唤醒(t *testing.T) {
	for _, operation := range []string{"register", "upload", "retry", "reimport"} {
		t.Run(operation, func(t *testing.T) {
			a, _ := assembledDeletion(t)
			item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: operation, Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
			if err != nil {
				t.Fatal(err)
			}
			if operation != "register" {
				if err := a.Sources.Register(t.Context(), item, "upload", "", "", 4, "", assemblyAudit("iso.prepare")); err != nil {
					t.Fatal(err)
				}
			}
			if operation == "retry" {
				if err := a.Sources.Cancel(t.Context(), item.ID, assemblyAudit("iso.cancel")); err != nil {
					t.Fatal(err)
				}
			}
			if operation == "reimport" {
				if err := a.Sources.Upload(t.Context(), item.ID, "", 0, bytes.NewBufferString("data"), repository.Audit{}); err != nil {
					t.Fatal(err)
				}
				item.State = model.ISOFailed
				item, err = a.Store.UpdateISO(t.Context(), item)
				if err != nil {
					t.Fatal(err)
				}
			}
			w := &observeWake{observe: func() {
				src, exists, err := a.Store.GetSource(t.Context(), item.ID, "")
				if err != nil || !exists {
					t.Fatal("唤醒时来源尚未保存", src, err)
				}
				want := model.SourcePending
				if operation == "upload" || operation == "reimport" {
					want = model.SourceFetched
				}
				if src.State != want {
					t.Fatal("唤醒时状态尚未提交", src)
				}
				if operation == "reimport" {
					saved, err := a.Store.GetISO(t.Context(), item.ID)
					if err != nil || saved.State != model.ISOAcquiring {
						t.Fatal("唤醒时尚未排队", saved, err)
					}
				}
			}}
			control := appmedia.NewControl(a.Sources, w)
			call := func(fail bool) error {
				audit := assemblyAudit("iso." + operation)
				if fail {
					audit.Actor = ""
				}
				switch operation {
				case "register":
					return control.Register(t.Context(), item, "upload", "", "", 4, "", audit)
				case "upload":
					content := "data"
					if fail {
						content = "too-long"
					}
					return control.Upload(t.Context(), item.ID, "", 0, bytes.NewBufferString(content), repository.Audit{})
				case "retry":
					return control.Retry(t.Context(), item.ID, audit)
				default:
					return control.Reimport(t.Context(), item.ID, audit)
				}
			}
			if err := call(true); err == nil {
				t.Fatal("失败输入被接受")
			}
			if w.calls != 0 {
				t.Fatal("失败仍唤醒", w.calls)
			}
			if err := call(false); err != nil {
				t.Fatal(err)
			}
			if w.calls != 1 {
				t.Fatal("保存成功未唤醒", w.calls)
			}
		})
	}
}
