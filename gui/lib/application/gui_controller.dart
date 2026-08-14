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
  bool _disposed = false;
  bool _refreshing = false;
  GuiConnectionState _state = GuiConnectionState.bootstrapping;
  GuiBootstrapFailure? _failure;
  GuiSnapshot? _snapshot;

  GuiConnectionState get state => _state;
  GuiBootstrapFailure? get failure => _failure;
  GuiSnapshot? get snapshot => _snapshot;

  Future<void> start() async {
    _timer?.cancel();
    _state = GuiConnectionState.bootstrapping;
    _failure = null;
    _snapshot = null;
    _notify();
    final result = await bootstrapper.bootstrap();
    if (_disposed) return;
    if (!result.isSuccess) {
      _setFailure(result.failure!);
      return;
    }
    try {
      _client = await connector(result.value!);
    } on Object {
      if (!_disposed) _setFailure(GuiBootstrapFailure.failed);
      return;
    }
    await _refresh();
  }

  Future<void> retry() async {
    await _client?.close();
    _client = null;
    await start();
  }

  Future<void> _refresh() async {
    if (_disposed || _refreshing || _client == null) return;
    _refreshing = true;
    try {
      final daemon = await _client!.daemonStatus();
      final profiles = await _client!.profileList();
      final configurations = await _client!.configurationList();
      final sessions = await _client!.sessionList();
      if (_disposed) return;
      _snapshot = GuiSnapshot(
        daemon: daemon,
        profiles: profiles,
        configurations: configurations,
        sessions: sessions,
      );
      _state = GuiConnectionState.ready;
      _failure = null;
      _notify();
    } on Object {
      if (!_disposed) {
        _state = _snapshot == null
            ? GuiConnectionState.failed
            : GuiConnectionState.stale;
        _failure = GuiBootstrapFailure.failed;
        _notify();
      }
    } finally {
      _refreshing = false;
      if (!_disposed && _client != null) {
        _timer = Timer(pollDelay, _refresh);
      }
    }
  }

  void _setFailure(GuiBootstrapFailure failure) {
    _failure = failure;
    _state = failure.code == 'unsupported_platform'
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
    _timer?.cancel();
    unawaited(_client?.close());
    super.dispose();
  }
}
