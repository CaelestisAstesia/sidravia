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
  bool _disposed = false;
  int _generation = 0;
  GuiConnectionState _state = GuiConnectionState.bootstrapping;
  GuiBootstrapFailure? _failure;
  GuiSnapshot? _snapshot;
  GuiConnectionState get state => _state;
  GuiBootstrapFailure? get failure => _failure;
  GuiSnapshot? get snapshot => _snapshot;

  Future<void> start() => _transition ??= _start();
  Future<void> retry() => start();

  Future<void> _start() async {
    final generation = ++_generation;
    _timer?.cancel();
    final old = _client;
    _client = null;
    if (old != null) await old.close();
    if (!_current(generation)) return;
    _state = GuiConnectionState.bootstrapping;
    _failure = null;
    _notify();
    try {
      final boot = await bootstrapper.bootstrap();
      if (!_current(generation)) return;
      if (!boot.isSuccess) {
        _setFailure(boot.failure!);
        return;
      }
      final client = await connector(boot.value!);
      if (!_current(generation)) {
        await client.close();
        return;
      }
      _client = client;
      await _refresh(generation, client);
    } on Object {
      if (_current(generation)) _setFailure(GuiBootstrapFailure.failed);
    } finally {
      if (_generation == generation) _transition = null;
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
      _notify();
      _timer?.cancel();
      _timer = Timer(pollDelay, () {
        _timer = null;
        unawaited(_refresh(generation, client));
      });
    } on Object {
      if (!_current(generation) || !identical(_client, client)) return;
      _timer?.cancel();
      _timer = null;
      _client = null;
      await client.close();
      _state = _snapshot == null
          ? GuiConnectionState.failed
          : GuiConnectionState.stale;
      _failure = GuiBootstrapFailure.failed;
      _notify();
    }
  }

  bool _current(int generation) => !_disposed && generation == _generation;
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
    unawaited(_client?.close());
    super.dispose();
  }
}
