import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/web_socket_ipc_client.dart';

void main() {
  test(
    'cancel owned business transport leaves primary connection usable',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final sockets = <WebSocket>[];
      final businessSeen = Completer<void>();
      final listener = server.listen((request) async {
        final s = await WebSocketTransformer.upgrade(request);
        sockets.add(s);
        s.listen((raw) {
          final r = jsonDecode(raw as String);
          if (r['method'] == 'session.business.get') {
            businessSeen.complete();
          } else {
            s.add(
              jsonEncode({
                'kind': 'response',
                'id': r['id'],
                'ok': false,
                'error': {'code': 'unknown_method', 'message': 'Unsupported'},
              }),
            );
          }
        });
      });
      final client = await WebSocketIpcClient.connect(
        GuiBootstrap(
          endpoint: Uri.parse('ws://127.0.0.1:${server.port}'),
          token: 'fictional-only',
          productVersion: 'test',
          buildId: 'test',
          daemonPid: 1,
          mode: 'desktop',
        ),
      );
      try {
        final query = client.queryBusiness('s');
        final canceled = expectLater(
          query.result,
          throwsA(isA<IpcTransportException>()),
        );
        await businessSeen.future;
        await query.cancel();
        await canceled;
        await expectLater(
          client.profileList(),
          throwsA(isA<IpcRequestFailure>()),
        );
      } finally {
        await client.close();
        for (final s in sockets) {
          await s.close();
        }
        await listener.cancel();
        await server.close(force: true);
      }
    },
  );
  for (final timeout in [false, true]) {
    test(
      'business ${timeout ? 'timeout' : 'cancel'} closes unfinished HTTP upgrade',
      () async {
        final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
        final requestSeen = Completer<void>(), peerClosed = Completer<void>();
        final sockets = <Socket>[];
        WebSocket? mainSocket;
        var connections = 0;
        final listening = server.listen((request) async {
          if (++connections == 1) {
            final socket = await WebSocketTransformer.upgrade(request);
            mainSocket = socket;
            socket.listen((raw) {
              final r = jsonDecode(raw as String);
              socket.add(
                jsonEncode({
                  'kind': 'response',
                  'id': r['id'],
                  'ok': false,
                  'error': {'code': 'unknown_method', 'message': 'Unsupported'},
                }),
              );
            });
          } else {
            final socket = await request.response.detachSocket(
              writeHeaders: false,
            );
            sockets.add(socket);
            socket.listen(
              (_) {},
              onDone: () {
                if (!peerClosed.isCompleted) {
                  peerClosed.complete();
                }
              },
            );
            requestSeen.complete();
          }
        });
        final client = await WebSocketIpcClient.connect(
          GuiBootstrap(
            endpoint: Uri.parse('ws://127.0.0.1:${server.port}'),
            token: 'fictional',
            productVersion: 'test',
            buildId: 'test',
            daemonPid: 1,
            mode: 'desktop',
          ),
          requestTimeout: timeout
              ? const Duration(milliseconds: 100)
              : const Duration(seconds: 5),
        );
        try {
          final query = client.queryBusiness('s');
          final result = expectLater(
            query.result,
            throwsA(isA<IpcTransportException>()),
          );
          await requestSeen.future;
          if (!timeout) {
            await query.cancel().timeout(const Duration(seconds: 1));
          }
          await result;
          await peerClosed.future.timeout(const Duration(seconds: 1));
          await query.cancel();
          await expectLater(
            client.profileList(),
            throwsA(isA<IpcRequestFailure>()),
          );
        } finally {
          await client.close();
          await mainSocket?.close();
          for (final s in sockets) {
            s.destroy();
          }
          await listening.cancel();
          await server.close(force: true);
        }
      },
    );
  }
}
