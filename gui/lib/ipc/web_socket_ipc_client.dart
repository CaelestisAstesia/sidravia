import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

class WebSocketIpcClient implements SidraviaDesktopClient {
  WebSocketIpcClient._(
    this._socket, {
    Duration requestTimeout = _requestTimeout,
  }) : _messages = StreamIterator(_socket),
       _timeout = requestTimeout;

  static const _maximumMessageBytes = 64 * 1024;
  static const _requestTimeout = Duration(seconds: 5);

  final WebSocket _socket;
  final StreamIterator<dynamic> _messages;
  final Duration _timeout;
  var _nextId = 0;
  var _inFlight = false;
  var _closed = false;

  static Future<WebSocketIpcClient> connect(
    GuiBootstrap bootstrap, {
    Duration requestTimeout = _requestTimeout,
  }) async {
    final connecting = WebSocket.connect(
      bootstrap.endpoint.toString(),
      headers: {
        'Authorization': 'Bearer ${bootstrap.token}',
        'Sidravia-Build-ID': bootstrap.buildId,
      },
    );
    try {
      return WebSocketIpcClient._(
        await connecting.timeout(requestTimeout),
        requestTimeout: requestTimeout,
      );
    } on Object {
      unawaited(_closeIfConnected(connecting));
      rethrow;
    }
  }

  static Future<void> _closeIfConnected(Future<WebSocket> connecting) async {
    try {
      await (await connecting).close(WebSocketStatus.normalClosure);
    } on Object {
      // The connection failure remains the stable public failure.
    }
  }

  @override
  Future<DaemonStatus> daemonStatus() async =>
      _call('daemon.status', const {}, decodeDaemonStatus);

  @override
  Future<List<InstitutionProfile>> profileList() async =>
      _call('profile.list', const {}, decodeProfiles);

  @override
  Future<List<ConfigurationSummary>> configurationList() async =>
      _call('configuration.list', const {}, decodeConfigurations);

  @override
  Future<DaemonStopResult> daemonStop() async =>
      _call('daemon.stop', const {}, decodeDaemonStop);

  @override
  Future<List<SessionSummary>> sessionList() async =>
      _call('session.list', const {}, decodeSessions);

  @override
  Future<ConfigurationSummary> configurationCreate({
    required String institutionProfileId,
    required String username,
    required String password,
  }) async => _call('configuration.create', {
    'institutionProfileId': institutionProfileId,
    'username': username,
    'password': password,
    'allowInsecureStorage': false,
    'autoLogin': false,
    'autoReconnect': true,
  }, decodeConfiguration);

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
  }) async => _call('configuration.update', {
    'configurationId': configurationId,
    'institutionProfileId': institutionProfileId,
    'username': username,
  }, decodeConfiguration);

  @override
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
  }) async => _call('configuration.setPassword', {
    'configurationId': configurationId,
    'password': password,
    'allowInsecureStorage': false,
  }, decodeConfiguration);

  @override
  Future<ConfigurationSummary> configurationSetAutoLogin({
    required String configurationId,
    required bool autoLogin,
  }) async => _call('configuration.update', {
    'configurationId': configurationId,
    'autoLogin': autoLogin,
  }, decodeConfiguration);

  @override
  Future<ConfigurationSummary> configurationSetAutoReconnect({
    required String configurationId,
    required bool autoReconnect,
  }) async => _call('configuration.update', {
    'configurationId': configurationId,
    'autoReconnect': autoReconnect,
  }, decodeConfiguration);

  @override
  Future<ConfigurationRemoveResult> configurationRemove(
    String configurationId,
  ) async => _call(
    'configuration.remove',
    {'configurationId': configurationId},
    (result) => decodeConfigurationRemove(
      result,
      expectedConfigurationId: configurationId,
    ),
  );

  @override
  Future<SessionSummary> sessionStartConfiguration(
    String configurationId,
  ) async => _call('session.startConfiguration', {
    'configurationId': configurationId,
  }, decodeSessionOperation);

  @override
  Future<SessionSummary> sessionStop(String sessionId) async =>
      _call('session.stop', {'sessionId': sessionId}, decodeSession);

  @override
  Future<SessionRemoveResult> sessionRemove(String sessionId) async => _call(
    'session.remove',
    {'sessionId': sessionId},
    (result) => decodeSessionRemove(result, expectedSessionId: sessionId),
  );

  @override
  Future<SessionSummary> sessionEnsureRunning(String sessionId) async => _call(
    'session.ensureRunning',
    {'sessionId': sessionId},
    decodeSessionOperation,
  );

  @override
  Future<SessionSummary> sessionRestart(String sessionId) async =>
      _call('session.restart', {'sessionId': sessionId}, decodeSession);

  Future<T> _call<T>(
    String method,
    Map<String, Object> payload,
    T Function(String result) decode,
  ) async {
    if (_closed || _inFlight) throw const IpcProtocolException();
    _inFlight = true;
    try {
      final id = 'gui-${++_nextId}';
      _socket.add(
        jsonEncode({
          'kind': 'request',
          'id': id,
          'method': method,
          'payload': payload,
        }),
      );
      final hasMessage = await _messages.moveNext().timeout(_timeout);
      if (!hasMessage) throw const IpcProtocolException();
      final message = _messages.current;
      if (message is! String ||
          utf8.encode(message).length > _maximumMessageBytes) {
        throw const IpcProtocolException();
      }
      final raw = jsonDecode(message);
      if (raw is! Map<String, dynamic> ||
          raw['kind'] != 'response' ||
          raw['id'] != id ||
          raw['ok'] is! bool) {
        throw const IpcProtocolException();
      }
      if (raw['ok'] == true) {
        final envelope = decodeObject(message, const {
          'kind',
          'id',
          'ok',
          'result',
        });
        if (envelope['result'] is! Map<String, dynamic>) {
          throw const IpcProtocolException();
        }
        return decode(jsonEncode(envelope['result']));
      }
      final envelope = decodeObject(message, const {
        'kind',
        'id',
        'ok',
        'error',
      });
      if (envelope['error'] is! Map<String, dynamic>) {
        throw const IpcProtocolException();
      }
      final error = envelope['error'] as Map<String, dynamic>;
      if (error.length != 2 ||
          error['code'] is! String ||
          error['message'] is! String ||
          (error['code'] as String).isEmpty ||
          (error['message'] as String).isEmpty) {
        throw const IpcProtocolException();
      }
      throw IpcRequestFailure(error['code'] as String);
    } on IpcRequestFailure {
      rethrow;
    } on Object {
      try {
        await _invalidate();
      } on Object {
        // The public transport boundary remains a stable protocol failure.
      }
      throw const IpcProtocolException();
    } finally {
      _inFlight = false;
    }
  }

  Future<void> _invalidate() async {
    if (_closed) return;
    _closed = true;
    try {
      await _messages.cancel();
    } on Object {
      // Socket close still runs when cancelling the iterator fails.
    }
    try {
      await _socket.close(WebSocketStatus.normalClosure);
    } on Object {
      // The closed state is final even when transport cleanup fails.
    }
  }

  @override
  Future<void> close() async {
    if (_closed) return;
    await _invalidate();
  }
}
