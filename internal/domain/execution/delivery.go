package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

var ErrFileNotFound = errors.New("machine file not found")
var ErrFrozenContent = errors.New("frozen content digest mismatch")

type FrozenFile struct {
	Kind catalog.FileKind
	Slot string
	Body []byte
	Hook bool
}

// BootDeliverable 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func BootDeliverable(t model.Task) bool { return !t.State.Finished() }

// PreseedAvailable 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func PreseedAvailable(t model.Task) bool {
	return t.State == model.TaskBooting || t.State == model.TaskInstalling
}

// InstallerAnswerAvailable 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
// 只判断原格式条件，不能替代此前的 Preseed 资格检查。
func InstallerAnswerAvailable(t model.Task) bool { return t.Snapshot.Format == "preseed" }

// FinalSignal 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func FinalSignal(sig Signal) bool {
	return sig.Timing == catalog.TimingFirstboot || sig.Phase == "booted" || sig.Phase == "init-start" || sig.Phase == "init-done" || sig.Phase == "ready"
}

// SignalAccessAllowed 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
// 仅在原凭据验证后调用，具体信号顺序及重复仍交给 Advance。
func SignalAccessAllowed(t model.Task, sig Signal) bool {
	return FinalSignal(sig) || !t.State.Finished()
}

// InstallAccessNeedsTime 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func InstallAccessNeedsTime(t model.Task) bool { return t.Execution != nil && t.Execution.Deadline > 0 }

// InstallAccessAllowed 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func InstallAccessAllowed(t model.Task, at int64) bool {
	if t.Execution == nil {
		return false
	}
	if InstallAccessNeedsTime(t) && at >= t.Execution.Deadline {
		return false
	}
	return PreseedAvailable(t)
}

// SelectFile 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func SelectFile(snapshot model.Snapshot, name string) (FrozenFile, error) {
	if strings.HasPrefix(name, "hooks/") {
		digest := strings.TrimPrefix(name, "hooks/")
		if len(digest) != 64 {
			return FrozenFile{}, ErrFileNotFound
		}
		var file FrozenFile
		for _, slot := range snapshot.Slots {
			if !slot.Exec {
				continue
			}
			for _, hook := range slot.Instances {
				if hook.Digest == digest {
					sum := sha256.Sum256([]byte(hook.Body))
					if hex.EncodeToString(sum[:]) != digest {
						return FrozenFile{}, ErrFrozenContent
					}
					file = FrozenFile{Body: []byte(hook.Body), Hook: true}
				}
			}
		}
		if !file.Hook {
			return FrozenFile{}, ErrFileNotFound
		}
		return file, nil
	}
	answer, _ := catalog.DeliveryFileName(snapshot.Format, catalog.FileAnswer)
	if name == answer {
		return FrozenFile{Kind: catalog.FileAnswer}, nil
	}
	aux, _ := catalog.DeliveryFileName(snapshot.Format, catalog.FileAux)
	if aux != "" && name == aux {
		return FrozenFile{Kind: catalog.FileAux}, nil
	}
	for _, slot := range snapshot.Slots {
		if slot.Exec && name == "scripts/"+strconv.Itoa(slot.Seq)+".sh" {
			return FrozenFile{Kind: catalog.FileScript, Slot: slot.Name}, nil
		}
	}
	return FrozenFile{}, ErrFileNotFound
}
