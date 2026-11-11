package media_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

var errFetchAckLost = errors.New("获取完成的提交回执丢失")

type fetchOutcomeStore struct {
	repository.Store
	committed, unreadable, lost bool
	cancel                      context.CancelFunc
}

func (s *fetchOutcomeStore) SaveSource(ctx context.Context, src model.Source, audit repository.Audit) (model.Source, error) {
	if src.State != model.SourceFetched || s.lost {
		return s.Store.SaveSource(ctx, src, audit)
	}
	if s.committed {
		if _, err := s.Store.SaveSource(ctx, src, audit); err != nil {
			return model.Source{}, err
		}
	}
	s.lost = true
	if s.cancel != nil {
		s.cancel()
	}
	return model.Source{}, errFetchAckLost
}

func (s *fetchOutcomeStore) GetSource(ctx context.Context, id, role string) (model.Source, bool, error) {
	if s.lost && s.unreadable {
		return model.Source{}, false, errors.New("暂时无法读取数据库")
	}
	return s.Store.GetSource(ctx, id, role)
}

func TestFetchReconcilesLostCommitAcknowledgment(t *testing.T) {
	for _, tc := range []struct {
		name       string
		committed  bool
		unreadable bool
		cancelled  bool
	}{
		{name: "已提交", committed: true},
		{name: "已提交但读取失败", committed: true, unreadable: true},
		{name: "已提交但原请求取消", committed: true, cancelled: true},
		{name: "确认未提交"},
		{name: "未提交但读取失败", unreadable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, db := importSetup(t)
			item, err := db.CreateISO(t.Context(), model.ISO{Name: "提交结果核对", Family: "linux", Series: "ubuntu"}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			data := isotest.Image(importFixture("ubuntu"))
			if err := fixture.Register(t.Context(), item, "upload", "", "", int64(len(data)), "", importAudit()); err != nil {
				t.Fatal(err)
			}
			store := &fetchOutcomeStore{Store: db, committed: tc.committed, unreadable: tc.unreadable}
			fixture.store = store
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancelled {
				store.cancel = cancel
			}
			err = fixture.Upload(ctx, item.ID, "", 0, bytes.NewReader(data), repository.Audit{})
			if tc.committed && !tc.unreadable {
				if err != nil {
					t.Errorf("确认已提交后不应报失败：%v", err)
				}
			} else if !errors.Is(err, errFetchAckLost) {
				t.Errorf("未确认提交成功时应保留原错误：%v", err)
			}
			final := fixture.finalPath(item.ID, "")
			preserveFinal := tc.committed || tc.unreadable
			path := final
			missing := final + ".part"
			if !preserveFinal {
				path, missing = missing, path
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(got, data) {
				t.Fatalf("发布文件状态错误：%v", readErr)
			}
			if _, err := os.Stat(missing); !os.IsNotExist(err) {
				t.Fatalf("不应同时保留错误的文件名：%v", err)
			}
			// 恢复数据库连接后，已提交的内容可以直接导入；未知且未提交的内容可重放收尾。
			fixture.store = db
			if !tc.committed && tc.unreadable {
				if err := fixture.Upload(t.Context(), item.ID, "", 0, bytes.NewReader(data), repository.Audit{}); err != nil {
					t.Fatal("连接恢复后不能继续上传", err)
				}
			}
			if tc.committed || tc.unreadable {
				if err := fixture.Import(t.Context(), item.ID); err != nil {
					t.Fatal("保存结果恢复后不能导入", err)
				}
				current, err := db.GetISO(t.Context(), item.ID)
				if err != nil || current.State != model.ISOReady {
					t.Fatal("导入没有完成", current.State, err)
				}
			}
		})
	}
}
