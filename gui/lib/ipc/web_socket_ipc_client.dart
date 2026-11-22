import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/state_models.dart';
import 'package:sidravia_gui/ipc/state_subscription.dart';

part 'web_socket_business_query.dart';
part 'web_socket_state_subscription.dart';

class WebSocketIpcClient
    implements
        SidraviaDesktopClient,
        SidraviaNetworkClient,
        SidraviaDiagnosticClient,
        SidraviaStateClient,
        SidraviaMaintenanceClient,
        SidraviaBusinessClient,
        SidraviaOverrideClient {
  WebSocketIpcClient._(
    this._socket,
    this._bootstrap, {
    Duration requestTimeout = _requestTimeout,
  }) : _timeout = requestTimeout {
    _reader = _socket.listen(
      _receive,
      onError: (Object _) =>
          _discontinue(const IpcTransportException('ipc_disconnected')),
      onDone: () =>
          _discontinue(const IpcTransportException('ipc_disconnected')),
    );
  }

  static const _maximumMessageBytes = 64 * 1024;
  static const _requestTimeout = Duration(seconds: 5);

  final WebSocket _socket;
  final GuiBootstrap _bootstrap;
  late final StreamSubscription<dynamic> _reader;
  _PendingRequest? _pending;
  _OwnedStateSubscription? _owner;
  Future<void>? _closing;
  Completer<void>? _operationDone;
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
        bootstrap,
        requestTimeout: requestTimeout,
      );
    } on Object catch (error) {
      unawaited(_closeIfConnected(connecting));
      throw IpcTransportException(
        error is TimeoutException
            ? 'ipc_timeout'
            : error is WebSocketException
            ? 'ipc_handshake_failed'
            : 'ipc_connection_failed',
      );
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
  BusinessQuery queryBusiness(String sessionId) =>
      _BusinessQuery(_bootstrap, sessionId, _timeout);

  @override
  Future<DiagnosticExport> diagnosticsExport() async => _call(
    'diagnostics.export',
    const {},
    decodeDiagnosticExport,
    decodeValue: decodeDiagnosticExportValue,
  );

  @override
  Future<NetworkInterfacesSnapshot> networkInterfaces() async => _call(
    'network.interfaces',
    const {},
    decodeNetworkInterfaces,
    decodeValue: decodeNetworkInterfacesValue,
  );

  @override
  Future<ConfigurationSummary> configurationSetNetworkBindingPolicy({
    required String configurationId,
    required NetworkBindingPolicy policy,
    bool allowInsecureStorage = false,
  }) async => _call('configuration.update', {
    'configurationId': configurationId,
    'networkBindingPolicy': policy.toJson(),
    if (allowInsecureStorage) 'allowInsecureStorage': true,
  }, decodeConfiguration);

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
  Future<MaintenanceStatus> updateStatus() =>
      _call('update.status', const {}, decodeMaintenanceStatus);
  @override
  Future<MaintenanceStatus> updateAcknowledge(
    String operationId,
    bool accepted,
    bool visible,
  ) => _call('update.acknowledge', {
    'operationId': operationId,
    'accepted': accepted,
    'visible': visible,
  }, decodeMaintenanceStatus);

  @override
  Future<DaemonStopResult> daemonStop() async =>
      _call('daemon.stop', const {}, decodeDaemonStop);

  @override
  Future<List<SessionSummary>> sessionList() async =>
      _call('session.list', const {}, decodeSessions);

  @override
  Future<ConfigurationSummary> configurationCreate({
    required NetworkBindingPolicy networkBindingPolicy,
    required String institutionProfileId,
    required String username,
    required String password,
    required bool autoLogin,
    required bool autoReconnect,
    required bool allowInsecureStorage,
  }) async => _call('configuration.create', {
    'networkBindingPolicy': networkBindingPolicy.toJson(),
    'institutionProfileId': institutionProfileId,
    'username': username,
    'password': password,
    'allowInsecureStorage': allowInsecureStorage,
    'autoLogin': autoLogin,
    'autoReconnect': autoReconnect,
  }, decodeConfiguration);

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    NetworkBindingPolicy? networkBindingPolicy,
    bool allowInsecureStorage = false,
  }) async => _call('configuration.update', {
    'configurationId': configurationId,
    'institutionProfileId': institutionProfileId,
    'username': username,
    'password': ?password,
    if (networkBindingPolicy != null)
      'networkBindingPolicy': networkBindingPolicy.toJson(),
    if (allowInsecureStorage) 'allowInsecureStorage': true,
  }, decodeConfiguration);

  @override
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
    bool allowInsecureStorage = false,
  }) async => _call('configuration.setPassword', {
    'configurationId': configurationId,
    'password': password,
    'allowInsecureStorage': allowInsecureStorage,
  }, decodeConfiguration);

  @override
  Future<ConfigurationSummary> configurationSetAutoLogin({
    required String configurationId,
    required bool autoLogin,
    bool allowInsecureStorage = false,
  }) async => _call('configuration.update', {
    'configurationId': configurationId,
    'autoLogin': autoLogin,
    if (allowInsecureStorage) 'allowInsecureStorage': true,
  }, decodeConfiguration);

  @override
  Future<ConfigurationSummary> configurationSetAutoReconnect({
    required String configurationId,
    required bool autoReconnect,
    bool allowInsecureStorage = false,
  }) async => _call('configuration.update', {
    'configurationId': configurationId,
    'autoReconnect': autoReconnect,
    if (allowInsecureStorage) 'allowInsecureStorage': true,
  }, decodeConfiguration);

  @override
  Future<ConfigurationRemoveResult> configurationRemove(
    String configurationId, {
    bool allowInsecureStorage = false,
  }) async => _call(
    'configuration.remove',
    {
      'configurationId': configurationId,
      if (allowInsecureStorage) 'allowInsecureStorage': true,
    },
    (result) => decodeConfigurationRemove(
      result,
      expectedConfigurationId: configurationId,
    ),
  );

  @override
  Future<ConfigurationOverride> configurationOverride(
    String id, {
    String? expectedDocument,
  }) => _call('configuration.getOverride', {
    'configurationId': id,
    'expectedDocument': ?expectedDocument,
  }, decodeConfigurationOverride);

  @override
  Future<OverrideValidation> validateOverride(
    String profile,
    String document,
  ) => _call('configuration.validateOverride', {
    'institutionProfileId': profile,
    'document': document,
  }, decodeOverrideValidation);

  @override
  Future<ConfigurationSummary> saveConfigurationEdit(
    Map<String, Object?> fields, {
    required bool creating,
    required bool allowInsecureStorage,
  }) => _call(creating ? 'configuration.create' : 'configuration.update', {
    ...fields,
    if (allowInsecureStorage || creating)
      'allowInsecureStorage': allowInsecureStorage,
  }, decodeConfiguration);

  @override
  Future<SessionSummary> startConfigurationVersion(String id, String version) =>
      _call('session.startConfiguration', {
        'configurationId': id,
        'expectedVersion': version,
      }, decodeSessionOperation);

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

  @override
  Future<StateSubscription> subscribeStateEvents() async {
    if (_owner != null) throw const IpcProtocolException();
    return _call(
      'state.subscribe',
      const {},
      (_) => throw const IpcProtocolException(),
      decodeValue: (raw) {
        final owner = _OwnedStateSubscription(
          this,
          decodeStateBootstrapValue(raw),
        );
        // Install synchronously in the sole reader before the next socket event.
        _owner = owner;
        return owner;
      },
    );
  }

  Future<T> _call<T>(
    String method,
    Map<String, Object?> payload,
    T Function(String result) decode, {
    T Function(Object? result)? decodeValue,
    bool unsubscribe = false,
  }) async {
    if (_closed || _inFlight || (_owner?.canceling == true && !unsubscribe)) {
      throw const IpcProtocolException();
    }
    final id = 'gui-${++_nextId}';
    final encoded = jsonEncode({
      'kind': 'request',
      'id': id,
      'method': method,
      'payload': payload,
    });
    if (utf8.encode(encoded).length > _maximumMessageBytes) {
      throw const IpcProtocolException();
    }
    _inFlight = true;
    final done = Completer<void>();
    _operationDone = done;
    final pending = _PendingRequest(id, method, (raw) {
      if (decodeValue != null) return decodeValue(raw);
      if (method == 'session.list') return decodeSessionsValue(raw);
      if (const {
        'session.startOneShot',
        'session.startConfiguration',
        'session.ensureRunning',
      }.contains(method)) {
        return decodeSessionOperationValue(raw);
      }
      if (const {
        'session.get',
        'session.stop',
        'session.restart',
      }.contains(method)) {
        return decodeSessionValue(raw);
      }
      return decode(jsonEncode(raw));
    });
    _pending = pending;
    try {
      _socket.add(encoded);
      return await pending.result.future.timeout(_timeout) as T;
    } on IpcRequestFailure {
      rethrow;
    } on Object catch (error) {
      final failure =
          error is IpcTransportException || error is IpcProtocolException
          ? error
          : error is TimeoutException
          ? const IpcTransportException('ipc_timeout')
          : error is SocketException || error is WebSocketException
          ? const IpcTransportException('ipc_disconnected')
          : const IpcProtocolException();
      _discontinue(failure);
      await _closing;
      throw failure;
    } finally {
      _inFlight = false;
      done.complete();
      _operationDone = null;
    }
  }

  void _receive(dynamic message) {
    if (_closed) return;
    try {
      if (message is! String ||
          utf8.encode(message).length > _maximumMessageBytes) {
        throw const IpcProtocolException();
      }
      final raw = decodeBindingCheckedJson(
        message,
        preserveUnsignedRevision: true,
        preserveRunGeneration: true,
        strictUnsignedNumbers: _pending?.method == 'diagnostics.export',
      );
      if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
      if (raw['kind'] == 'event') {
        final event = decodeStateEventValue(raw);
        final owner = _owner;
        if (owner == null) throw const IpcProtocolException();
        if (!owner.canceling) owner.accept(event);
        return;
      }
      final pending = _pending;
      if (pending == null ||
          raw['kind'] != 'response' ||
          raw['id'] != pending.id ||
          raw['ok'] is! bool) {
        throw const IpcProtocolException();
      }
      final keys = {'kind', 'id', 'ok', raw['ok'] == true ? 'result' : 'error'};
      if (raw.length != keys.length || !raw.keys.toSet().containsAll(keys)) {
        throw const IpcProtocolException();
      }
      if (raw['ok'] == true) {
        if (raw['result'] is! Map<String, dynamic>) {
          throw const IpcProtocolException();
        }
        final result = pending.decode(raw['result']);
        _pending = null;
        pending.result.complete(result);
      } else {
        final error = raw['error'];
        if (error is! Map<String, dynamic> ||
            error.length != 2 ||
            !error.keys.toSet().containsAll(const {'code', 'message'}) ||
            error['code'] is! String ||
            (error['code'] as String).isEmpty ||
            error['message'] is! String ||
            (error['message'] as String).isEmpty) {
          throw const IpcProtocolException();
        }
        _pending = null;
        pending.result.completeError(
          IpcRequestFailure(error['code'] as String),
        );
      }
    } on Object catch (error) {
      _discontinue(
        error is IpcTransportException ? error : const IpcProtocolException(),
      );
    }
  }

  Future<void> _unsubscribe(_OwnedStateSubscription owner) async {
    if (_closed || _owner != owner) return;
    owner.canceling = true;
    owner.clearPending();
    final operation = _operationDone;
    if (operation != null) await operation.future;
    if (_closed) return;
    try {
      await _call<Object?>(
        'state.unsubscribe',
        const {},
        (_) => null,
        unsubscribe: true,
        decodeValue: (raw) {
          decodeStateUnsubscribeValue(raw);
          // Matching ACK is the barrier; the next old event is unsolicited.
          _owner = null;
          owner.end();
          return null;
        },
      );
    } on Object catch (error) {
      _discontinue(
        error is IpcTransportException ? error : const IpcProtocolException(),
      );
      await _closing;
      rethrow;
    }
  }

  void _discontinue(Object failure) {
    if (_closed) return;
    _closed = true;
    final pending = _pending;
    _pending = null;
    if (pending != null) pending.result.completeError(failure);
    final owner = _owner;
    _owner = null;
    owner?.end(failure);
    _closing = _shutdown();
  }

  Future<void> _shutdown() async {
    try {
      await _reader.cancel();
    } on Object {
      /* Closure remains final. */
    }
    try {
      await _socket.close(WebSocketStatus.normalClosure);
    } on Object {
      /* Fixed public failure. */
    }
  }

  @override
  Future<void> close() async {
    _discontinue(const IpcTransportException('ipc_disconnected'));
    await _closing;
    // Reader cleanup never waits on the operation that invoked it.
    final operation = _operationDone;
    if (operation != null) await operation.future;
  }
}

class _PendingRequest {
  _PendingRequest(this.id, this.method, this.decode);
  final String id, method;
  final Object? Function(Object?) decode;
  final result = Completer<Object?>();
}
