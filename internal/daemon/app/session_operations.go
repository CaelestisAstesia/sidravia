package app

import (
	"context"

	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
)

type SessionStartResult struct {
	SessionID session.AuthenticationSessionID
	Snapshot  session.Snapshot
	Outcome   string
}

const (
	SessionStartCreated        = "created"
	SessionStartAlreadyRunning = "already_running"
	SessionStartResumed        = "resumed"
)

func (application *Application) StartConfigurationAuthentication(ctx context.Context, configurationID config.ConfigurationID) (SessionStartResult, error) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	return application.startConfigurationAuthentication(ctx, configurationID)
}

// startConfigurationAuthentication requires operationMu through selection and start.
func (application *Application) startConfigurationAuthentication(ctx context.Context, configurationID config.ConfigurationID) (SessionStartResult, error) {
	application.sessionIndexMu.Lock()
	existing := application.sessionsByConfig[configurationID]
	application.sessionIndexMu.Unlock()
	if existing != "" && application.invalidSessions[existing] {
		if err := application.retireConfigurationSession(ctx, configurationID); err != nil {
			return SessionStartResult{}, err
		}
		existing = ""
	}
	if existing != "" {
		before, err := application.supervisor.Get(ctx, existing)
		if err != nil {
			return SessionStartResult{}, err
		}
		if before.State == session.Suspended || before.State == session.Stopping {
			definition, changed, err := application.changedReconnectDefinition(ctx, configurationID, existing)
			if err != nil {
				return SessionStartResult{}, err
			}
			if changed {
				return application.replaceConfigurationSession(ctx, configurationID, definition)
			}
		}
		snapshot, err := application.supervisor.EnsureRunning(ctx, existing)
		if err != nil {
			return SessionStartResult{}, err
		}
		outcome := SessionStartAlreadyRunning
		if before.State == session.Suspended || before.State == session.Stopping {
			outcome = SessionStartResumed
		}
		return SessionStartResult{SessionID: existing, Snapshot: snapshot, Outcome: outcome}, nil
	}
	definition, err := application.authenticationResolver.Resolve(ctx, configurationID, "pending")
	if err != nil {
		return SessionStartResult{}, err
	}

	sessionID, snapshot, err := application.supervisor.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		return SessionStartResult{}, err
	}

	application.sessionIndexMu.Lock()
	application.sessionsByConfig[configurationID] = sessionID
	application.sessionIndexMu.Unlock()
	application.reconnectByConfig[configurationID] = definition.AutoReconnect

	return SessionStartResult{SessionID: sessionID, Snapshot: snapshot, Outcome: SessionStartCreated}, nil
}

// StartOneShotAuthentication starts an ad hoc session with request-supplied
// credentials and the existing MaintainAuthentication intent. It uses the
// same serialized, single-active admission boundary as persisted starts.
// The session has no Configuration owner; failures leave no new association.
func (application *Application) StartOneShotAuthentication(ctx context.Context, input OneShotAuthenticationInput) (SessionStartResult, error) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()

	definition, err := application.authenticationResolver.ResolveOneShot(ctx, input, "pending")
	if err != nil {
		return SessionStartResult{}, err
	}

	sessionID, snapshot, err := application.supervisor.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		return SessionStartResult{}, err
	}

	// One-shot sessions have no Configuration owner and are intentionally not
	// added to sessionsByConfig.
	return SessionStartResult{SessionID: sessionID, Snapshot: snapshot, Outcome: SessionStartCreated}, nil
}

// automaticLoginFailure keeps internal error identity without echoing its text.
type automaticLoginFailure struct{ cause error }

func (*automaticLoginFailure) Error() string         { return "automatic_login_failed" }
func (failure *automaticLoginFailure) Unwrap() error { return failure.cause }

// PerformAutomaticLogin finds the sole AutoLogin=true Configuration, if any,
// and starts authentication within the same serialized application operation.
// It is evaluated once per daemon generation after the first accepted
// Environment Snapshot. A failure returns a fixed safe error that carries no
// configuration ID, username or underlying text. Its private cause remains
// available for internal classification; the caller is
// responsible for emitting the fixed Warn event. The daemon, IPC and snapshot
// delivery remain running regardless.
func (application *Application) PerformAutomaticLogin(ctx context.Context) error {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	if application.maintenanceActive.Load() {
		return nil
	}
	configurations, err := application.catalog.List(ctx)
	if err != nil {
		return &automaticLoginFailure{cause: err}
	}
	var autoLoginID config.ConfigurationID
	for _, configuration := range configurations {
		if configuration.AutoLogin {
			autoLoginID = configuration.ConfigurationID
			break
		}
	}
	if autoLoginID == "" {
		return nil
	}
	if _, err := application.startConfigurationAuthentication(ctx, autoLoginID); err != nil {
		return &automaticLoginFailure{cause: err}
	}
	return nil
}

func (application *Application) StopSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	return application.supervisor.Stop(ctx, sessionID)
}

func (application *Application) EnsureSessionRunning(ctx context.Context, sessionID session.AuthenticationSessionID) (SessionStartResult, error) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()

	if application.invalidSessions[sessionID] {
		return SessionStartResult{}, supervisor.ErrSessionStateConflict
	}
	before, err := application.supervisor.Get(ctx, sessionID)
	if err != nil {
		return SessionStartResult{}, err
	}
	if before.State == session.Suspended || before.State == session.Stopping {
		if configurationID, ok := application.configurationForSession(sessionID); ok {
			definition, changed, err := application.changedReconnectDefinition(ctx, configurationID, sessionID)
			if err != nil {
				return SessionStartResult{}, err
			}
			if changed {
				return application.replaceConfigurationSession(ctx, configurationID, definition)
			}
		}
	}
	snapshot, err := application.supervisor.EnsureRunning(ctx, sessionID)
	if err != nil {
		return SessionStartResult{}, err
	}
	outcome := SessionStartAlreadyRunning
	if before.State == session.Suspended || before.State == session.Stopping {
		outcome = SessionStartResumed
	}
	return SessionStartResult{SessionID: sessionID, Snapshot: snapshot, Outcome: outcome}, nil
}

func (application *Application) RestartSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	if application.invalidSessions[sessionID] {
		return session.Snapshot{}, supervisor.ErrSessionStateConflict
	}
	if configurationID, ok := application.configurationForSession(sessionID); ok {
		definition, changed, err := application.changedReconnectDefinition(ctx, configurationID, sessionID)
		if err != nil {
			return session.Snapshot{}, err
		}
		if changed {
			result, err := application.replaceConfigurationSession(ctx, configurationID, definition)
			return result.Snapshot, err
		}
	}
	return application.supervisor.Restart(ctx, sessionID)
}

// configurationForSession resolves the persisted owner while operationMu is held.
func (application *Application) configurationForSession(sessionID session.AuthenticationSessionID) (config.ConfigurationID, bool) {
	application.sessionIndexMu.Lock()
	defer application.sessionIndexMu.Unlock()
	for configurationID, associated := range application.sessionsByConfig {
		if associated == sessionID {
			return configurationID, true
		}
	}
	return "", false
}

// changedReconnectDefinition resolves the replacement before cleanup so a
// catalog or resolver failure leaves the existing Session intact.
// operationMu must be held by the caller.
func (application *Application) changedReconnectDefinition(ctx context.Context, configurationID config.ConfigurationID, currentSessionID session.AuthenticationSessionID) (session.RuntimeDefinition, bool, error) {
	configuration, err := application.catalog.Get(ctx, configurationID)
	if err != nil {
		return session.RuntimeDefinition{}, false, err
	}
	application.sessionIndexMu.Lock()
	associated := application.sessionsByConfig[configurationID]
	application.sessionIndexMu.Unlock()
	if associated != currentSessionID || application.reconnectByConfig[configurationID] == configuration.AutoReconnect {
		return session.RuntimeDefinition{}, false, nil
	}
	definition, err := application.authenticationResolver.Resolve(ctx, configurationID, "pending")
	if err != nil {
		return session.RuntimeDefinition{}, false, err
	}
	return definition, true, nil
}

// replaceConfigurationSession retires the previous immutable runtime only
// after the replacement definition resolves. A failed start leaves no Session
// association, so a later manual start can retry honestly.
// operationMu must be held by the caller.
func (application *Application) replaceConfigurationSession(ctx context.Context, configurationID config.ConfigurationID, definition session.RuntimeDefinition) (SessionStartResult, error) {
	if err := application.retireConfigurationSession(ctx, configurationID); err != nil {
		return SessionStartResult{}, err
	}
	sessionID, snapshot, err := application.supervisor.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		return SessionStartResult{}, err
	}
	application.sessionIndexMu.Lock()
	application.sessionsByConfig[configurationID] = sessionID
	application.sessionIndexMu.Unlock()
	application.reconnectByConfig[configurationID] = definition.AutoReconnect
	return SessionStartResult{SessionID: sessionID, Snapshot: snapshot, Outcome: SessionStartCreated}, nil
}

func (application *Application) RemoveSession(ctx context.Context, sessionID session.AuthenticationSessionID) error {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	if err := application.supervisor.Remove(ctx, sessionID); err != nil {
		return err
	}
	delete(application.invalidSessions, sessionID)
	application.sessionIndexMu.Lock()
	for configurationID, associated := range application.sessionsByConfig {
		if associated == sessionID {
			delete(application.sessionsByConfig, configurationID)
			delete(application.reconnectByConfig, configurationID)
		}
	}
	application.sessionIndexMu.Unlock()
	return nil
}

func (application *Application) GetSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	return application.supervisor.Get(ctx, sessionID)
}

func (application *Application) ListSessions(ctx context.Context) ([]session.Snapshot, error) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	return application.supervisor.List(ctx)
}
