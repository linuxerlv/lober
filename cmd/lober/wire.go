//go:build wireinject

package main

import (
	"context"
	"github.com/goforj/wire"
	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/store"
	"log/slog"
)

func newApp(ctx context.Context, cfg config, clock console.Clock, logger *slog.Logger) (*app, func(), error) {
	wire.Build(openStore, loggingStoreOf, wire.Bind(new(store.Store), new(*loggingStore)), nativeSourcesOf, gateOf, newDeletionJournal, supervisorOf, console.NewHub, machineOf, consoleDepsOf, console.New, appOf)
	return nil, nil, nil
}
