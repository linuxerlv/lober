//go:build wireinject

package main

import (
	"context"
	"github.com/goforj/wire"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/notification"
	selectionguard "github.com/lober/lober/internal/infrastructure/selection"
	"github.com/lober/lober/internal/interfaces/console"
	"log/slog"
)

func newApp(ctx context.Context, cfg config, clock ports.Clock, logger *slog.Logger) (*app, func(), error) {
	wire.Build(openStore, loggingStoreOf, wire.Bind(new(repository.Store), new(*loggingStore)), mediaDirsOf, gateOf, sourceLocksOf, acquisitionOf, deletionJournalOf, deletionOf, supervisorOf, mediaControlOf, notification.NewHub, machineOf, selectionguard.NewGuard, taskBackendOf, taskControllerOf, taskReaderOf, taskEventsOf, wizardOf, hookAuthoringOf, templateAuthoringOf, previewRendererOf, authoringEditorOf, hostRegistryOf, mediaRegistryOf, identityServiceOf, auditReaderOf, consoleDepsOf, console.New, appOf)
	return nil, nil, nil
}
