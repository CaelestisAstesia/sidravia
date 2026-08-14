import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

class WebSocketIpcClient implements SidraviaIpcClient {
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
      decodeDaemonStatus(await _call('daemon.status'));

  @override
  Future<List<InstitutionProfile>> profileList() async =>
      decodeProfiles(await _call('profile.list'));

  @override
  Future<List<ConfigurationSummary>> configurationList() async =>
      decodeConfigurations(await _call('configuration.list'));

  @override
  Future<List<SessionSummary>> sessionList() async =>
      decodeSessions(await _call('session.list'));

  Future<String> _call(String method) async {
    if (_closed || _inFlight) throw const IpcProtocolException();
    _inFlight = true;
    try {
      final id = 'gui-${++_nextId}';
      _socket.add(
        jsonEncode({
          'kind': 'request',
          'id': id,
          'method': method,
          'payload': <String, Object>{},
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
        return jsonEncode(envelope['result']);
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
      throw const IpcProtocolException();
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
    await _messages.cancel();
    await _socket.close(WebSocketStatus.normalClosure);
  }

  @override
  Future<void> close() async {
    if (_closed) return;
    await _invalidate();
  }
}
