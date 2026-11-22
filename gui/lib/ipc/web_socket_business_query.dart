part of 'web_socket_ipc_client.dart';

class _BusinessQuery implements BusinessQuery {
  _BusinessQuery(GuiBootstrap bootstrap, String sessionId, Duration timeout) {
    _work = _run(bootstrap, sessionId, timeout);
    _result = Future.any([_work, _cancelled.future]);
  }
  late final Future<BusinessSnapshot> _result, _work;
  WebSocketIpcClient? _client;
  bool _canceled = false;
  Future<void>? _canceling;
  final _cancelled = Completer<BusinessSnapshot>();
  @override
  Future<BusinessSnapshot> get result => _result;
  final _handshake = HttpClient();
  Future<BusinessSnapshot> _run(
    GuiBootstrap bootstrap,
    String sessionId,
    Duration timeout,
  ) async {
    final connecting = WebSocket.connect(
      bootstrap.endpoint.toString(),
      headers: {
        'Authorization': 'Bearer ${bootstrap.token}',
        'Sidravia-Build-ID': bootstrap.buildId,
      },
      customClient: _handshake,
    );
    try {
      final socket = await connecting.timeout(timeout);
      final client = WebSocketIpcClient._(
        socket,
        bootstrap,
        requestTimeout: timeout,
      );
      _client = client;
      try {
        if (_canceled) throw const IpcTransportException('ipc_canceled');
        return await client._call(
          'session.business.get',
          {'sessionId': sessionId},
          decodeBusiness,
          decodeValue: decodeBusinessValue,
        );
      } finally {
        await client.close();
      }
    } on IpcRequestFailure {
      rethrow;
    } on IpcProtocolException {
      rethrow;
    } on IpcTransportException {
      rethrow;
    } on TimeoutException {
      throw const IpcTransportException('ipc_timeout');
    } on Object {
      throw const IpcTransportException('ipc_connection_failed');
    } finally {
      // Also terminates an HTTP upgrade that never returned a WebSocket.
      _handshake.close(force: true);
      if (_client == null) {
        unawaited(WebSocketIpcClient._closeIfConnected(connecting));
      }
    }
  }

  @override
  Future<void> cancel() {
    if (_canceling != null) return _canceling!;
    _canceled = true;
    _handshake.close(force: true);
    if (!_cancelled.isCompleted) {
      _cancelled.completeError(const IpcTransportException('ipc_canceled'));
    }
    return _canceling = () async {
      await _client?.close();
      try {
        await _work;
      } on Object {
        /* Request result owns its safe error. */
      }
    }();
  }
}
