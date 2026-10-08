import 'package:sidravia_gui/application/gui_operation.dart';
import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:sidravia_gui/application/gui_connection_state.dart';
import 'package:sidravia_gui/application/gui_snapshot.dart';
import 'package:sidravia_gui/application/gui_network_diagnosis.dart';

import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/web_socket_ipc_client.dart';
import 'package:sidravia_gui/ipc/state_models.dart';
import 'package:sidravia_gui/ipc/state_subscription.dart';

// Retain the controller entrypoint's public lifecycle type for tool clients.
export 'gui_connection_state.dart';

typedef IpcConnector = Future<SidraviaIpcClient> Function(
  GuiBootstrap bootstrap,
);

class GuiController extends ChangeNotifier {
  GuiController({
    required this.bootstrapper,
    this.connector = WebSocketIpcClient.connect,
    this.pollDelay = const Duration(seconds: 3),
    this.stopTimeout = const Duration(seconds: 4),
    this.reconnectDelay = const Duration(seconds: 1),
  });

  final GuiBootstrapper bootstrapper;
  final IpcConnector connector;
  final Duration pollDelay;
  final Duration stopTimeout;
  final Duration reconnectDelay;
  StateSubscription? _subscription;
  StreamSubscription<StateEvent>? _events;
  Future<void>? _releasing;
  Future<void>? _closed;
  int _retryAttempt = 0;
  bool _exitRequested = false;
  SidraviaIpcClient? _client;
  Timer? _timer;
  Future<void>? _transition;
  bool _userBusy = false;
  bool _disposed = false;
  int _generation = 0;
  GuiConnectionState _state = GuiConnectionState.bootstrapping;
  GuiBootstrapFailure? _failure;
  GuiSnapshot? _snapshot;
  String? _notice;
  String? _outdatedSessionId;
  NetworkDiagnosis? _diagnosis;
  String? _diagnosisTargetLabel;
  String? _diagnosisError;
  bool _diagnosisVisible = false;
  bool _diagnosisBusy = false;
  bool _diagnosisStale = true;
  bool _diagnosisPending = false;
  bool _diagnosisProbe = false;
  bool _diagnosisScheduled = false;
  int _diagnosisRevision = 0;

  NetworkDiagnosis? get diagnosis => _diagnosis;
  bool get diagnosisBusy => _diagnosisBusy;
  bool get diagnosisStale =>
      _diagnosisStale || state != GuiConnectionState.ready;
  String? get diagnosisError => _diagnosisError;
  String? get diagnosisTargetLabel => _diagnosisTargetLabel;

  (String?, String?)? get _diagnosisTarget {
    // Selection does not require runtime dependencies or saved credentials.
    final caps = GuiCapabilities(state: state, snapshot: snapshot, busy: false);
    if (caps.capability != GuiCapabilityState.manageable) return null;
    final session = caps.retainedSession;
    return session != null
        ? (null, session.id)
        : (caps.configuration!.id, null);
  }

  String? get diagnosisUnavailable {
    if (state != GuiConnectionState.ready || _exitRequested) {
      return '通信尚未就绪，网络诊断暂不可用。';
    }
    if (_client is! SidraviaNetworkClient) {
      return '当前客户端不提供网络诊断；离线预览没有真实网络观察。';
    }
    if (_diagnosisTarget == null) {
      return '需要一份连接配置及明确的会话关系；请先检查连接配置。';
    }
    return null;
  }

  bool get canDiagnose =>
      _diagnosisVisible &&
      diagnosisUnavailable == null &&
      !busy &&
      !_diagnosisBusy;

  void setDiagnosisVisible(bool visible) {
    if (_disposed || _diagnosisVisible == visible) return;
    _diagnosisVisible = visible;
    ++_diagnosisRevision;
    _diagnosisStale = true;
    _diagnosisPending = visible;
    _diagnosisProbe = false;
    if (visible) _notify();
  }

  void refreshDiagnosis({bool probe = false}) {
    if (!canDiagnose) return;
    _diagnosisPending = true;
    _diagnosisProbe = probe;
    _diagnosisStale = true;
    ++_diagnosisRevision;
    _notify();
  }

  void _invalidateDiagnosis() {
    ++_diagnosisRevision;
    _diagnosisStale = true;
    _diagnosisPending = _diagnosisVisible;
    // Network events and recovery never repeat an explicit probe.
    _diagnosisProbe = false;
  }

  void _scheduleDiagnosis() {
    if (!_diagnosisPending ||
        !_diagnosisVisible ||
        _diagnosisScheduled ||
        _transition != null ||
        diagnosisUnavailable != null ||
        _disposed ||
        _exitRequested) {
      return;
    }
    _diagnosisScheduled = true;
    scheduleMicrotask(() {
      _diagnosisScheduled = false;
      if (!_diagnosisPending ||
          !_diagnosisVisible ||
          _transition != null ||
          diagnosisUnavailable != null ||
          _disposed ||
          _exitRequested) {
        return;
      }
      final target = _diagnosisTarget!;
      final probe = _diagnosisProbe;
      _diagnosisPending = false;
      _diagnosisProbe = false;
      _transition = _runDiagnosis(_generation, _client!, target, probe);
    });
  }

  Future<void> _runDiagnosis(
    int generation,
    SidraviaIpcClient client,
    (String?, String?) target,
    bool probe,
  ) async {
    final revision = _diagnosisRevision;
    _diagnosisBusy = true;
    _diagnosisError = null;
    _timer?.cancel();
    _timer = null;
    _notify();
    try {
      final result = await (client as SidraviaNetworkClient).networkDiagnose(
        configurationId: target.$1,
        sessionId: target.$2,
        probe: probe,
      );
      if (_canContinue(generation) &&
          identical(_client, client) &&
          _diagnosisVisible &&
          revision == _diagnosisRevision &&
          target == _diagnosisTarget) {
        _diagnosis = result;
        _diagnosisTargetLabel = target.$2 != null
            ? '会话 ${target.$2}'
            : '配置 ${target.$1}';
        _diagnosisStale = false;
      }
    } on IpcRequestFailure catch (error) {
      if (_canContinue(generation) &&
          identical(_client, client) &&
          _diagnosisVisible &&
          revision == _diagnosisRevision) {
        _diagnosisError =
            '网络诊断失败：${diagnosisErrorMessage(error.code)} (${error.code})';
      }
    } on Object catch (error) {
      if (_current(generation) && identical(_client, client)) {
        await _invalidate(generation, client, error);
      }
    } finally {
      _diagnosisBusy = false;
      if (_generation == generation) {
        _transition = null;
        _scheduleRefresh(generation, client);
      }
      _notify();
    }
  }

  bool get sessionNeedsReset =>
      _snapshot?.sessions.any((s) => s.cleanupRequired) == true ||
      _outdatedSessionId != null &&
          _snapshot?.sessions.any((s) => s.id == _outdatedSessionId) == true;

  GuiConnectionState get state => _state;
  GuiBootstrapFailure? get failure => _failure;
  GuiSnapshot? get snapshot => _snapshot;
  String? get notice => _notice;
  bool get busy => _userBusy;
  GuiCapabilities get capabilities =>
      GuiCapabilities(state: _state, snapshot: _snapshot, busy: _userBusy);

  ConnectionPresentation get connectionPresentation =>
      ConnectionPresentation.project(
        capabilities,
        sessionNeedsReset: sessionNeedsReset,
        notice: notice,
      );

  Future<bool> performConnectionAction(GuiConnectionAction action) {
    if (action.kind == GuiConnectionActionKind.retryIpc) return _retryAction();
    if (!const {
      GuiConnectionActionKind.start,
      GuiConnectionActionKind.reconnect,
      GuiConnectionActionKind.stop,
    }.contains(action.kind)) {
      return Future.value(false);
    }
    final id = action.targetId;
    if (id == null) return Future.value(false);
    return _mutate(
      (client, allow) => switch (action.kind) {
        GuiConnectionActionKind.start => client.sessionStartConfiguration(id),
        GuiConnectionActionKind.reconnect => client.sessionRestart(id),
        GuiConnectionActionKind.stop => client.sessionStop(id),
        _ => throw StateError('Non-mutation connection action'),
      },
      allowed: (_) {
        final p = connectionPresentation;
        bool matches(GuiConnectionAction current) =>
            current.kind == action.kind && current.targetId == id;
        return (p.primaryEnabled && matches(p.primaryAction)) ||
            (p.secondaryEnabled && matches(p.secondaryAction));
      },
    );
  }

  Future<bool> _retryAction() async {
    if (busy ||
        (state != GuiConnectionState.failed &&
            state != GuiConnectionState.stale)) {
      return false;
    }
    await retry();
    return state == GuiConnectionState.ready;
  }

  Future<void> start() {
    if (_exitRequested || _disposed) return Future<void>.value();
    final current = _transition;
    if (current != null) return current;
    _userBusy = true;
    return _transition = _start();
  }

  Future<void> retry() => start();

  Future<bool> createConfiguration({
    required String institutionProfileId,
    required String username,
    required String password,
    InsecureStorageConfirmation? onInsecureStorageConfirmation,
  }) => _mutate(
    (client, allow) => client.configurationCreate(
      networkBindingPolicy: const NetworkBindingPolicy.automatic(),
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,

      autoLogin: false,
      autoReconnect: true,
      allowInsecureStorage: allow,
    ),
    operation: GuiOperation.createConfiguration,
    onInsecureStorageConfirmation: onInsecureStorageConfirmation,
    allowed: (caps) => caps.canCreate,
  );

  Future<bool> updateConfiguration({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    InsecureStorageConfirmation? onInsecureStorageConfirmation,
  }) => _mutate(
    (client, allow) => client.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
      allowInsecureStorage: allow,
    ),
    operation: password == null
        ? GuiOperation.updateConfiguration
        : GuiOperation.updatePassword,
    onInsecureStorageConfirmation: onInsecureStorageConfirmation,
    allowed: (caps) => caps.matchesConfiguration(configurationId),
  );

  Future<bool> setPassword({
    required String configurationId,
    required String password,
    InsecureStorageConfirmation? onInsecureStorageConfirmation,
  }) => _mutate(
    (client, allow) => client.configurationSetPassword(
      configurationId: configurationId,
      password: password,
      allowInsecureStorage: allow,
    ),
    operation: GuiOperation.updatePassword,
    onInsecureStorageConfirmation: onInsecureStorageConfirmation,
    allowed: (caps) => caps.matchesConfiguration(configurationId),
  );

  Future<bool> startConfiguration(String configurationId) => _mutate(
    (client, allow) => client.sessionStartConfiguration(configurationId),
    allowed: (caps) =>
        caps.canConnect &&
        caps.matchesConfiguration(configurationId) &&
        !sessionNeedsReset,
  );
  Future<bool> stopSession(String sessionId) => _mutate(
    (client, allow) => client.sessionStop(sessionId),
    allowed: (caps) => caps.canStop && caps.matchesSession(sessionId),
  );
  Future<bool> ensureSessionRunning(String sessionId) => _mutate(
    (client, allow) => client.sessionEnsureRunning(sessionId),
    allowed: (caps) =>
        caps.canConnect && caps.matchesSession(sessionId) && !sessionNeedsReset,
  );
  Future<bool> restartSession(String sessionId) => _mutate(
    (client, allow) => client.sessionRestart(sessionId),
    allowed: (caps) =>
        caps.canConnect && caps.matchesSession(sessionId) && !sessionNeedsReset,
  );

  Future<bool> resetSession(String sessionId) => _mutate(
    (client, allow) => client.sessionRemove(sessionId),
    fallback: '重置会话失败，请重试。',
    allowed: (capabilities) =>
        capabilities.canResetSession &&
        capabilities.retainedSession?.id == sessionId,
  );

  Future<bool> deleteConfiguration(
    String configurationId, {
    InsecureStorageConfirmation? onInsecureStorageConfirmation,
  }) => _mutate(
    (client, allow) => client.configurationRemove(
      configurationId,
      allowInsecureStorage: allow,
    ),
    operation: GuiOperation.deleteConfiguration,
    onInsecureStorageConfirmation: onInsecureStorageConfirmation,
    fallback: '删除配置失败，请重试。',
    allowed: (capabilities) =>
        capabilities.canDeleteConfiguration &&
        capabilities.configuration?.id == configurationId,
  );

  Future<bool> setAutoLogin({
    required String configurationId,
    required bool autoLogin,
    InsecureStorageConfirmation? onInsecureStorageConfirmation,
  }) => _mutate(
    (client, allow) => client.configurationSetAutoLogin(
      configurationId: configurationId,
      autoLogin: autoLogin,
      allowInsecureStorage: allow,
    ),
    fallback: '自动登录设置失败，请重试。',
    operation: GuiOperation.autoLogin,
    onInsecureStorageConfirmation: onInsecureStorageConfirmation,
    allowed: (capabilities) =>
        capabilities.canEditAutoLogin &&
        capabilities.configuration?.id == configurationId,
  );

  Future<bool> setAutoReconnect({
    required String configurationId,
    required bool autoReconnect,
    InsecureStorageConfirmation? onInsecureStorageConfirmation,
  }) => _mutate(
    (client, allow) => client.configurationSetAutoReconnect(
      configurationId: configurationId,
      autoReconnect: autoReconnect,
      allowInsecureStorage: allow,
    ),
    fallback: '自动重连设置失败，请重试。',
    operation: GuiOperation.autoReconnect,
    onInsecureStorageConfirmation: onInsecureStorageConfirmation,
    allowed: (capabilities) =>
        capabilities.canEditAutoReconnect &&
        capabilities.configuration?.id == configurationId,
  );

  /// Graceful exit requested only by the explicit tray path.
  ///
  /// Serializes with an in-flight poll/mutation, cancels future polling and
  /// submits the existing typed `daemon.stop` at most once within a bounded
  /// wait. Business, transport, protocol and timeout failures never prevent
  /// final native destruction; this future always completes.
  Future<bool> exitAndDisconnect() async {
    if (_exitRequested) return false;
    _exitRequested = true;
    _timer?.cancel();
    _timer = null;
    final deadline = DateTime.now().add(stopTimeout);
    try {
      final current = _transition;
      if (current != null) {
        try {
          final remaining = deadline.difference(DateTime.now());
          if (remaining <= Duration.zero) return false;
          await current.timeout(remaining);
        } on Object {
          return false;
        }
      }
      _timer?.cancel();
      _timer = null;
      if (_disposed) return false;
      final generation = _generation;
      final client = _client;
      if (client is! SidraviaDesktopClient) return false;
      try {
        final remaining = deadline.difference(DateTime.now());
        if (remaining <= Duration.zero) return false;
        await client.daemonStop().timeout(remaining);
        return !_disposed && generation == _generation;
      } on Object {
        return false;
      }
    } finally {
      final releasing = _releaseResources();
      final remaining = deadline.difference(DateTime.now());
      if (remaining > Duration.zero) {
        try {
          await releasing.timeout(remaining);
        } on Object {
          /* Owned by close(). */
        }
      }
    }
  }

  Future<void> _start() async {
    final generation = ++_generation;
    _invalidateDiagnosis();
    try {
      _timer?.cancel();
      _timer = null;
      await _releaseResources();
      if (!_canContinue(generation)) return;
      _state = _snapshot == null
          ? GuiConnectionState.bootstrapping
          : GuiConnectionState.stale;
      _failure = null;
      _notice = null;
      _notify();
      final boot = await bootstrapper.bootstrap();
      if (!_canContinue(generation)) return;
      if (!boot.isSuccess) {
        _setFailure(boot.failure!);
        return;
      }
      final client = await connector(boot.value!);
      if (!_canContinue(generation)) {
        await _closeQuietly(client);
        return;
      }
      _client = client;
      await _refresh(generation, client);
    } on Object catch (error) {
      if (_current(generation)) _setFailure(_ipcFailure(error));
    } finally {
      if (_generation == generation) {
        _transition = null;
        _userBusy = false;
        _scheduleReconnect();
      }
      _notify();
    }
  }

  Future<bool> _mutate(
    Future<Object?> Function(SidraviaIpcClient, bool) action, {
    String fallback = '操作失败，请刷新后重试。',
    GuiOperation operation = GuiOperation.connection,
    InsecureStorageConfirmation? onInsecureStorageConfirmation,
    bool allowInsecureStorage = false,
    bool Function(GuiCapabilities capabilities)? allowed,
  }) async {
    if (_exitRequested || _disposed) return false;
    final current = _transition;
    if (current != null) {
      if (_userBusy) return false;
      await current;
    }
    if (_transition != null ||
        _exitRequested ||
        _state != GuiConnectionState.ready ||
        _client == null) {
      return false;
    }
    if (allowed != null && !allowed(capabilities)) return false;
    _userBusy = true;
    final generation = _generation;
    final completer = Completer<GuiMutationResult>();
    _transition = _runMutation(
      action,
      completer,
      fallback,
      operation,
      allowInsecureStorage,
    );
    _notify();
    final result = await completer.future;
    if (result.errorCode != 'insecure_storage_confirmation_required' ||
        allowInsecureStorage ||
        onInsecureStorageConfirmation == null) {
      return result.succeeded;
    }
    final confirmed = await onInsecureStorageConfirmation(operation);
    if (!confirmed || !_canContinue(generation)) return false;
    return _mutate(
      action,
      fallback: fallback,
      operation: operation,
      allowed: allowed,
      allowInsecureStorage: true,
    );
  }

  Future<void> _runMutation(
    Future<Object?> Function(SidraviaIpcClient, bool) action,
    Completer<GuiMutationResult> completer,
    String fallback,
    GuiOperation operation,
    bool allowInsecureStorage,
  ) async {
    final generation = _generation;
    final client = _client!;
    _timer?.cancel();
    _timer = null;
    _notice = null;
    _invalidateDiagnosis();
    try {
      await action(client, allowInsecureStorage);
      if (!_canContinue(generation) || !identical(_client, client)) {
        completer.complete(const GuiMutationResult(false));
        return;
      }
      await _refresh(generation, client);
      completer.complete(GuiMutationResult(_state == GuiConnectionState.ready));
    } on IpcRequestFailure catch (error) {
      if (_current(generation) && identical(_client, client)) {
        if (error.code == 'configuration_session_invalidation_failed') {
          _outdatedSessionId = capabilities.retainedSession?.id;
          await _refresh(generation, client);
        }
        _notice = _guidance(operation, error.code, fallback);
        _scheduleRefresh(generation, client);
        _notify();
      }
      completer.complete(GuiMutationResult(false, error.code));
    } on Object catch (error) {
      if (_current(generation) && identical(_client, client)) {
        await _invalidate(generation, client, error);
      }
      completer.complete(const GuiMutationResult(false));
    } finally {
      if (_generation == generation) {
        _transition = null;
        _userBusy = false;
      }
      _notify();
    }
  }

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
      }
      if (!_canContinue(generation) || !identical(_client, client)) return;
      // Session IDs are local to a daemon instance. A stale reconnect to the
      // same PID retains its marker; only a complete new snapshot replaces it.
      if (_snapshot?.daemon.pid != daemon.pid) {
        _outdatedSessionId = null;
      }
      _invalidateDiagnosis();
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
      _notify();
    } on Object catch (error) {
      if (!_current(generation) || !identical(_client, client)) return;
      await _invalidate(generation, client, error);
    }
  }

  void _scheduleRefresh(int generation, SidraviaIpcClient client) {
    if (client is SidraviaStateClient ||
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
    _timer = Timer(pollDelay, () {
      _timer = null;
      if (_transition != null ||
          _exitRequested ||
          !_current(generation) ||
          !identical(_client, client)) {
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
    _invalidateDiagnosis();
    _snapshot = GuiSnapshot(
      daemon: previous.daemon,
      profiles: previous.profiles,
      configurations: previous.configurations,
      sessions: List.unmodifiable(sessions),
      network: network,
    );
    _notify();
  }

  Future<void> _invalidate(
    int generation,
    SidraviaIpcClient client,
    Object error,
  ) async {
    if (!_current(generation) || !identical(_client, client)) return;
    _timer?.cancel();
    _timer = null;
    _state = _snapshot == null
        ? GuiConnectionState.failed
        : GuiConnectionState.stale;
    _invalidateDiagnosis();
    _failure = _ipcFailure(error);
    _notice = null;
    // Detach synchronously before awaiting cancellation: duplicate terminal
    // callbacks and any completion of an old RPC are now inert.
    final releasing = _releaseResources();
    _notify();
    await releasing;
    if (_canContinue(generation)) _scheduleReconnect();
  }

  void _scheduleReconnect() {
    if (_disposed ||
        _exitRequested ||
        _client != null ||
        _failure?.code == 'unsupported_platform' ||
        _timer != null) {
      return;
    }
    final delay = Duration(
      microseconds:
          (reconnectDelay.inMicroseconds * (1 << _retryAttempt.clamp(0, 5)))
              .clamp(1000, 30000000),
    );
    _timer = Timer(delay, () {
      _timer = null;
      if (_disposed || _exitRequested) return;
      if (_transition != null) {
        _scheduleReconnect();
        return;
      }
      _retryAttempt = (_retryAttempt + 1).clamp(0, 5);
      unawaited(start());
    });
  }

  Future<void> _releaseResources() {
    final client = _client;
    final events = _events;
    final subscription = _subscription;
    _client = null;
    _events = null;
    _subscription = null;
    final previous = _releasing;
    return _releasing = () async {
      await previous;
      // Close the transport first, aborting any pending RPC. Cancellation
      // cannot then compete with that RPC for the sole request slot.
      await _closeQuietly(client);
      try {
        await events?.cancel();
      } on Object {
        /* Closure is final. */
      }
      try {
        await subscription?.close();
      } on Object {
        /* Closure is final. */
      }
    }();
  }

  GuiBootstrapFailure _ipcFailure(Object error) => guiIpcFailure(
    error is IpcTransportException
        ? error.code
        : error is IpcRequestFailure
        ? 'ipc_business_rejected'
        : 'ipc_protocol_error',
  );

  String _guidance(GuiOperation operation, String code, String fallback) =>
      switch (code) {
        'configuration_session_invalidation_failed' =>
          '配置已提交，旧会话清理未完成；请在连接配置中移除旧会话后重试。',
        'profile_not_found' => '学校配置已不存在，请刷新。',
        'configuration_conflict' => '登录配置已存在，请刷新。',
        'insecure_storage_confirmation_required' =>
          operation.insecureStorageGuidance,
        'session_state_conflict' => '当前状态无法执行此操作。',
        'session_active_conflict' => '已有认证会话，请刷新。',
        'session_not_found' => '会话已不存在，请刷新。',
        'configuration_not_found' => '登录配置已不存在，请刷新。',
        'configuration_auto_login_conflict' => '其他配置已启用自动登录，请先处理。',
        _ => fallback,
      };

  bool _current(int generation) => !_disposed && generation == _generation;

  bool _canContinue(int generation) => !_exitRequested && _current(generation);

  Future<void> _closeQuietly(SidraviaIpcClient? client) async {
    if (client == null) return;
    try {
      await client.close();
    } on Object {
      // A close failure must not escape the controller lifecycle boundary.
    }
  }

  void _setFailure(GuiBootstrapFailure failure) {
    _failure = failure;
    _state = _snapshot != null
        ? GuiConnectionState.stale
        : failure.code == 'unsupported_platform'
        ? GuiConnectionState.unsupported
        : GuiConnectionState.failed;
    _notify();
  }

  void _notify() {
    if (!_disposed) {
      _scheduleDiagnosis();
      notifyListeners();
    }
  }

  /// Await owned IPC resources and any late bootstrap/connector completion.
  Future<void> close() {
    if (_closed != null) return _closed!;
    _exitRequested = true;
    ++_generation;
    _timer?.cancel();
    _timer = null;
    final transition = _transition;
    final releasing = _releaseResources();
    return _closed = () async {
      await releasing;
      await transition;
      await _releaseResources();
    }();
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    unawaited(close());
    super.dispose();
  }
}
