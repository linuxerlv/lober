package execution

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/lober/lober/internal/application/execution/port"
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/repository"
)

type installerLogs struct {
	repo     port.InstallerLogRepository
	notifier Notifier
}

// NewInstallerLogs 的八项约定唯一连接 docs/contracts/machine-execution.md「日志收录入口」。
func NewInstallerLogs(repo port.InstallerLogRepository, notifier Notifier) port.InstallerLogReceiver {
	return &installerLogs{repo: repo, notifier: notifier}
}

// ReceiveInstallerLog 的八项约定唯一连接 docs/contracts/machine-execution.md「日志收录入口」。
func (s *installerLogs) ReceiveInstallerLog(ctx context.Context, in port.InstallerLog) error {
	task, err := s.repo.GetTask(ctx, in.TaskID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	if !executionrules.CanReceiveInstallerLog(task, in.Attempt) {
		return nil
	}
	_, err = s.repo.AppendEvents(ctx, task.ID, []repository.NewEvent{{At: in.At, Kind: "installer.syslog", Fields: map[string]string{"source_ip": in.SourceIP, "log_base64": base64.StdEncoding.EncodeToString(in.Body)}}})
	if err == nil && s.notifier != nil {
		s.notifier.Publish(task.ID)
	}
	return err
}
