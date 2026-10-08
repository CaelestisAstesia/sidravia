import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/state_models.dart';
import 'package:sidravia_gui/ipc/state_subscription.dart';
import 'package:sidravia_gui/ipc/web_socket_ipc_client.dart';

void main() {
  test('ACK installs full typed bootstrap before immediate event and RPC interleaving', () async {
    final peer = await _Peer.open();
    addTearDown(peer.close);
    final subscribing = peer.client.subscribeStateEvents();
    final request = await peer.next();
    expect(request['method'], 'state.subscribe');
    expect(request['payload'], isEmpty);
    peer.ok(request, _bootstrap([_session('s', '9007199254740993')], ['s']));
    peer.socket.add(_changed('s', '18446744073709551615'));
    final owned = await subscribing;
    expect(
      owned.initial.sessions.single.revision,
      BigInt.parse('9007199254740993'),
    );
    expect(owned.initial.sessions.single.cleanupRequired, isTrue);
    final events = <StateEvent>[];
    final delivered = Completer<void>();
    final listener = owned.events.listen((event) {
      events.add(event);
      if (events.length == 2) delivered.complete();
    });
    final rpc = peer.client.daemonStatus();
    final ordinary = await peer.next();
    peer.socket.add(_network(1));
    peer.ok(ordinary, _status);
    expect((await rpc).mode, 'headless');
    await delivered.future;
    final changed = events.first as SessionChanged;
    expect(changed.session.revision, BigInt.parse('18446744073709551615'));
    expect(changed.session.institutionProfileId, 'i');
    expect(changed.session.institutionDisplayName, 'Institution');
    expect(changed.session.authenticationProtocolId, 'p');
    expect(
      changed.session.protocolSocket.runGeneration,
      BigInt.parse('9007199254740993'),
    );
    expect(events.last, isA<NetworkChanged>());
    await _retire(peer, owned);
    await listener.cancel();
  });

  test('stale/equal cursors retain cleanup changes; equal terminal wins and stays terminal', () async {
    final peer = await _Peer.open();
    addTearDown(peer.close);
    final owned = await _subscribe(peer, _bootstrap([_session('s', '5')]));
    // No listener: the transport must retain a bounded latest event itself.
    peer.socket.add(_changed('s', '4'));
    peer.socket.add(_changed('s', '5'));
    peer.socket.add(_changed('s', '5', cleanup: true));
    await peer.barrier();
    final events = <StateEvent>[];
    final cleanup = Completer<void>();
    final removed = Completer<void>();
    final listener = owned.events.listen((event) {
      events.add(event);
      if (event is SessionChanged) cleanup.complete();
      if (event is SessionRemoved) removed.complete();
    });
    await cleanup.future;
    expect((events.single as SessionChanged).cleanupRequired, isTrue);
    peer.socket.add(_removed('s', 5));
    peer.socket.add(_changed('s', '6'));
    peer.socket.add(_removed('s', 7));
    peer.socket.add(_network(1));
    peer.socket.add(_network(1));
    await peer.barrier();
    await removed.future;
    expect(events.whereType<SessionChanged>(), hasLength(1));
    expect(events.whereType<SessionRemoved>().single.revision, BigInt.from(5));
    expect(events.whereType<NetworkChanged>(), hasLength(1));
    await _retire(peer, owned);
    await listener.cancel();
  });

  test('paused listener coalesces latest with FIFO fairness and separate network key', () async {
    final peer = await _Peer.open();
    addTearDown(peer.close);
    final owned = await _subscribe(peer);
    final events = <StateEvent>[];
    final delivered = Completer<void>();
    final listener = owned.events.listen((event) {
      events.add(event);
      if (events.length == 3) delivered.complete();
    });
    listener.pause();
    peer.socket.add(_changed('network', '1'));
    peer.socket.add(_changed('b', '1'));
    for (var i = 2; i <= 350; i++) {
      peer.socket.add(_changed('network', '$i'));
      peer.socket.add(_network(i));
    }
    await peer.barrier();
    expect(events, isEmpty);
    listener.resume();
    await delivered.future;
    expect((events[0] as SessionChanged).session.id, 'network');
    expect((events[0] as SessionChanged).session.revision, BigInt.from(350));
    expect((events[1] as SessionChanged).session.id, 'b');
    expect((events[2] as NetworkChanged).network.revision, BigInt.from(350));
    await peer.barrier();
    expect(events, hasLength(3));
    await _retire(peer, owned);
    await listener.cancel();
  });

  test('unsubscribe discards valid preACK events, resets epoch and rejects postACK events', () async {
    final peer = await _Peer.open();
    addTearDown(peer.close);
    final owned = await _subscribe(peer);
    final events = <StateEvent>[];
    final listener = owned.events.listen(events.add);
    final closing = owned.close();
    final request = await peer.next();
    expect(request['method'], 'state.unsubscribe');
    peer.socket.add(_changed('old', '1'));
    peer.ok(request, '{"status":"unsubscribed"}');
    await closing;
    expect(events, isEmpty);
    await listener.cancel();
    final fresh = await _subscribe(peer, _bootstrap([_session('old', '1')]));
    expect(fresh.initial.sessions.single.id, 'old');
    final value = Completer<StateEvent>();
    final freshListener = fresh.events.listen(value.complete);
    peer.socket.add(_changed('old', '2'));
    expect((await value.future as SessionChanged).session.revision, BigInt.two);
    await _retire(peer, fresh);
    await freshListener.cancel();
    // An old event after the ACK is a protocol violation even without an RPC.
    final rpc = peer.client.daemonStatus();
    await peer.next();
    final failed = expectLater(rpc, throwsA(isA<IpcProtocolException>()));
    peer.socket.add(_removed('old', 3));
    await failed;
  });

  test('stream cancel awaits unsubscribe ACK; concurrent client close interrupts barrier', () async {
    final peer = await _Peer.open();
    addTearDown(peer.close);
    final owned = await _subscribe(peer);
    final listener = owned.events.listen((_) {});
    var canceled = false;
    final cancel = listener.cancel().then((_) {
      canceled = true;
    });
    final request = await peer.next();
    expect(request['method'], 'state.unsubscribe');
    expect(canceled, isFalse);
    peer.socket.add(_changed('s', '1'));
    peer.ok(request, '{"status":"unsubscribed"}');
    await cancel;
    final fresh = await _subscribe(peer);
    final paused = fresh.events.listen((_) {}, onError: (Object _) {});
    paused.pause();
    final retiring = fresh.close();
    final expected = expectLater(
      retiring,
      throwsA(isA<IpcTransportException>()),
    );
    await peer.next();
    await Future.wait([peer.client.close(), peer.client.close()]);
    await expected;
    await paused.cancel();
  });

  test('business rejection reuses socket; overlapping RPC/subscribe safely rejected', () async {
    final peer = await _Peer.open();
    addTearDown(peer.close);
    final subscribing = peer.client.subscribeStateEvents();
    final rejected = expectLater(
      subscribing,
      throwsA(
        isA<IpcRequestFailure>().having(
          (e) => e.code,
          'code',
          'state_snapshot_unavailable',
        ),
      ),
    );
    final request = await peer.next();
    await expectLater(
      peer.client.daemonStatus(),
      throwsA(isA<IpcProtocolException>()),
    );
    await expectLater(
      peer.client.subscribeStateEvents(),
      throwsA(isA<IpcProtocolException>()),
    );
    peer.socket.add(
      jsonEncode({
        'kind': 'response',
        'id': request['id'],
        'ok': false,
        'error': {
          'code': 'state_snapshot_unavailable',
          'message': 'private token endpoint',
        },
      }),
    );
    await rejected;
    await peer.barrier();
    final owned = await _subscribe(peer);
    await expectLater(
      peer.client.subscribeStateEvents(),
      throwsA(isA<IpcProtocolException>()),
    );
    // A locally oversized unsent request does not corrupt the connection.
    await expectLater(
      peer.client.sessionStop('x' * 65536),
      throwsA(isA<IpcProtocolException>()),
    );
    await peer.barrier();
    await _retire(peer, owned);
  });

  test('malformed ACK and event/binary/oversize/wrongID invalidate pending RPC and stream', () async {
    for (final bad in [
      'null',
      '{"sessions":null,"network":{}}',
      '{"sessions":{"sessions":[],"cleanupRequiredSessionIds":[]},"network":{"available":false,"revision":0,"interfaces":[]},"extra":1}',
    ]) {
      final peer = await _Peer.open();
      try {
        final future = peer.client.subscribeStateEvents();
        final expected = expectLater(
          future,
          throwsA(isA<IpcProtocolException>()),
        );
        peer.ok(await peer.next(), bad);
        await expected;
        await expectLater(
          peer.client.daemonStatus(),
          throwsA(isA<IpcProtocolException>()),
        );
      } finally {
        await peer.close();
      }
    }
    for (final bad in <Object>[
      '{"kind":"event","method":"unknown","payload":{}}',
      '{"kind":"event","id":"r","method":"session.removed","payload":{"sessionId":"s","revision":1}}',
      '{"kind":"event","method":"session.removed","payload":{"sessionId":"s","revision":1e0}}',
      '{"kind":"event","method":"session.removed","payload":{"sessionId":"s","revision":1,"revision":2}}',
      <int>[1, 2, 3],
      'x' * 65537,
      '{"kind":"response","id":"wrong","ok":true,"result":{}}',
    ]) {
      final peer = await _Peer.open();
      try {
        final owned = await _subscribe(peer);
        final ended = expectLater(
          owned.events.toList(),
          throwsA(isA<IpcProtocolException>()),
        );
        final rpc = peer.client.daemonStatus();
        final failed = expectLater(rpc, throwsA(isA<IpcProtocolException>()));
        await peer.next();
        peer.socket.add(bad);
        await failed;
        await ended;
        await expectLater(
          peer.client.daemonStatus(),
          throwsA(isA<IpcProtocolException>()),
        );
      } finally {
        await peer.close();
      }
    }
  });

  test('preACK event is rejected and terminal coalesces pending changed at equal revision', () async {
    final early = await _Peer.open();
    try {
      final acquiring = early.client.subscribeStateEvents();
      final rejected = expectLater(
        acquiring,
        throwsA(isA<IpcProtocolException>()),
      );
      await early.next();
      early.socket.add(_removed('early', 1));
      await rejected;
    } finally {
      await early.close();
    }
    final peer = await _Peer.open();
    addTearDown(peer.close);
    final owned = await _subscribe(peer);
    peer.socket.add(_changed('s', '1'));
    peer.socket.add(_removed('s', 1));
    peer.socket.add(_changed('s', '2'));
    await peer.barrier();
    final delivered = Completer<StateEvent>();
    final listener = owned.events.listen(delivered.complete);
    expect(() => owned.events.listen((_) {}), throwsStateError);
    expect(
      await delivered.future,
      isA<SessionRemoved>().having((e) => e.revision, 'revision', BigInt.one),
    );
    await _retire(peer, owned);
    await listener.cancel();
  });

  test('bad/missing unsubscribe ACK invalidates instead of exposing a reusable connection', () async {
    for (final bad in [
      '{"status":"other"}',
      'null',
      '{"status":"unsubscribed","extra":true}',
    ]) {
      final peer = await _Peer.open();
      try {
        final owned = await _subscribe(peer);
        final closing = owned.close();
        final failed = expectLater(
          closing,
          throwsA(isA<IpcProtocolException>()),
        );
        peer.ok(await peer.next(), bad);
        await failed;
        await expectLater(
          peer.client.daemonStatus(),
          throwsA(isA<IpcProtocolException>()),
        );
      } finally {
        await peer.close();
      }
    }
    final peer = await _Peer.open(timeout: const Duration(milliseconds: 100));
    addTearDown(peer.close);
    final owned = await _subscribe(peer);
    final closing = owned.close();
    final failed = expectLater(
      closing,
      throwsA(
        isA<IpcTransportException>().having(
          (e) => e.code,
          'code',
          'ipc_timeout',
        ),
      ),
    );
    await peer.next();
    await failed;
    await peer.client.close();
  });

  test('timeout/peerclose/concurrent close resolve acquired work; fresh connection restores bootstrap', () async {
    for (final mode in ['timeout', 'peerclose', 'close']) {
      final peer = await _Peer.open(timeout: const Duration(milliseconds: 100));
      try {
        final rpc = peer.client.subscribeStateEvents();
        final failed = expectLater(rpc, throwsA(isA<IpcTransportException>()));
        await peer.next();
        if (mode == 'peerclose') await peer.socket.close();
        if (mode == 'close') {
          await Future.wait([peer.client.close(), peer.client.close()]);
        }
        await failed;
      } finally {
        await peer.close();
      }
    }
    final restored = await _Peer.open();
    addTearDown(restored.close);
    final fresh = await _subscribe(
      restored,
      _bootstrap([_session('restored', '18446744073709551615')], ['restored']),
    );
    expect(fresh.initial.sessions.single.cleanupRequired, isTrue);
    expect(
      fresh.initial.sessions.single.revision,
      BigInt.parse('18446744073709551615'),
    );
    await _retire(restored, fresh);
  });

  test('three independent resource bounds discontinue with fixed reconnect-needed', () async {
    for (final kind in ['live', 'terminal', 'pending']) {
      final peer = await _Peer.open();
      try {
        final owned = await _subscribe(peer);
        // Keep pending bounded ourselves for live/history proof, and unlistened
        // for pending proof. No private counters or production hooks are used.
        final failure = Completer<Object>();
        StreamSubscription<StateEvent>? listener;
        if (kind != 'pending') {
          listener = owned.events.listen((_) {}, onError: failure.complete);
        }
        if (kind == 'live') {
          for (var i = 0; i < 256; i++) {
            peer.socket.add(_changed('s$i', '1'));
          }
        } else if (kind == 'terminal') {
          for (var i = 0; i < 257; i++) {
            peer.socket.add(_removed('s$i', 1));
          }
        } else {
          for (var i = 0; i < 128; i++) {
            peer.socket.add(_removed('gone$i', 1));
          }
          for (var i = 0; i < 129; i++) {
            peer.socket.add(_changed('live$i', '1'));
          }
          listener = owned.events.listen((_) {}, onError: failure.complete);
          listener.pause();
          // Pause before socket callbacks run; retains one bounded pending map.
          final rejected = expectLater(
            peer.client.daemonStatus(),
            throwsA(isA<IpcTransportException>()),
          );
          await peer.next();
          await rejected;
          listener.resume();
        }
        expect(
          await failure.future,
          isA<IpcTransportException>().having(
            (e) => e.code,
            'code',
            'ipc_reconnect_needed',
          ),
        );
        await peer.client.close();
        await listener?.cancel();
        await expectLater(
          peer.client.daemonStatus(),
          throwsA(isA<IpcProtocolException>()),
        );
      } finally {
        await peer.close();
      }
    }
  });
}

const _status =
    '{"productVersion":"v","buildId":"b","pid":1,"status":"running","mode":"headless"}';
String _session(String id, String revision) =>
    '{"sessionId":${jsonEncode(id)},"displayName":"","institutionProfileId":"i","institutionDisplayName":"Institution","authenticationProtocolId":"p","accountName":"a","intent":"suspend_authentication","state":"suspended","revision":$revision,"updatedAt":"2026-10-07T01:02:03Z","protocolSocket":{"state":"not_observed","runGeneration":9007199254740993,"updatedAt":"2026-10-07T01:02:03Z"}}';
String _bootstrap([
  List<String> sessions = const [],
  List<String> cleanup = const [],
]) =>
    '{"sessions":{"sessions":[${sessions.join(',')}],"cleanupRequiredSessionIds":${jsonEncode(cleanup)}},"network":{"available":false,"revision":0,"interfaces":[]}}';
String _changed(String id, String revision, {bool cleanup = false}) =>
    '{"kind":"event","method":"session.changed","payload":{"session":${_session(id, revision)},"cleanupRequired":$cleanup}}';
String _removed(String id, int revision) => jsonEncode({
  'kind': 'event',
  'method': 'session.removed',
  'payload': {'sessionId': id, 'revision': revision},
});
String _network(int revision) =>
    '{"kind":"event","method":"network.changed","payload":{"available":true,"revision":$revision,"observedAt":"2026-10-07T01:02:03Z","interfaces":[]}}';

Future<StateSubscription> _subscribe(_Peer peer, [String? bootstrap]) async {
  final future = peer.client.subscribeStateEvents();
  peer.ok(await peer.next(), bootstrap ?? _bootstrap());
  return future;
}

Future<void> _retire(_Peer peer, StateSubscription owned) async {
  final closing = owned.close();
  final request = await peer.next();
  expect(request['method'], 'state.unsubscribe');
  peer.ok(request, '{"status":"unsubscribed"}');
  await closing;
  await owned.close();
}

class _Peer {
  _Peer(this.server, this.socket, this.client)
    : requests = StreamIterator(
        socket.map(
          (message) => jsonDecode(message as String) as Map<String, dynamic>,
        ),
      );
  final HttpServer server;
  final WebSocket socket;
  final WebSocketIpcClient client;
  final StreamIterator<Map<String, dynamic>> requests;
  static Future<_Peer> open({
    Duration timeout = const Duration(seconds: 5),
  }) async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final connected = Completer<WebSocket>();
    server.listen((request) async {
      connected.complete(await WebSocketTransformer.upgrade(request));
    });
    final client = await WebSocketIpcClient.connect(
      GuiBootstrap(
        endpoint: Uri.parse('ws://127.0.0.1:${server.port}/ipc'),
        token: 'fixture-token',
        productVersion: 'v',
        buildId: 'b',
        daemonPid: 1,
        mode: 'desktop',
      ),
      requestTimeout: timeout,
    );
    return _Peer(server, await connected.future, client);
  }

  Future<Map<String, dynamic>> next() async {
    expect(await requests.moveNext(), isTrue);
    return requests.current;
  }

  void ok(Map<String, dynamic> request, String result) => socket.add(
    '{"kind":"response","id":${jsonEncode(request['id'])},"ok":true,"result":$result}',
  );
  Future<void> barrier() async {
    final future = client.daemonStatus();
    ok(await next(), _status);
    await future;
  }

  Future<void> close() async {
    await client.close();
    await requests.cancel();
    await socket.close();
    await server.close(force: true);
  }
}
