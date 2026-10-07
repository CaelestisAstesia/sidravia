import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/web_socket_ipc_client.dart';

void main() {
  for (final listing in [false, true]) {
    test('raw WebSocket binding duplicate rejected listing=$listing', () async {
      final server = await _server((socket, _) {
        socket.listen((message) {
          final request = jsonDecode(message as String) as Map<String, dynamic>;
          final result = _fixtureResult('configuration.create');
          final wire =
              jsonEncode(
                listing
                    ? {
                        'storageProtection': 'protected',
                        'configurations': [result],
                      }
                    : result,
              ).replaceFirst(
                '"mode":"automatically_select_latest_available"',
                r'"mode":"automatically_select_latest_available","\u006dode":"automatically_select_latest_available"',
              );
          socket.add(
            '{"kind":"response","id":${jsonEncode(request['id'])},"ok":true,"result":$wire}',
          );
        });
      });
      addTearDown(() => server.close(force: true));
      final client = await WebSocketIpcClient.connect(_bootstrap(server.port));
      await expectLater(
        listing
            ? client.configurationList()
            : client.configurationSetAutoLogin(
                configurationId: 'fixture-configuration',
                autoLogin: true,
              ),
        throwsA(isA<IpcProtocolException>()),
      );
      await client.close();
    });
  }

  test('duplicate raw response members invalidate the socket before the next request', () async {
    Future<void> status(WebSocketIpcClient client) async {
      await client.daemonStatus();
    }

    Future<void> configurations(WebSocketIpcClient client) async {
      await client.configurationList();
    }

    Future<void> sessions(WebSocketIpcClient client) async {
      await client.sessionList();
    }

    Future<void> createConfiguration(WebSocketIpcClient client) async {
      await client.configurationCreate(
        networkBindingPolicy: const NetworkBindingPolicy.automatic(),
        institutionProfileId: 'jlu',
        username: 'u',
        password: 'p',
        autoLogin: false,
        autoReconnect: true,
        allowInsecureStorage: false,
      );
    }

    Future<void> verify(
      String name,
      Future<void> Function(WebSocketIpcClient) request,
      String Function(String id) response,
    ) async {
      final received = <String>[];
      final server = await _server((socket, _) {
        socket.listen((message) {
          final raw = jsonDecode(message as String) as Map<String, dynamic>;
          final id = raw['id'] as String;
          received.add(id);
          socket.add(response(id));
        });
      });
      addTearDown(() => server.close(force: true));
      final client = await WebSocketIpcClient.connect(_bootstrap(server.port));
      await expectLater(
        request(client),
        throwsA(isA<IpcProtocolException>()),
        reason: name,
      );
      await expectLater(
        request(client),
        throwsA(isA<IpcProtocolException>()),
        reason: '$name after invalidation',
      );
      expect(received, hasLength(1), reason: name);
      await client.close();
      await server.close(force: true);
    }

    final statusResult = jsonEncode(_fixtureResult('daemon.status'));
    final sessionResult = jsonEncode(_fixtureResult('session.restart'))
        .replaceFirst(
          '"sessionId":"session-retained"',
          '"sessionId":"session-retained","sessionId":"session-retained"',
        );
    final configurationResult =
        jsonEncode(_fixtureResult('configuration.create')).replaceFirst(
          '"username":"fixture-user"',
          '"username":"fixture-user","username":"fixture-user"',
        );
    final policyDuplicateResult =
        jsonEncode(_fixtureResult('configuration.create')).replaceFirst(
          '"mode":"automatically_select_latest_available"',
          '"mode":"automatically_select_latest_available","mode":"automatically_select_latest_available"',
        );
    String success(String id, String result) =>
        '{"kind":"response","id":${jsonEncode(id)},"ok":true,"result":$result}';
    final escapedIdKey = r'"\u0069d"';

    await verify(
      'literal duplicate id',
      status,
      (id) =>
          '{"kind":"response","id":${jsonEncode(id)},"id":${jsonEncode(id)},"ok":true,"result":$statusResult}',
    );
    await verify(
      'escaped id alias',
      status,
      (id) =>
          '{"kind":"response","id":${jsonEncode(id)},$escapedIdKey:${jsonEncode(id)},"ok":true,"result":$statusResult}',
    );
    await verify(
      'duplicate kind',
      status,
      (id) =>
          '{"kind":"response","kind":"response","id":${jsonEncode(id)},"ok":true,"result":$statusResult}',
    );
    await verify(
      'duplicate ok',
      status,
      (id) =>
          '{"kind":"response","id":${jsonEncode(id)},"ok":true,"ok":true,"result":$statusResult}',
    );
    await verify(
      'duplicate error code',
      createConfiguration,
      (id) =>
          '{"kind":"response","id":${jsonEncode(id)},"ok":false,"error":{"code":"configuration_conflict","code":"configuration_conflict","message":"safe"}}',
    );
    await verify(
      'duplicate error message alias',
      createConfiguration,
      (id) =>
          '{"kind":"response","id":${jsonEncode(id)},"ok":false,"error":{"code":"configuration_conflict","message":"safe",${r'"\u006dessage"'}:"safe"}}',
    );
    await verify(
      'nested Session member in list',
      sessions,
      (id) => success(id, '{"sessions":[$sessionResult]}'),
    );
    await verify(
      'nested Configuration member in list',
      configurations,
      (id) => success(
        id,
        '{"storageProtection":"protected","configurations":[$configurationResult]}',
      ),
    );
    await verify(
      'nested binding policy member',
      configurations,
      (id) => success(
        id,
        '{"storageProtection":"protected","configurations":[$policyDuplicateResult]}',
      ),
    );
  });

  test(
    'explicit consent reaches password settings and removal wire primitives',
    () async {
      final received = <Map<String, dynamic>>[];
      final server = await _server((socket, _) {
        socket.listen((message) {
          final req = jsonDecode(message as String) as Map<String, dynamic>;
          received.add(req);
          socket.add(
            jsonEncode({
              'kind': 'response',
              'id': req['id'],
              'ok': true,
              'result': _fixtureResult(req['method'] as String),
            }),
          );
        });
      });
      addTearDown(() => server.close(force: true));
      final c = await WebSocketIpcClient.connect(_bootstrap(server.port));
      await c.configurationSetPassword(
        configurationId: 'fixture-configuration',
        password: 'fixture',
        allowInsecureStorage: true,
      );
      await c.configurationSetAutoLogin(
        configurationId: 'fixture-configuration',
        autoLogin: true,
        allowInsecureStorage: true,
      );
      await c.configurationSetAutoReconnect(
        configurationId: 'fixture-configuration',
        autoReconnect: true,
        allowInsecureStorage: true,
      );
      await c.configurationRemove(
        'fixture-configuration',
        allowInsecureStorage: true,
      );
      expect(received.map((r) => r['payload']), [
        {
          'configurationId': 'fixture-configuration',
          'password': 'fixture',
          'allowInsecureStorage': true,
        },
        {
          'configurationId': 'fixture-configuration',
          'autoLogin': true,
          'allowInsecureStorage': true,
        },
        {
          'configurationId': 'fixture-configuration',
          'autoReconnect': true,
          'allowInsecureStorage': true,
        },
        {
          'configurationId': 'fixture-configuration',
          'allowInsecureStorage': true,
        },
      ]);
      await c.close();
    },
  );

  test(
    'transport serializes supplied creation policy without replacing it',
    () async {
      Map<String, dynamic>? request;
      final server = await _server((socket, _) {
        socket.listen((message) {
          request = jsonDecode(message as String) as Map<String, dynamic>;
          socket.add(
            jsonEncode({
              'kind': 'response',
              'id': request!['id'],
              'ok': true,
              'result': _fixtureResult('configuration.create'),
            }),
          );
        });
      });
      addTearDown(() => server.close(force: true));
      final c = await WebSocketIpcClient.connect(_bootstrap(server.port));
      await c.configurationCreate(
        networkBindingPolicy: const NetworkBindingPolicy.automatic(),
        institutionProfileId: 'jlu',
        username: 'u',
        password: 'p',
        autoLogin: true,
        autoReconnect: false,
        allowInsecureStorage: true,
      );
      expect(request!['payload'], {
        'networkBindingPolicy': {
          'mode': 'automatically_select_latest_available',
        },
        'institutionProfileId': 'jlu',
        'username': 'u',
        'password': 'p',
        'autoLogin': true,
        'autoReconnect': false,
        'allowInsecureStorage': true,
      });
      await c.close();
    },
  );

  test('refused connection has a fixed transport code', () async {
    final socket = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    final port = socket.port;
    await socket.close();
    await expectLater(
      WebSocketIpcClient.connect(_bootstrap(port)),
      throwsA(
        isA<IpcTransportException>().having(
          (e) => e.code,
          'code',
          'ipc_connection_failed',
        ),
      ),
    );
  });
  test('real socket timeout invalidates with a distinct code', () async {
    final server = await _server((socket, _) {
      socket.listen((_) {});
    });
    addTearDown(() => server.close(force: true));
    final client = await WebSocketIpcClient.connect(
      _bootstrap(server.port),
      requestTimeout: const Duration(milliseconds: 30),
    );
    await expectLater(
      client.daemonStatus(),
      throwsA(
        isA<IpcTransportException>().having(
          (e) => e.code,
          'code',
          'ipc_timeout',
        ),
      ),
    );
    await expectLater(
      client.daemonStatus(),
      throwsA(isA<IpcProtocolException>()),
    );
    await client.close();
  });

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
      networkBindingPolicy: const NetworkBindingPolicy.automatic(),
      institutionProfileId: 'jlu',
      username: 'fixture-user',
      password: 'fixture-configuration-password',

      autoLogin: false,
      autoReconnect: true,
      allowInsecureStorage: false,
    );
    await client.configurationUpdate(
      configurationId: 'fixture-configuration',
      institutionProfileId: 'jlu',
      username: 'fixture-user',
      password: 'replacement-password',
      allowInsecureStorage: true,
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
      'networkBindingPolicy': {'mode': 'automatically_select_latest_available'},
      'institutionProfileId': 'jlu',
      'username': 'fixture-user',
      'password': 'fixture-configuration-password',
      'allowInsecureStorage': false,
      'autoLogin': false,
      'autoReconnect': true,
    });
    expect(received[1]['payload'], {
      'configurationId': 'fixture-configuration',
      'institutionProfileId': 'jlu',
      'username': 'fixture-user',
      'password': 'replacement-password',
      'allowInsecureStorage': true,
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
        networkBindingPolicy: const NetworkBindingPolicy.automatic(),
        institutionProfileId: 'jlu',
        username: 'u',
        password: '',

        autoLogin: false,
        autoReconnect: true,
        allowInsecureStorage: false,
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
