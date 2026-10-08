package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"sidravia/internal/daemon/authentication/supervisor"
	"sidravia/internal/ipc/contract"
)

var _ contract.StateEventSource = (*Application)(nil)

var errStateProjection = errors.New("application state projection: stream discontinuity")

// SubscribeStateEvents registers before its first owner query. App operations
// cannot fall between registration and bootstrap; network/actor owners may
// advance independently and their original revisions remain in the stream.
func (application *Application) SubscribeStateEvents(ctx context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
	if err := readModelContextError(ctx); err != nil {
		return contract.StateBootstrap{}, nil, err
	}
	source, err := application.sup.Subscribe()
	if err != nil {
		return contract.StateBootstrap{}, nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			source.Close()
		}
	}()
	application.opMu.Lock()
	defer application.opMu.Unlock()
	view, err := application.listSessionViewLocked(ctx)
	if err != nil {
		return contract.StateBootstrap{}, nil, err
	}
	network, available, err := application.sup.LatestSystemNetworkSnapshot(ctx)
	if err != nil {
		return contract.StateBootstrap{}, nil, errors.Join(err, readModelContextError(ctx))
	}
	if err := readModelContextError(ctx); err != nil {
		return contract.StateBootstrap{}, nil, err
	}
	value := contract.StateBootstrap{Sessions: contract.SessionListResult{Sessions: []contract.SessionResult{}, CleanupRequiredSessionIDs: []string{}}, Network: networkInterfacesResult(network, available)}
	for _, snapshot := range view.Sessions {
		value.Sessions.Sessions = append(value.Sessions.Sessions, toSessionResult(snapshot))
	}
	for _, id := range view.CleanupRequiredSessionIDs {
		value.Sessions.CleanupRequiredSessionIDs = append(value.Sessions.CleanupRequiredSessionIDs, string(id))
	}
	data, err := contract.MarshalStateBootstrap(value)
	if err != nil {
		return contract.StateBootstrap{}, nil, fmt.Errorf("%w: %w", errStateProjection, err)
	}
	owned, err := contract.DecodeStateBootstrap(data)
	if err != nil {
		return contract.StateBootstrap{}, nil, fmt.Errorf("%w: %w", errStateProjection, err)
	}
	if err := readModelContextError(ctx); err != nil {
		return contract.StateBootstrap{}, nil, err
	}
	transferred = true
	return owned, &applicationStateStream{application: application, source: source}, nil
}

type applicationStateStream struct {
	application *Application
	source      *supervisor.Subscription
	mu          sync.Mutex // protects termination cause only; never held while waiting on source
	cause       error
}

func (stream *applicationStateStream) Close() error {
	stream.source.Close()
	return nil
}

func (stream *applicationStateStream) Next(ctx context.Context) (contract.StateEvent, error) {
	if err := readModelContextError(ctx); err != nil {
		return contract.StateEvent{}, err
	}
	stream.mu.Lock()
	cause := stream.cause
	stream.mu.Unlock()
	if cause != nil {
		return contract.StateEvent{}, cause
	}
	event, err := stream.source.Next(ctx)
	if err != nil {
		if ctx.Err() == nil {
			_ = stream.Close()
		}
		return contract.StateEvent{}, err
	}
	value := contract.StateEvent{Method: string(event.Kind)}
	switch event.Kind {
	case supervisor.StateSessionChanged:
		stream.application.opMu.Lock()
		cleanup := stream.application.invalidSessions[event.SessionID]
		stream.application.opMu.Unlock()
		value.SessionChanged = &contract.SessionChangedPayload{Session: toSessionResult(event.Snapshot), CleanupRequired: cleanup}
	case supervisor.StateSessionRemoved:
		value.SessionRemoved = &contract.SessionRemovedPayload{SessionID: string(event.SessionID), Revision: event.Revision}
	case supervisor.StateNetworkChanged:
		network := networkInterfacesResult(event.NetworkSnapshot, true)
		value.NetworkChanged = &network
	}
	data, err := contract.EncodeStateEvent(value)
	if err == nil {
		var owned contract.StateEvent
		owned, err = contract.DecodeStateEvent(data)
		if err == nil {
			if err := readModelContextError(ctx); err != nil {
				return contract.StateEvent{}, err
			}
			return owned, nil
		}
	}
	cause = fmt.Errorf("%w: %w", errStateProjection, err)
	stream.mu.Lock()
	if stream.cause == nil {
		stream.cause = cause
	}
	cause = stream.cause
	stream.mu.Unlock()
	_ = stream.Close()
	return contract.StateEvent{}, cause
}
