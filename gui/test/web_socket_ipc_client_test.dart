import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/web_socket_ipc_client.dart';

void main() {
  test(
    'uses literal headers and sends sequential unique one-flight IDs',
    () async {
      final received = <Map<String, dynamic>>[];
      final server = await _server((socket, request) {
        expect(
          request.headers.value('Authorization'),
          'Bearer $_bootstrapToken',
        );
        expect(request.headers.value('Sidravia-Build-ID'), 'fixture');
        socket.listen((message) {
          final envelope =
              jsonDecode(message as String) as Map<String, dynamic>;
          received.add(envelope);
          socket.add(
            jsonEncode({
              'kind': 'response',
              'id': envelope['id'],
              'ok': true,
              'result': envelope['method'] == 'daemon.status'
                  ? _daemonResult
                  : {'profiles': []},
            }),
          );
        });
      });
      addTearDown(() => server.close(force: true));

      final client = await WebSocketIpcClient.connect(
        _bootstrap(server.port),
        requestTimeout: const Duration(milliseconds: 20),
      );
      expect((await client.daemonStatus()).desktopOwnerPid, 2);
      expect(await client.profileList(), isEmpty);
      await client.close();

      expect(received.map((value) => value['id']), ['gui-1', 'gui-2']);
      expect(received.map((value) => value['method']), [
        'daemon.status',
        'profile.list',
      ]);
      expect(
        received.every((value) => value['payload'].toString() == '{}'),
        isTrue,
      );
    },
  );

  test('rejects one in-flight call without sending a second request', () async {
    final received = <Map<String, dynamic>>[];
    final server = await _server((socket, _) {
      socket.listen((message) {
        received.add(jsonDecode(message as String) as Map<String, dynamic>);
      });
    });
    addTearDown(() => server.close(force: true));

    final client = await WebSocketIpcClient.connect(
      _bootstrap(server.port),
      requestTimeout: const Duration(milliseconds: 20),
    );
    final first = client.daemonStatus();
    await Future<void>.delayed(Duration.zero);
    await expectLater(
      client.profileList(),
      throwsA(isA<IpcProtocolException>()),
    );
    await expectLater(first, throwsA(isA<IpcProtocolException>()));
    expect(received, hasLength(1));
  });

  test(
    'strict error, wrong ID, and timeout failures invalidate without reuse',
    () async {
      final responses = <Map<String, dynamic>>[
        {
          'kind': 'response',
          'id': 'wrong',
          'ok': true,
          'result': _daemonResult,
        },
        {
          'kind': 'response',
          'id': 'later',
          'ok': false,
          'error': {'code': 'invalid_argument', 'message': 'safe'},
        },
      ];
      final server = await _server((socket, _) {
        socket.listen((message) {
          final request = jsonDecode(message as String) as Map<String, dynamic>;
          final response = responses.removeAt(0);
          if (response['id'] == 'later') response['id'] = request['id'];
          socket.add(jsonEncode(response));
        });
      });
      addTearDown(() => server.close(force: true));

      final wrongId = await WebSocketIpcClient.connect(
        _bootstrap(server.port),
        requestTimeout: const Duration(milliseconds: 20),
      );
      await expectLater(
        wrongId.daemonStatus(),
        throwsA(isA<IpcProtocolException>()),
      );
      await expectLater(
        wrongId.daemonStatus(),
        throwsA(isA<IpcProtocolException>()),
      );

      final error = await WebSocketIpcClient.connect(
        _bootstrap(server.port),
        requestTimeout: const Duration(milliseconds: 20),
      );
      await expectLater(
        error.daemonStatus(),
        throwsA(isA<IpcProtocolException>()),
      );
      await expectLater(
        error.daemonStatus(),
        throwsA(isA<IpcProtocolException>()),
      );

      final silent = await _server((socket, _) => socket.listen((_) {}));
      addTearDown(() => silent.close(force: true));
      final timedOut = await WebSocketIpcClient.connect(
        _bootstrap(silent.port),
        requestTimeout: const Duration(milliseconds: 20),
      );
      await expectLater(
        timedOut.daemonStatus(),
        throwsA(isA<IpcProtocolException>()),
      );
      await expectLater(
        timedOut.daemonStatus(),
        throwsA(isA<IpcProtocolException>()),
      );
    },
  );
}

Future<HttpServer> _server(
  void Function(WebSocket socket, HttpRequest request) handler,
) async {
  final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
  server.listen((request) async {
    final socket = await WebSocketTransformer.upgrade(request);
    handler(socket, request);
  });
  return server;
}

const _bootstrapToken =
    '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';

const _daemonResult = {
  'productVersion': 'fixture',
  'buildId': 'fixture',
  'pid': 1,
  'status': 'running',
  'mode': 'desktop',
  'desktopOwnerPid': 2,
};

GuiBootstrap _bootstrap(int port) => GuiBootstrap(
  endpoint: Uri.parse('ws://127.0.0.1:$port/ipc'),
  token: _bootstrapToken,
  productVersion: 'fixture',
  buildId: 'fixture',
  daemonPid: 1,
  mode: 'desktop',
);
