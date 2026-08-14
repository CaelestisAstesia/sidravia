import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

class WebSocketIpcClient implements SidraviaIpcClient {
  WebSocketIpcClient._(this._socket) : _messages = StreamIterator(_socket);

  static const _maximumMessageBytes = 64 * 1024;
  static const _requestTimeout = Duration(seconds: 5);

  final WebSocket _socket;
  final StreamIterator<dynamic> _messages;
  var _nextId = 0;
  var _inFlight = false;
  var _closed = false;

  static Future<WebSocketIpcClient> connect(GuiBootstrap bootstrap) async {
    final socket = await WebSocket.connect(
      bootstrap.endpoint.toString(),
      headers: {
        'Authorization': 'Bearer ${bootstrap.token}',
        'Sidravia-Build-ID': bootstrap.buildId,
      },
    ).timeout(_requestTimeout);
    return WebSocketIpcClient._(socket);
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
      final hasMessage = await _messages.moveNext().timeout(_requestTimeout);
      if (!hasMessage) throw const IpcProtocolException();
      final message = _messages.current;
      if (message is! String ||
          utf8.encode(message).length > _maximumMessageBytes) {
        throw const IpcProtocolException();
      }
      final envelope = decodeObject(message, const {
        'kind',
        'id',
        'ok',
        'result',
      });
      if (envelope['kind'] != 'response' ||
          envelope['id'] != id ||
          envelope['ok'] != true ||
          envelope['result'] is! Map<String, dynamic>) {
        throw const IpcProtocolException();
      }
      return jsonEncode(envelope['result']);
    } finally {
      _inFlight = false;
    }
  }

  @override
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    await _messages.cancel();
    await _socket.close(WebSocketStatus.normalClosure);
  }
}
