import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/state_models.dart';
import 'package:sidravia_gui/ipc/state_subscription.dart';

void main() {
  test('production bootstrap uses ordered metadata and full state, including zero-session network', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final calls = <String>[];
    final connected = Completer<WebSocket>();
    WebSocket? currentSocket;
    server.listen((request) async {
      final socket = await WebSocketTransformer.upgrade(request);
      currentSocket = socket;
      if (!connected.isCompleted) connected.complete(socket);
      socket.listen((message) {
        final request = jsonDecode(message as String) as Map<String, dynamic>;
        final method = request['method'] as String;
        calls.add(method);
        final result = switch (method) {
          'daemon.status' => '{"productVersion":"v","buildId":"b","pid":1,"status":"running","mode":"desktop","desktopOwnerPid":2}',
          'profile.list' => '{"profiles":[]}',
          'configuration.list' =>
            '{"storageProtection":"protected","configurations":[]}',
          'state.subscribe' => '{"sessions":{"sessions":[],"cleanupRequiredSessionIds":[]},"network":{"available":false,"revision":0,"interfaces":[]}}',
          _ => throw StateError('Unexpected RPC $method'),
        };
        socket.add(
          '{"kind":"response","id":${jsonEncode(request['id'])},"ok":true,"result":$result}',
        );
      });
    });
    final c = GuiController(
      bootstrapper: _Bootstrap(server.port),
      reconnectDelay: const Duration(milliseconds: 20),
    );
    addTearDown(() async {
      await c.close();
      c.dispose();
      await currentSocket?.close();
      await server.close(force: true);
    });
    await c.start();
    expect(c.state, GuiConnectionState.ready);
    expect(calls, [
      'daemon.status',
      'profile.list',
      'configuration.list',
      'state.subscribe',
    ]);
    expect(c.snapshot!.sessions, isEmpty);
    expect(c.snapshot!.network!.available, false);
    final delivered = Completer<void>();
    c.addListener(() {
      if (c.snapshot?.network?.available == true && !delivered.isCompleted) {
        delivered.complete();
      }
    });
    (await connected.future).add(
      '{"kind":"event","method":"network.changed","payload":{"available":true,"revision":9007199254740993,"observedAt":"2026-10-08T01:02:03Z","interfaces":[]}}',
    );
    await delivered.future;
    expect(c.snapshot!.network!.revision, BigInt.parse('9007199254740993'));
    expect(calls, hasLength(4));
    final stale = Completer<void>();
    final recovered = Completer<void>();
    c.addListener(() {
      if (c.state == GuiConnectionState.stale && !stale.isCompleted) {
        stale.complete();
      }
      if (c.state == GuiConnectionState.ready &&
          calls.length == 8 &&
          !recovered.isCompleted) {
        recovered.complete();
      }
    });
    currentSocket!.add('{"kind":"event","method":"unknown","payload":{}}');
    await stale.future;
    expect(c.failure!.code, 'ipc_protocol_error');
    await recovered.future;
    expect(calls.skip(4), [
      'daemon.status',
      'profile.list',
      'configuration.list',
      'state.subscribe',
    ]);
    expect(c.snapshot!.network!.available, false);
  });

  test('complete changed, same-revision cleanup, removed and network project live without polling', () async {
    final client = _LiveClient();
    final c = _controller(client);
    await c.start();
    final revision = BigInt.parse('18446744073709551615');
    client.owner.emit(SessionChanged(_session(revision)));
    expect(c.snapshot!.sessions.single.protocolSocket.runGeneration, revision);
    client.owner.emit(SessionChanged(_session(revision, cleanup: true)));
    expect(c.snapshot!.sessions.single.cleanupRequired, true);
    expect(c.capabilities.canConnect, false);
    expect(c.capabilities.canManage, true);
    expect(c.capabilities.canResetSession, true);
    client.owner.emit(NetworkChanged(_network(7)));
    client.owner.emit(SessionRemoved(sessionId: 's', revision: revision));
    expect(c.snapshot!.sessions, isEmpty);
    expect(c.snapshot!.network!.revision, BigInt.from(7));
    expect(c.capabilities.canConnect, true);
    expect(client.calls, ['daemon', 'profiles', 'configurations', 'subscribe']);
    await c.close();
    expect(client.closed, true);
    expect(client.owner.closed, true);
    c.dispose();
    client.dispose();
  });

  test('live removals during sequential metadata refresh survive mutation completion', () async {
    final client = _LiveClient(initialSessions: [_session(BigInt.one)]);
    final c = _controller(client);
    await c.start();
    client.calls.clear();
    client.metadataGate = Completer<void>();
    final mutation = c.setAutoReconnect(
      configurationId: 'demo-config',
      autoReconnect: true,
    );
    await pumpEventQueue();
    expect(c.busy, true);
    client.owner.emit(SessionRemoved(sessionId: 's', revision: BigInt.one));
    client.owner.emit(NetworkChanged(_network(9)));
    expect(await c.startConfiguration('demo-config'), false);
    client.metadataGate!.complete();
    expect(await mutation, true);
    expect(c.snapshot!.sessions, isEmpty);
    expect(c.snapshot!.network!.revision, BigInt.from(9));
    expect(client.calls, ['daemon', 'profiles', 'configurations']);
    expect(client.subscribes, 1);
    expect(client.operations.where((op) => op.startsWith('session.')), isEmpty);
    await c.close();
    c.dispose();
    client.dispose();
  });

  test('committed cleanup failure requeries metadata and blocks reuse while stop and cleanup remain safe', () async {
    final client = _LiveClient(
      initialSessions: [
        _session(BigInt.one, state: SessionState.authenticated),
      ],
    );
    final c = _controller(client);
    await c.start();
    client.committedFailure = true;
    client.calls.clear();
    expect(
      await c.updateConfiguration(
        configurationId: 'demo-config',
        institutionProfileId: 'jlu',
        username: 'saved',
      ),
      false,
    );
    expect(c.snapshot!.configurations.single.username, 'saved');
    expect(c.snapshot!.sessions.single.cleanupRequired, true);
    expect(c.sessionNeedsReset, true);
    expect(c.notice, contains('配置已提交'));
    expect(_callsWithoutSubscribe(client), [
      'daemon',
      'profiles',
      'configurations',
    ]);
    expect(client.subscribes, 1);
    expect(await c.restartSession('s'), false);
    expect(c.connectionPresentation.primaryEnabled, true);
    expect(await c.stopSession('s'), true);
    expect(await c.resetSession('s'), true);
    expect(c.sessionNeedsReset, false);
    await c.close();
    c.dispose();
    client.dispose();
  });

  test('stream loss retains stale display, closes epoch and automatically recovers full state', () async {
    final first = _LiveClient(initialSessions: [_session(BigInt.from(9))]);
    final second = _LiveClient(
      initialSessions: [_session(BigInt.one)],
      network: _network(2),
    );
    var attempts = 0;
    final c = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => attempts++ == 0 ? first : second,
      reconnectDelay: const Duration(milliseconds: 20),
    );
    await c.start();
    var retainedStaleDisplay = false;
    c.addListener(() {
      if (c.state == GuiConnectionState.stale &&
          c.snapshot?.sessions.singleOrNull?.revision == BigInt.from(9)) {
        retainedStaleDisplay = true;
      }
    });
    final recovered = _waitForController(
      c,
      () => c.state == GuiConnectionState.ready && attempts == 2,
    );
    first.owner.fail();
    final stale = _waitForController(
      c,
      () => c.state == GuiConnectionState.stale,
    );
    await stale;
    expect(c.snapshot!.sessions.single.revision, BigInt.from(9));
    expect(c.capabilities.canManage, false);
    expect(first.closed, true);
    await recovered;
    expect(c.state, GuiConnectionState.ready);
    expect(retainedStaleDisplay, true);
    expect(second.calls, ['daemon', 'profiles', 'configurations', 'subscribe']);
    expect(c.snapshot!.sessions.single.revision, BigInt.one);
    expect(c.snapshot!.network!.revision, BigInt.two);
    expect(attempts, 2);
    await c.close();
    c.dispose();
    first.dispose();
    second.dispose();
  });

  test(
    'manual retry cancels backoff and teardown prevents further attempts',
    () async {
      final first = _LiveClient();
      final second = _LiveClient();
      var attempts = 0;
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => attempts++ == 0 ? first : second,
      );
      await c.start();
      final stale = _waitForController(
        c,
        () => c.state == GuiConnectionState.stale,
      );
      first.owner.fail();
      await stale;
      final recovered = _waitForController(
        c,
        () => c.state == GuiConnectionState.ready && attempts == 2,
      );
      await c.retry();
      await recovered;
      expect(c.state, GuiConnectionState.ready);
      expect(attempts, 2);
      final staleAgain = _waitForController(
        c,
        () => c.state == GuiConnectionState.stale,
      );
      second.owner.fail();
      await staleAgain;
      await c.close();
      c.dispose();
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(attempts, 2);
      first.dispose();
      second.dispose();
    },
  );

  test(
    'awaited close waits for late connector and rejects its stale epoch',
    () async {
      final late = _LiveClient();
      final connecting = Completer<SidraviaIpcClient>();
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) => connecting.future,
      );
      final starting = c.start();
      await pumpEventQueue();
      var completed = false;
      final closing = c.close().then((_) => completed = true);
      await pumpEventQueue();
      expect(completed, false);
      connecting.complete(late);
      await starting;
      await closing;
      expect(late.closed, true);
      expect(late.calls, isEmpty);
      expect(late.subscribes, 0);
      c.dispose();
      late.dispose();
    },
  );

  test(
    'dispose cancels pending state acquisition and awaits all owned resources',
    () async {
      final client = _LiveClient()..subscribeGate = Completer<void>();
      final c = _controller(client);
      final starting = c.start();
      await pumpEventQueue();
      expect(client.subscribes, 1);
      c.dispose();
      await c.close();
      await starting;
      expect(client.closed, true);
      expect(c.state, isNot(GuiConnectionState.ready));
      expect(client.owner.closed, true);
      client.dispose();
    },
  );

  test('reconnect recovery excludes mutations until complete and close stops its pending acquisition', () async {
    final first = _LiveClient();
    final second = _LiveClient()..subscribeGate = Completer<void>();
    var attempts = 0;
    final c = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => attempts++ == 0 ? first : second,
      reconnectDelay: const Duration(milliseconds: 20),
    );
    await c.start();
    first.owner.fail();
    await second.subscribeStarted.future.timeout(const Duration(seconds: 3));
    expect(second.subscribes, 1);
    expect(c.state, GuiConnectionState.stale);
    expect(await c.startConfiguration('demo-config'), false);
    expect(second.operations, isEmpty);
    final closing = c.close();
    await closing;
    expect(second.closed, true);
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(attempts, 2);
    c.dispose();
    first.dispose();
    second.dispose();
  });

  testWidgets('unsupported bootstrap never starts retry timer', (tester) async {
    final boot = _Unsupported();
    final c = GuiController(bootstrapper: boot);
    await c.start();
    expect(c.state, GuiConnectionState.unsupported);
    await tester.pump(const Duration(minutes: 2));
    expect(boot.calls, 1);
    await c.close();
    c.dispose();
  });
}

Future<void> _waitForController(
  GuiController controller,
  bool Function() condition,
) {
  if (condition()) return Future<void>.value();
  final completed = Completer<void>();
  void check() {
    if (!completed.isCompleted && condition()) completed.complete();
  }

  controller.addListener(check);
  return completed.future
      .timeout(const Duration(seconds: 3))
      .whenComplete(() => controller.removeListener(check));
}

List<String> _callsWithoutSubscribe(_LiveClient client) =>
    client.calls.where((call) => call != 'subscribe').toList();
GuiController _controller(_LiveClient client) => GuiController(
  bootstrapper: OfflineDemoBootstrap(),
  connector: (_) async => client,
  pollDelay: const Duration(milliseconds: 1),
);

SessionSummary _session(
  BigInt revision, {
  bool cleanup = false,
  SessionState state = SessionState.suspended,
}) => SessionSummary(
  id: 's',
  configurationId: 'demo-config',
  displayName: '',
  accountName: 'u',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom-5.2.0-d',
  state: state,
  intent: SessionIntent.suspendAuthentication,
  revision: revision,
  cleanupRequired: cleanup,
  protocolSocket: NetworkProtocolSocket(
    state: 'not_observed',
    runGeneration: revision,
  ),
);
NetworkInterfacesSnapshot _network(int revision) => NetworkInterfacesSnapshot(
  available: revision != 0,
  revision: BigInt.from(revision),
  interfaces: const [],
  observedAt: revision == 0 ? null : '2026-10-08T01:02:03Z',
);

class _Owned implements StateSubscription {
  _Owned(this.initial);
  @override
  final StateBootstrap initial;
  final stream = StreamController<StateEvent>(sync: true);
  bool closed = false;
  @override
  Stream<StateEvent> get events => stream.stream;
  void emit(StateEvent event) => stream.add(event);
  void fail() =>
      stream.addError(const IpcTransportException('ipc_disconnected'));
  @override
  Future<void> close() async {
    if (closed) return;
    closed = true;
    final closing = stream.close();
    if (stream.hasListener) await closing;
  }
}

class _LiveClient extends OfflineDemoClient implements SidraviaStateClient {
  _LiveClient({
    List<SessionSummary> initialSessions = const [],
    NetworkInterfacesSnapshot? network,
  }) : owner = _Owned(
         StateBootstrap(
           sessions: initialSessions,
           network: network ?? _network(0),
         ),
       );
  final _Owned owner;
  final calls = <String>[];
  int subscribes = 0;
  final subscribeStarted = Completer<void>();
  Completer<void>? metadataGate, subscribeGate;
  bool committedFailure = false;
  @override
  Future<DaemonStatus> daemonStatus() async {
    calls.add('daemon');
    return super.daemonStatus();
  }

  @override
  Future<List<InstitutionProfile>> profileList() async {
    calls.add('profiles');
    return super.profileList();
  }

  @override
  Future<List<ConfigurationSummary>> configurationList() async {
    calls.add('configurations');
    if (metadataGate != null) await metadataGate!.future;
    return super.configurationList();
  }

  @override
  Future<List<SessionSummary>> sessionList() =>
      throw StateError('Production must subscribe');
  @override
  Future<StateSubscription> subscribeStateEvents() async {
    calls.add('subscribe');
    subscribes++;
    if (!subscribeStarted.isCompleted) subscribeStarted.complete();
    if (subscribeGate != null) await subscribeGate!.future;
    if (closed) throw const IpcTransportException('ipc_disconnected');
    return owner;
  }

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    bool allowInsecureStorage = false,
  }) async {
    final saved = await super.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
      allowInsecureStorage: allowInsecureStorage,
    );
    if (committedFailure) {
      owner.emit(
        SessionChanged(
          _session(
            BigInt.one,
            cleanup: true,
            state: SessionState.authenticated,
          ),
        ),
      );
      throw const IpcRequestFailure(
        'configuration_session_invalidation_failed',
      );
    }
    return saved;
  }

  @override
  Future<SessionSummary> sessionStop(String sessionId) async {
    final stopped = _session(BigInt.two);
    owner.emit(SessionChanged(stopped));
    return stopped;
  }

  @override
  Future<SessionRemoveResult> sessionRemove(String sessionId) async {
    owner.emit(SessionRemoved(sessionId: sessionId, revision: BigInt.two));
    return SessionRemoveResult(sessionId: sessionId, status: 'removed');
  }

  @override
  Future<void> close() async {
    await super.close();
    if (subscribeGate != null && !subscribeGate!.isCompleted) {
      subscribeGate!.complete();
    }
    if (metadataGate != null && !metadataGate!.isCompleted) {
      metadataGate!.complete();
    }
    await owner.close();
  }
}

class _Bootstrap implements GuiBootstrapper {
  _Bootstrap(this.port);
  final int port;
  @override
  Future<GuiBootstrapResult> bootstrap() async => GuiBootstrapResult.success(
    GuiBootstrap(
      endpoint: Uri.parse('ws://127.0.0.1:$port/ipc'),
      token: 'fixture',
      productVersion: 'v',
      buildId: 'b',
      daemonPid: 1,
      mode: 'desktop',
    ),
  );
}

class _Unsupported implements GuiBootstrapper {
  int calls = 0;
  @override
  Future<GuiBootstrapResult> bootstrap() async {
    calls++;
    return const GuiBootstrapResult.failure(
      GuiBootstrapFailure.unsupportedPlatform,
    );
  }
}
