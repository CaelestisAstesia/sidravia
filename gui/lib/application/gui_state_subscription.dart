part of 'gui_controller.dart';

// GuiController owns the snapshot, timers and subscription for each connection.
extension _GuiStateSubscription on GuiController {
  Future<void> _refresh(int generation, SidraviaIpcClient client) async {
    try {
      final daemon = await client.daemonStatus();
      if (!_canContinue(generation) || !identical(_client, client)) return;
      final profiles = await client.profileList();
      if (!_canContinue(generation) || !identical(_client, client)) return;
      final configurations = await client.configurationList();
      if (!_canContinue(generation) || !identical(_client, client)) return;
      List<SessionSummary> sessions;
      NetworkInterfacesSnapshot? network;
      if (client is SidraviaStateClient) {
        if (_subscription == null) {
          final subscription = await (client as SidraviaStateClient)
              .subscribeStateEvents();
          if (!_canContinue(generation) || !identical(_client, client)) {
            await _closeQuietly(client);
            await subscription.close();
            return;
          }
          _subscription = subscription;
          sessions = subscription.initial.sessions;
          network = subscription.initial.network;
        } else {
          sessions = _snapshot!.sessions;
          network = _snapshot!.network;
        }
      } else {
        sessions = await client.sessionList();
        if (client is SidraviaNetworkClient) {
          network = await (client as SidraviaNetworkClient).networkInterfaces();
        }
      }
      if (!_canContinue(generation) || !identical(_client, client)) return;
      // Session IDs are local to a daemon instance. A stale reconnect to the
      // same PID retains its marker; only a complete new snapshot replaces it.
      if (_snapshot?.daemon.pid != daemon.pid) {
        _outdatedSessionId = null;
      }
      _snapshot = GuiSnapshot(
        daemon: daemon,
        profiles: profiles,
        configurations: configurations,
        sessions: sessions,
        network: network,
      );
      if (_subscription != null && _events == null) {
        _events = _subscription!.events.listen(
          (event) => _applyEvent(generation, client, event),
          onError: (Object error) =>
              unawaited(_invalidate(generation, client, error)),
          onDone: () => unawaited(
            _invalidate(
              generation,
              client,
              const IpcTransportException('ipc_disconnected'),
            ),
          ),
        );
      }
      if (!_canContinue(generation) || !identical(_client, client)) return;
      _retryAttempt = 0;
      _state = GuiConnectionState.ready;
      _failure = null;
      _notice = null;
      _scheduleRefresh(generation, client);
      _scheduleMaintenance(generation, client);
      _notify();
    } on Object catch (error) {
      if (!_current(generation) || !identical(_client, client)) return;
      await _invalidate(generation, client, error);
    }
  }

  void _scheduleRefresh(int generation, SidraviaIpcClient client) {
    if ((client is SidraviaStateClient && client is! SidraviaOverrideClient) ||
        !_canContinue(generation) ||
        !identical(_client, client) ||
        _state != GuiConnectionState.ready) {
      return;
    }
    _timer?.cancel();
    if (_exitRequested) {
      _timer = null;
      return;
    }
    // Session and network events do not invalidate the saved configuration.
    final delay =
        client is SidraviaStateClient && pollDelay < const Duration(seconds: 5)
        ? const Duration(seconds: 5)
        : pollDelay;
    _timer = Timer(delay, () {
      _timer = null;
      if (_exitRequested ||
          !_current(generation) ||
          !identical(_client, client)) {
        return;
      }
      if (_transition != null) {
        _scheduleRefresh(generation, client);
        return;
      }
      _transition = _poll(generation, client);
      _notify();
    });
  }

  Future<void> _poll(int generation, SidraviaIpcClient client) async {
    try {
      await _refresh(generation, client);
    } finally {
      if (_generation == generation) _transition = null;
      _notify();
    }
  }

  void _applyEvent(int generation, SidraviaIpcClient client, StateEvent event) {
    if (!_canContinue(generation) || !identical(_client, client)) return;
    final previous = _snapshot;
    if (previous == null) return;
    var sessions = previous.sessions;
    var network = previous.network;
    switch (event) {
      case SessionChanged(:final session):
        final old = sessions.where((s) => s.id == session.id).firstOrNull;
        if (old != null && session.revision < old.revision) return;
        if (old == null && sessions.length + 1 >= stateResourceCapacity) {
          unawaited(
            _invalidate(
              generation,
              client,
              const IpcTransportException('ipc_reconnect_needed'),
            ),
          );
          return;
        }
        sessions = [
          for (final s in sessions)
            if (s.id == session.id) session else s,
          if (old == null) session,
        ];
      case SessionRemoved(:final sessionId, :final revision):
        final old = sessions.where((s) => s.id == sessionId).firstOrNull;
        if (old != null && revision < old.revision) return;
        sessions = sessions.where((s) => s.id != sessionId).toList();
      case NetworkChanged(network: final changed):
        if (network != null && changed.revision <= network.revision) return;
        network = changed;
    }
    _snapshot = GuiSnapshot(
      daemon: previous.daemon,
      profiles: previous.profiles,
      configurations: previous.configurations,
      sessions: List.unmodifiable(sessions),
      network: network,
    );
    _notify();
  }
}
