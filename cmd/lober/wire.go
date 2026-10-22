//go:build wireinject

package main

import (
	"context"
	"github.com/goforj/wire"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/notification"
	"github.com/lober/lober/internal/interfaces/console"
	"log/slog"
)

func newApp(ctx context.Context, cfg config, clock console.Clock, logger *slog.Logger) (*app, func(), error) {
	wire.Build(openStore, loggingStoreOf, wire.Bind(new(repository.Store), new(*loggingStore)), nativeSourcesOf, gateOf, newDeletionJournal, supervisorOf, notification.NewHub, machineOf, console.NewSelectionGuard, taskControlOf, wizardOf, hookAuthoringOf, consoleDepsOf, console.New, appOf)
	return nil, nil, nil
}
