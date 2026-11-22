part of 'web_socket_ipc_client.dart';

class _OwnedStateSubscription implements StateSubscription {
  _OwnedStateSubscription(this.client, this.initial) {
    for (final session in initial.sessions) {
      _live[session.id] = (session.revision, session.cleanupRequired);
    }
    _networkRevision = initial.network.revision;
    _events = StreamController<StateEvent>(
      sync: true,
      onListen: _schedule,
      onResume: _schedule,
      onCancel: close,
    );
  }
  final WebSocketIpcClient client;
  @override
  final StateBootstrap initial;
  late final StreamController<StateEvent> _events;
  @override
  Stream<StateEvent> get events => _events.stream;
  final _pending = <(String, String), StateEvent>{};
  final _live = <String, (BigInt, bool)>{};
  final _terminal = <String>{};
  late BigInt _networkRevision;
  bool canceling = false, _ended = false, _scheduled = false;
  Object? _failure;
  Future<void>? _closeFuture;

  void accept(StateEvent event) {
    if (_ended || canceling) return;
    final (String, String) key;
    switch (event) {
      case SessionChanged(:final session):
        if (_terminal.contains(session.id)) return;
        final cursor = _live[session.id];
        if (cursor != null &&
            (session.revision < cursor.$1 ||
                session.revision == cursor.$1 &&
                    session.cleanupRequired == cursor.$2)) {
          return;
        }
        if (cursor == null && _live.length + 1 >= stateResourceCapacity) {
          _overflow();
          return;
        }
        _live[session.id] = (session.revision, session.cleanupRequired);
        key = ('session', session.id);
      case SessionRemoved(:final sessionId, :final revision):
        if (_terminal.contains(sessionId)) return;
        final cursor = _live[sessionId];
        if (cursor != null && revision < cursor.$1) return;
        if (_terminal.length >= stateResourceCapacity) {
          _overflow();
          return;
        }
        _terminal.add(sessionId);
        _live.remove(sessionId);
        key = ('session', sessionId);
      case NetworkChanged(:final network):
        if (network.revision <= _networkRevision) return;
        _networkRevision = network.revision;
        key = ('network', '');
    }
    if (!_pending.containsKey(key) &&
        _pending.length >= stateResourceCapacity) {
      _overflow();
      return;
    }
    // Assignment retains insertion order: one latest value with FIFO fairness.
    _pending[key] = event;
    _schedule();
  }

  void _overflow() =>
      client._discontinue(const IpcTransportException('ipc_reconnect_needed'));
  void clearPending() => _pending.clear();
  void _schedule() {
    if (_scheduled ||
        !_events.hasListener ||
        _events.isPaused ||
        _events.isClosed) {
      return;
    }
    _scheduled = true;
    scheduleMicrotask(_flush);
  }

  void _flush() {
    _scheduled = false;
    while (_events.hasListener && !_events.isPaused && !_events.isClosed) {
      if (_ended) {
        final failure = _failure;
        _failure = null;
        if (failure != null) _events.addError(failure);
        unawaited(_events.close());
        return;
      }
      if (canceling || _pending.isEmpty) return;
      final key = _pending.keys.first;
      final event = _pending.remove(key)!;
      // sync controller cannot silently build an async queue behind a pause.
      _events.add(event);
    }
  }

  void end([Object? failure]) {
    if (_ended) return;
    _ended = true;
    canceling = true;
    clearPending();
    _live.clear();
    _terminal.clear();
    _failure = failure;
    _schedule();
  }

  @override
  Future<void> close() {
    if (_ended) return Future.value();
    return _closeFuture ??= client._unsubscribe(this);
  }
}
