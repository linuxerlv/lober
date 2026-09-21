// 本文件实现 contract.HostStore。对应实现计划任务 7，依据 SPEC §4.3。
//
// 骨架阶段占位，见 task.go 顶部注释。
package sqlite

import (
	"context"

	"github.com/lober/lober/internal/core/model"
)

func (s *Store) UpsertHost(ctx context.Context, host model.Host) (model.Host, error) {
	panic("sqlite: UpsertHost 尚未实现（计划任务 7）")
}

func (s *Store) GetHost(ctx context.Context, hostID string) (model.Host, error) {
	panic("sqlite: GetHost 尚未实现（计划任务 7）")
}

func (s *Store) FindHostByIdentity(ctx context.Context, sn, mac, uuid string) (model.Host, error) {
	panic("sqlite: FindHostByIdentity 尚未实现（计划任务 7）")
}

func (s *Store) ListHosts(ctx context.Context) ([]model.Host, error) {
	panic("sqlite: ListHosts 尚未实现（计划任务 7）")
}
