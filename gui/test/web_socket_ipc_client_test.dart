import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/web_socket_ipc_client.dart';

void main() {
  test('auto reconnect true false use exact update payload and authoritative return', () async {
    final received = <Map<String, dynamic>>[];
    final server = await _server((socket, _) {
      socket.listen((message) {
        final request = jsonDecode(message as String) as Map<String, dynamic>;
        received.add(request);
        final payload = request['payload'] as Map<String, dynamic>;
        final result = {
          ..._fixtureResult('configuration.update'),
          'autoReconnect': payload['autoReconnect'],
        };
        socket.add(
          jsonEncode({
            'kind': 'response',
            'id': request['id'],
            'ok': true,
            'result': result,
          }),
        );
      });
    });
    addTearDown(() => server.close(force: true));
    final client = await WebSocketIpcClient.connect(_bootstrap(server.port));
    for (final value in [true, false]) {
      final result = await client.configurationSetAutoReconnect(
        configurationId: 'fixture-configuration',
        autoReconnect: value,
      );
      expect(result.autoReconnect, value);
      expect(received.last['method'], 'configuration.update');
      expect(received.last['payload'], {
        'configurationId': 'fixture-configuration',
        'autoReconnect': value,
      });
      expect(result.autoLogin, true);
      expect(result.username, 'fixture-user');
    }
    await client.close();
    expect(received, hasLength(2));
  });

  test('sends all exact operational fixture shapes', () async {
    final received = <Map<String, dynamic>>[];
    final server = await _server((socket, request) {
      expect(request.headers.value('Authorization'), 'Bearer $_token');
      expect(request.headers.value('Sidravia-Build-ID'), 'fixture');
      socket.listen((message) {
        final request = jsonDecode(message as String) as Map<String, dynamic>;
        received.add(request);
        socket.add(
          jsonEncode({
            'kind': 'response',
            'id': request['id'],
            'ok': true,
            'result': _fixtureResult(request['method'] as String),
          }),
        );
      });
    });
    addTearDown(() => server.close(force: true));
    final client = await WebSocketIpcClient.connect(_bootstrap(server.port));

    await client.configurationCreate(
      institutionProfileId: 'jlu',
      username: 'fixture-user',
      password: 'fixture-configuration-password',
    );
    await client.configurationUpdate(
      configurationId: 'fixture-configuration',
      institutionProfileId: 'jlu',
      username: 'fixture-user',
    );
    await client.configurationSetPassword(
      configurationId: 'fixture-configuration',
      password: '',
    );
    await client.daemonStop();
    await client.sessionStartConfiguration(
      'cfg-0123456789abcdef0123456789abcdef',
    );
    await client.sessionStop('session-retained');
    await client.sessionEnsureRunning('session-retained');
    await client.sessionRestart('session-retained');
    await client.configurationSetAutoLogin(
      configurationId: 'fixture-configuration',
      autoLogin: true,
    );
    await client.sessionRemove('session-retained');
    await client.configurationRemove('fixture-configuration');
    await client.close();

    expect(received.map((value) => value['method']), [
      'configuration.create',
      'configuration.update',
      'configuration.setPassword',
      'daemon.stop',
      'session.startConfiguration',
      'session.stop',
      'session.ensureRunning',
      'session.restart',
      'configuration.update',
      'session.remove',
      'configuration.remove',
    ]);
    expect(received[0]['payload'], {
      'institutionProfileId': 'jlu',
      'username': 'fixture-user',
      'password': 'fixture-configuration-password',
      'allowInsecureStorage': false,
      'autoLogin': false,
      'autoReconnect': true,
    });
    expect(received[2]['payload'], {
      'configurationId': 'fixture-configuration',
      'password': '',
      'allowInsecureStorage': false,
    });
    expect(
      received.firstWhere(
        (value) => value['method'] == 'daemon.stop',
      )['payload'],
      <String, dynamic>{},
    );
    expect(
      received.where((value) => value['method'] == 'configuration.update'),
      hasLength(2),
    );
    expect(
      received.lastWhere(
        (value) => value['method'] == 'configuration.update',
      )['payload'],
      {'configurationId': 'fixture-configuration', 'autoLogin': true},
    );
    expect(
      received.lastWhere(
        (value) => value['method'] == 'session.remove',
      )['payload'],
      {'sessionId': 'session-retained'},
    );
    expect(
      received.lastWhere(
        (value) => value['method'] == 'configuration.remove',
      )['payload'],
      {'configurationId': 'fixture-configuration'},
    );
    expect(
      received.every((value) => (value['id'] as String).startsWith('gui-')),
      isTrue,
    );
  });

  test('valid business errors retain the connection but protocol errors invalidate it', () async {
    final business = await _server((socket, _) {
      socket.listen((message) {
        final request = jsonDecode(message as String) as Map<String, dynamic>;
        if (request['method'] == 'configuration.create') {
          socket.add(
            jsonEncode({
              'kind': 'response',
              'id': request['id'],
              'ok': false,
              'error': {'code': 'configuration_conflict', 'message': 'safe'},
            }),
          );
        } else {
          socket.add(
            jsonEncode({
              'kind': 'response',
              'id': request['id'],
              'ok': true,
              'result': _fixtureResult('daemon.status'),
            }),
          );
        }
      });
    });
    addTearDown(() => business.close(force: true));
    final client = await WebSocketIpcClient.connect(_bootstrap(business.port));
    await expectLater(
      client.configurationCreate(
        institutionProfileId: 'jlu',
        username: 'u',
        password: '',
      ),
      throwsA(isA<IpcRequestFailure>()),
    );
    expect((await client.daemonStatus()).mode, 'headless');
    await client.close();

    final broken = await _server((socket, _) {
      socket.listen((message) {
        final request = jsonDecode(message as String) as Map<String, dynamic>;
        socket.add(
          jsonEncode({
            'kind': 'response',
            'id': 'wrong-${request['id']}',
            'ok': true,
            'result': _fixtureResult('daemon.status'),
          }),
        );
      });
    });
    addTearDown(() => broken.close(force: true));
    final invalid = await WebSocketIpcClient.connect(_bootstrap(broken.port));
    await expectLater(
      invalid.daemonStatus(),
      throwsA(isA<IpcProtocolException>()),
    );
    await expectLater(
      invalid.daemonStatus(),
      throwsA(isA<IpcProtocolException>()),
    );
  });

  test(
    'invalid typed result invalidates without sending a second request',
    () async {
      final received = <Map<String, dynamic>>[];
      final server = await _server((socket, _) {
        socket.listen((message) {
          final request = jsonDecode(message as String) as Map<String, dynamic>;
          received.add(request);
          socket.add(
            jsonEncode({
              'kind': 'response',
              'id': request['id'],
              'ok': true,
              'result': {'unexpected': true},
            }),
          );
        });
      });
      addTearDown(() => server.close(force: true));
      final client = await WebSocketIpcClient.connect(_bootstrap(server.port));

      await expectLater(
        client.daemonStatus(),
        throwsA(isA<IpcProtocolException>()),
      );
      await expectLater(
        client.daemonStatus(),
        throwsA(isA<IpcProtocolException>()),
      );

      expect(received, hasLength(1));
    },
  );
}

Future<HttpServer> _server(
  void Function(WebSocket, HttpRequest) handler,
) async {
  final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
  server.listen(
    (request) async =>
        handler(await WebSocketTransformer.upgrade(request), request),
  );
  return server;
}

Map<String, dynamic> _fixtureResult(String method) {
  final fixture = jsonDecode(
    File('../internal/ipc/contract/testdata/v1/conformance.json')
        .readAsStringSync(),
  ) as Map<String, dynamic>;
  final item = (fixture['cases'] as List<dynamic>)
      .cast<Map<String, dynamic>>()
      .firstWhere((value) => value['method'] == method);
  return (jsonDecode(item['successResponse'] as String)
          as Map<String, dynamic>)['result']
      as Map<String, dynamic>;
}

GuiBootstrap _bootstrap(int port) => GuiBootstrap(
  endpoint: Uri.parse('ws://127.0.0.1:$port/ipc'),
  token: _token,
  productVersion: 'fixture',
  buildId: 'fixture',
  daemonPid: 1,
  mode: 'desktop',
);

const _token =
    '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
