import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/web_socket_ipc_client.dart';

enum GuiConnectionState { bootstrapping, ready, stale, failed, unsupported }

typedef IpcConnector = Future<SidraviaIpcClient> Function(
  GuiBootstrap bootstrap,
);

class GuiController extends ChangeNotifier {
  GuiController({
    required this.bootstrapper,
    this.connector = WebSocketIpcClient.connect,
    this.pollDelay = const Duration(seconds: 3),
    this.stopTimeout = const Duration(seconds: 4),
  });

  final GuiBootstrapper bootstrapper;
  final IpcConnector connector;
  final Duration pollDelay;
  final Duration stopTimeout;
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
  bool get sessionNeedsReset =>
      _outdatedSessionId != null &&
      _snapshot?.sessions.any((s) => s.id == _outdatedSessionId) == true;

  GuiConnectionState get state => _state;
  GuiBootstrapFailure? get failure => _failure;
  GuiSnapshot? get snapshot => _snapshot;
  String? get notice => _notice;
  bool get busy => _userBusy;
  GuiCapabilities get capabilities =>
      GuiCapabilities(state: _state, snapshot: _snapshot, busy: _userBusy);

  Future<void> start() {
    if (_exitRequested) return Future<void>.value();
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
  }) => _mutate(
    (client) => client.configurationCreate(
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
    ),
  );

  Future<bool> updateConfiguration({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    bool allowInsecureStorage = false,
  }) => _mutate(
    (client) => client.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
      allowInsecureStorage: allowInsecureStorage,
    ),
  );

  Future<bool> setPassword({
    required String configurationId,
    required String password,
  }) => _mutate(
    (client) => client.configurationSetPassword(
      configurationId: configurationId,
      password: password,
    ),
  );

  Future<bool> startConfiguration(String configurationId) =>
      _mutate((client) => client.sessionStartConfiguration(configurationId));

  Future<bool> stopSession(String sessionId) =>
      _mutate((client) => client.sessionStop(sessionId));

  Future<bool> ensureSessionRunning(String sessionId) =>
      _mutate((client) => client.sessionEnsureRunning(sessionId));

  Future<bool> restartSession(String sessionId) =>
      _mutate((client) => client.sessionRestart(sessionId));

  Future<bool> resetSession(String sessionId) => _mutate(
    (client) => client.sessionRemove(sessionId),
    fallback: '重置会话失败，请重试。',
    allowed: (capabilities) =>
        capabilities.canResetSession &&
        capabilities.retainedSession?.id == sessionId,
  );

  Future<bool> deleteConfiguration(String configurationId) => _mutate(
    (client) => client.configurationRemove(configurationId),
    fallback: '删除配置失败，请重试。',
    allowed: (capabilities) =>
        capabilities.canDeleteConfiguration &&
        capabilities.configuration?.id == configurationId,
  );

  Future<bool> setAutoLogin({
    required String configurationId,
    required bool autoLogin,
  }) => _mutate(
    (client) => client.configurationSetAutoLogin(
      configurationId: configurationId,
      autoLogin: autoLogin,
    ),
    fallback: '自动登录设置失败，请重试。',
    allowed: (capabilities) =>
        capabilities.canEditAutoLogin &&
        capabilities.configuration?.id == configurationId,
  );

  Future<bool> setAutoReconnect({
    required String configurationId,
    required bool autoReconnect,
  }) => _mutate(
    (client) => client.configurationSetAutoReconnect(
      configurationId: configurationId,
      autoReconnect: autoReconnect,
    ),
    fallback: '自动重连设置失败，请重试。',
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
  }

  Future<void> _start() async {
    final generation = ++_generation;
    try {
      _timer?.cancel();
      final old = _client;
      _client = null;
      await _closeQuietly(old);
      if (!_current(generation)) return;
      _state = GuiConnectionState.bootstrapping;
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
      }
      _notify();
    }
  }

  Future<bool> _mutate(
    Future<Object?> Function(SidraviaIpcClient) action, {
    String fallback = '操作失败，请刷新后重试。',
    bool Function(GuiCapabilities capabilities)? allowed,
  }) async {
    if (_exitRequested) return false;
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
    final completer = Completer<bool>();
    _transition = _runMutation(action, completer, fallback);
    _notify();
    return completer.future;
  }

  Future<void> _runMutation(
    Future<Object?> Function(SidraviaIpcClient) action,
    Completer<bool> completer,
    String fallback,
  ) async {
    final generation = _generation;
    final client = _client!;
    _timer?.cancel();
    _timer = null;
    _notice = null;
    try {
      await action(client);
      if (!_canContinue(generation) || !identical(_client, client)) {
        completer.complete(false);
        return;
      }
      await _refresh(generation, client);
      completer.complete(_state == GuiConnectionState.ready);
    } on IpcRequestFailure catch (error) {
      if (_current(generation) && identical(_client, client)) {
        if (error.code == 'configuration_session_invalidation_failed') {
          _outdatedSessionId = capabilities.retainedSession?.id;
          await _refresh(generation, client);
        }
        _notice = _guidance(error.code, fallback);
        _scheduleRefresh(generation, client);
        _notify();
      }
      completer.complete(false);
    } on Object catch (error) {
      if (_current(generation) && identical(_client, client)) {
        await _invalidate(generation, client, error);
      }
      completer.complete(false);
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
      final sessions = await client.sessionList();
      if (!_canContinue(generation) || !identical(_client, client)) return;
      _snapshot = GuiSnapshot(
        daemon: daemon,
        profiles: profiles,
        configurations: configurations,
        sessions: sessions,
      );
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

  Future<void> _invalidate(
    int generation,
    SidraviaIpcClient client,
    Object error,
  ) async {
    _timer?.cancel();
    _timer = null;
    _client = null;
    await _closeQuietly(client);
    if (!_current(generation)) return;
    _state = _snapshot == null
        ? GuiConnectionState.failed
        : GuiConnectionState.stale;
    _failure = _ipcFailure(error);
    _notice = null;
    _notify();
  }

  GuiBootstrapFailure _ipcFailure(Object error) => guiIpcFailure(
    error is IpcTransportException
        ? error.code
        : error is IpcRequestFailure
        ? 'ipc_business_rejected'
        : 'ipc_protocol_error',
  );

  String _guidance(String code, String fallback) => switch (code) {
    'configuration_session_invalidation_failed' =>
      '配置已提交，旧会话清理未完成；请在连接配置中移除旧会话后重试。',
    'profile_not_found' => '学校配置已不存在，请刷新。',
    'configuration_conflict' => '登录配置已存在，请刷新。',
    'insecure_storage_confirmation_required' => '凭据存储未受保护，密码未保存。',
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
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    ++_generation;
    _timer?.cancel();
    final client = _client;
    _client = null;
    unawaited(_closeQuietly(client));
    super.dispose();
  }
}
