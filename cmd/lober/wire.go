//go:build wireinject

package main

import (
	"context"
	"github.com/goforj/wire"
	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/domain/repository"
	"log/slog"
)

func newApp(ctx context.Context, cfg config, clock console.Clock, logger *slog.Logger) (*app, func(), error) {
	wire.Build(openStore, loggingStoreOf, wire.Bind(new(repository.Store), new(*loggingStore)), nativeSourcesOf, gateOf, newDeletionJournal, supervisorOf, console.NewHub, consoleDepsOf, console.New, appOf)
	return nil, nil, nil
}
