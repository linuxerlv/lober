package media

import (
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func CheckDeletion(item model.ISO, sources []model.Source) error {
	if err := repository.CheckID(item.ID); err != nil {
		return err
	}
	// 尚未识别的镜像可以没有版本和架构；临时值仅用于校验，不写回对象。
	checked := item
	if checked.Version == "" {
		checked.Version = "pending"
	}
	if checked.Arch == "" {
		checked.Arch = "x86_64"
	}
	if sources == nil || len(checked.Validate()) > 0 {
		return repository.ErrInvalid
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if source.ISOID != item.ID || seen[source.Role] {
			return repository.ErrInvalid
		}
		if err := repository.CheckSource(source); err != nil {
			return err
		}
		seen[source.Role] = true
	}
	return nil
}
