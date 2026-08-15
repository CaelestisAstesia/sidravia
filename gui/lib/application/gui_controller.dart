import 'dart:async';

import 'package:flutter/foundation.dart';
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
  });

  final GuiBootstrapper bootstrapper;
  final IpcConnector connector;
  final Duration pollDelay;
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

  GuiConnectionState get state => _state;
  GuiBootstrapFailure? get failure => _failure;
  GuiSnapshot? get snapshot => _snapshot;
  String? get notice => _notice;
  bool get busy => _userBusy;

  Future<void> start() {
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
  }) => _mutate(
    (client) => client.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
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
      if (!_current(generation)) return;
      if (!boot.isSuccess) {
        _setFailure(boot.failure!);
        return;
      }
      final client = await connector(boot.value!);
      if (!_current(generation)) {
        await _closeQuietly(client);
        return;
      }
      _client = client;
      await _refresh(generation, client);
    } on Object {
      if (_current(generation)) _setFailure(GuiBootstrapFailure.failed);
    } finally {
      if (_generation == generation) {
        _transition = null;
        _userBusy = false;
      }
      _notify();
    }
  }

  Future<bool> _mutate(
    Future<Object?> Function(SidraviaIpcClient) action,
  ) async {
    final current = _transition;
    if (current != null) {
      if (_userBusy) return false;
      await current;
    }
    if (_transition != null ||
        _state != GuiConnectionState.ready ||
        _client == null) {
      return false;
    }
    _userBusy = true;
    final completer = Completer<bool>();
    _transition = _runMutation(action, completer);
    _notify();
    return completer.future;
  }

  Future<void> _runMutation(
    Future<Object?> Function(SidraviaIpcClient) action,
    Completer<bool> completer,
  ) async {
    final generation = _generation;
    final client = _client!;
    _timer?.cancel();
    _timer = null;
    _notice = null;
    try {
      await action(client);
      if (!_current(generation) || !identical(_client, client)) {
        completer.complete(false);
        return;
      }
      await _refresh(generation, client);
      completer.complete(_state == GuiConnectionState.ready);
    } on IpcRequestFailure catch (error) {
      if (_current(generation) && identical(_client, client)) {
        _notice = _guidance(error.code);
        _scheduleRefresh(generation, client);
        _notify();
      }
      completer.complete(false);
    } on Object {
      if (_current(generation) && identical(_client, client)) {
        await _invalidate(generation, client);
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
      final profiles = await client.profileList();
      final configurations = await client.configurationList();
      final sessions = await client.sessionList();
      if (!_current(generation) || !identical(_client, client)) return;
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
    } on Object {
      if (!_current(generation) || !identical(_client, client)) return;
      await _invalidate(generation, client);
    }
  }

  void _scheduleRefresh(int generation, SidraviaIpcClient client) {
    _timer?.cancel();
    _timer = Timer(pollDelay, () {
      _timer = null;
      if (_transition != null ||
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

  Future<void> _invalidate(int generation, SidraviaIpcClient client) async {
    _timer?.cancel();
    _timer = null;
    _client = null;
    await _closeQuietly(client);
    if (!_current(generation)) return;
    _state = _snapshot == null
        ? GuiConnectionState.failed
        : GuiConnectionState.stale;
    _failure = GuiBootstrapFailure.failed;
    _notice = null;
    _notify();
  }

  String _guidance(String code) => switch (code) {
    'profile_not_found' => '学校配置已不存在，请刷新。',
    'configuration_conflict' => '登录配置已存在，请刷新。',
    'insecure_storage_confirmation_required' => '凭据存储未受保护，密码未保存。',
    'session_state_conflict' => '当前状态无法执行此操作。',
    'session_active_conflict' => '已有认证会话，请刷新。',
    _ => '操作失败，请刷新后重试。',
  };

  bool _current(int generation) => !_disposed && generation == _generation;

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
