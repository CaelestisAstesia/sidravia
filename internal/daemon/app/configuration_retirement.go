package app

import (
	"context"
	"errors"
	"time"

	connectionconfig "sidravia/internal/connection/config"
	"sidravia/internal/daemon/authentication/supervisor"
)

// ErrConfigurationSessionInvalidation distinguishes a durable edit from failed cleanup.
var ErrConfigurationSessionInvalidation = errors.New("configuration committed; session cleanup required")

func (application *Application) retireConfigurationSession(ctx context.Context, id connectionconfig.ID) error {
	application.sessionIndexMu.Lock()
	sessionID := application.sessionsByConfig[id]
	application.sessionIndexMu.Unlock()
	if sessionID == "" {
		return nil
	}
	application.invalidSessions[sessionID] = true
	// Once persistence commits, caller cancellation must not skip cleanup.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()
	if err := application.supervisor.Remove(cleanup, sessionID); err != nil && !errors.Is(err, supervisor.ErrSessionNotFound) {
		return errors.Join(ErrConfigurationSessionInvalidation, err)
	}
	delete(application.invalidSessions, sessionID)
	application.sessionIndexMu.Lock()
	delete(application.sessionsByConfig, id)
	application.sessionIndexMu.Unlock()
	delete(application.reconnectByConfig, id)
	return nil
}

func (application *Application) RemoveConfiguration(ctx context.Context, id connectionconfig.ID, expectedVersion string, allow ...bool) error {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	if err := application.catalog.Delete(ctx, id, expectedVersion, allow...); err != nil {
		return err
	}
	return application.retireConfigurationSession(ctx, id)
}
