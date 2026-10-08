import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/state_models.dart';
import 'package:sidravia_gui/ipc/state_subscription.dart';

void main() {
  testWidgets(
    'production diagnostics queries and displays typed facts, probe is explicit',
    (tester) async {
      final client = _Client();
      final c = _controller(client);
      await c.start();
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AdvancedPage(
              controller: c,
              detailsOnly: false,
              onBack: () {},
            ),
          ),
        ),
      );
      await _pumpUntil(
        tester,
        () =>
            client.requests.length == 1 &&
            c.diagnosis != null &&
            !c.diagnosisBusy &&
            !c.diagnosisStale,
      );
      expect(client.requests, [(null, 's', false)]);
      expect(find.text('2026-10-08T01:02:03Z'), findsOneWidget);
      expect(find.text('192.168.1.2'), findsOneWidget);
      expect(find.text('1 / 2 / 3'), findsOneWidget);
      expect(find.text('192.168.1.2:61440'), findsOneWidget);
      expect(find.text('10.0.0.1:61440'), findsNWidgets(2));
      final probe = find.text('执行有限 IP 探测');
      await tester.ensureVisible(probe);
      await tester.tap(probe);
      await _pumpUntil(
        tester,
        () =>
            client.requests.isNotEmpty &&
            client.requests.last.$3 &&
            !c.diagnosisBusy &&
            !c.diagnosisStale,
      );
      expect(client.requests.last, (null, 's', true));
      expect(find.text('未收到回显，结果不确定'), findsOneWidget);
      client.owner.stream.add(NetworkChanged(_network(2)));
      await _pumpUntil(
        tester,
        () =>
            client.requests.length == 3 &&
            !c.diagnosisBusy &&
            !c.diagnosisStale,
      );
      expect(client.requests.last, (null, 's', false));
      await tester.pumpWidget(const SizedBox());
      final count = client.requests.length;
      client.owner.stream.add(NetworkChanged(_network(3)));
      await tester.pump(const Duration(milliseconds: 1));
      expect(client.requests.length, count);
      await tester.runAsync(c.close);
      c.dispose();
      client.dispose();
    },
  );

  test('single configuration diagnosis ignores runtime availability; empty and ambiguous targets do not query', () async {
    final client = _Client(sessions: const []);
    client.configs = [
      const ConfigurationSummary(
        id: 'demo-config',
        displayName: '',
        institutionProfileId: 'jlu',
        institutionDisplayName: '吉林大学',
        authenticationProtocolId: 'drcom-5.2.0-d',
        username: 'u',
        credentialStored: false,
        storageProtection: 'protected',
        runtimeAvailability:
            ConfigurationRuntimeAvailability.profileUnavailable,
      ),
    ];
    final c = _controller(client);
    await c.start();
    c.setDiagnosisVisible(true);
    await _settle(c);
    expect(client.requests.single, ('demo-config', null, false));
    expect(c.diagnosisStale, false);
    await c.close();
    c.dispose();
    client.dispose();
    for (final sessions in [
      <SessionSummary>[],
      [_session('other')],
    ]) {
      final empty = _Client(sessions: sessions)..configs = [];
      final controller = _controller(empty);
      await controller.start();
      controller.setDiagnosisVisible(true);
      await pumpEventQueue();
      expect(controller.diagnosisUnavailable, contains('连接配置'));
      expect(empty.requests, isEmpty);
      await controller.close();
      controller.dispose();
      empty.dispose();
    }
  });

  testWidgets('preview and unsupported diagnosis give explicit guidance', (
    tester,
  ) async {
    final preview = OfflineDemoClient();
    final c = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => preview,
      pollDelay: const Duration(days: 1),
    );
    await c.start();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AdvancedPage(controller: c, detailsOnly: false, onBack: () {}),
        ),
      ),
    );
    await tester.pump();
    expect(find.textContaining('离线预览没有真实网络观察'), findsOneWidget);
    expect(c.canDiagnose, false);
    await tester.pumpWidget(const SizedBox());
    await c.close();
    c.dispose();
    preview.dispose();
    final client = _Client()
      ..result = const NetworkDiagnosis(
        observedAt: '2026-10-08T01:02:03Z',
        selectionBasis: 'session_binding',
        status: 'unsupported',
        unsupportedReason: 'platform',
        target: NetworkEndpoint('10.0.0.1', 61440),
      );
    final real = _controller(client);
    await real.start();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AdvancedPage(
            controller: real,
            detailsOnly: false,
            onBack: () {},
          ),
        ),
      ),
    );
    await _pumpUntil(
      tester,
      () => real.diagnosis != null && !real.diagnosisStale,
    );
    expect(find.text('当前平台不支持路由诊断'), findsOneWidget);
    expect(find.text('尚无协议 socket 观察'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    await tester.runAsync(real.close);
    real.dispose();
    client.dispose();
  });

  test('events coalesce, reject late run and target responses, and never repeat probe', () async {
    final client = _Client();
    final c = _controller(client);
    await c.start();
    c.setDiagnosisVisible(true);
    await _settle(c);
    final old = c.diagnosis;
    client.gate = Completer<NetworkDiagnosis>();
    c.refreshDiagnosis(probe: true);
    await pumpEventQueue();
    client.owner.stream.add(NetworkChanged(_network(2)));
    client.owner.stream.add(NetworkChanged(_network(3)));
    client.owner.stream.add(
      SessionRemoved(sessionId: 's', revision: BigInt.from(2)),
    );
    client.owner.stream.add(SessionChanged(_session('new', revision: 2)));
    expect(c.diagnosisStale, true);
    expect(identical(c.diagnosis, old), true);
    expect(client.requests.length, 2);
    client.gate!.complete(_result);
    await _settle(c);
    expect(client.requests.length, 3);
    expect(client.requests.last, (null, 'new', false));
    expect(c.diagnosisTargetLabel, '会话 new');
    expect(client.maxConcurrent, 1);
    await c.close();
    c.dispose();
    client.dispose();
  });

  test('mutation waits for diagnosis, saves invalidate, hidden responses stay stale', () async {
    final client = _Client();
    final c = _controller(client);
    await c.start();
    c.setDiagnosisVisible(true);
    await _settle(c);
    client.gate = Completer<NetworkDiagnosis>();
    c.refreshDiagnosis();
    await pumpEventQueue();
    final mutation = c.setAutoReconnect(
      configurationId: 'demo-config',
      autoReconnect: true,
    );
    await pumpEventQueue();
    expect(client.operations, isEmpty);
    client.gate!.complete(_result);
    expect(await mutation, true);
    await _settle(c);
    expect(client.maxConcurrent, 1);
    expect(client.operations, contains('configuration.set_auto_reconnect'));
    client.gate = Completer<NetworkDiagnosis>();
    c.refreshDiagnosis();
    await pumpEventQueue();
    c.setDiagnosisVisible(false);
    client.gate!.complete(_result);
    await pumpEventQueue();
    expect(c.diagnosisStale, true);
    expect(client.closed, false);
    await c.close();
    c.dispose();
    client.dispose();
  });

  test('business errors retain code; transport loss disables actions and stale facts', () async {
    final client = _Client();
    final c = _controller(client);
    await c.start();
    c.setDiagnosisVisible(true);
    await _settle(c);
    client.error = const IpcRequestFailure('session_not_found');
    c.refreshDiagnosis();
    await _settle(c);
    expect(c.diagnosisError, contains('session_not_found'));
    expect(c.diagnosisStale, true);
    expect(client.closed, false);
    client.error = const IpcTransportException('ipc_disconnected');
    c.refreshDiagnosis();
    await pumpEventQueue();
    expect(c.state, GuiConnectionState.stale);
    expect(c.canDiagnose, false);
    expect(c.diagnosisStale, true);
    expect(client.closed, true);
    await c.close();
    c.dispose();
    client.dispose();
  });

  test(
    'close owns an outstanding diagnosis and ignores its late response',
    () async {
      final client = _Client();
      final c = _controller(client);
      await c.start();
      client.gate = Completer<NetworkDiagnosis>();
      c.setDiagnosisVisible(true);
      await pumpEventQueue();
      final closing = c.close();
      client.gate!.complete(_result);
      await closing;
      expect(c.diagnosis, null);
      expect(client.closed, true);
      c.dispose();
      client.dispose();
    },
  );
}

Future<void> _settle(GuiController c) async {
  for (var i = 0; i < 30; i++) {
    await pumpEventQueue(times: 2);
    if (!c.diagnosisBusy) return;
  }
  fail('Diagnosis did not settle');
}

Future<void> _pumpUntil(
  WidgetTester tester,
  bool Function() condition, {
  Duration timeout = const Duration(seconds: 3),
}) async {
  const step = Duration(milliseconds: 1);
  var elapsed = Duration.zero;
  while (!condition() && elapsed < timeout) {
    await tester.pump(step);
    elapsed += step;
  }
  await tester.pump();
  expect(
    condition(),
    isTrue,
    reason: 'Widget state did not settle in $timeout',
  );
}

GuiController _controller(_Client client) => GuiController(
  bootstrapper: OfflineDemoBootstrap(),
  connector: (_) async => client,
  pollDelay: const Duration(days: 1),
  reconnectDelay: const Duration(days: 1),
);
SessionSummary _session(String id, {int revision = 1}) => SessionSummary(
  id: id,
  configurationId: 'demo-config',
  displayName: '',
  accountName: 'u',
  state: SessionState.suspended,
  intent: SessionIntent.suspendAuthentication,
  revision: BigInt.from(revision),
  protocolSocket: NetworkProtocolSocket(
    state: 'not_observed',
    runGeneration: BigInt.from(revision),
  ),
);
NetworkInterfacesSnapshot _network(int revision) => NetworkInterfacesSnapshot(
  available: true,
  revision: BigInt.from(revision),
  observedAt: '2026-10-08T01:02:03Z',
  interfaces: const [],
);
const _result = NetworkDiagnosis(
  observedAt: '2026-10-08T01:02:03Z',
  selectionBasis: 'session_binding',
  status: 'available',
  target: NetworkEndpoint('10.0.0.1', 61440),
  route: NetworkDiagnosticRoute(
    interfaceId: 'if-1',
    interfaceIndex: 7,
    sourceIPv4: '192.168.1.2',
    destinationPrefix: '10.0.0.0/8',
    nextHopIPv4: '192.168.1.1',
    routeMetric: 1,
    interfaceMetric: 2,
    effectiveMetric: 3,
  ),
  probe: NetworkDiagnosticProbe('not_requested', null),
);

class _Owner implements StateSubscription {
  _Owner(List<SessionSummary> sessions)
    : initial = StateBootstrap(sessions: sessions, network: _network(1));
  @override
  final StateBootstrap initial;
  final stream = StreamController<StateEvent>(sync: true);
  @override
  Stream<StateEvent> get events => stream.stream;
  @override
  Future<void> close() async {
    await stream.close();
  }
}

class _Client extends OfflineDemoClient
    implements SidraviaNetworkClient, SidraviaStateClient {
  _Client({List<SessionSummary>? sessions})
    : owner = _Owner(sessions ?? [_session('s')]);
  final _Owner owner;
  List<ConfigurationSummary>? configs;
  final requests = <(String?, String?, bool)>[];
  Completer<NetworkDiagnosis>? gate;
  Object? error;
  NetworkDiagnosis result = _result;
  int concurrent = 0, maxConcurrent = 0;
  @override
  Future<List<ConfigurationSummary>> configurationList() async =>
      configs ?? await super.configurationList();
  @override
  Future<StateSubscription> subscribeStateEvents() async => owner;
  @override
  Future<NetworkDiagnosis> networkDiagnose({
    String? configurationId,
    String? sessionId,
    bool probe = false,
  }) async {
    requests.add((configurationId, sessionId, probe));
    concurrent++;
    if (concurrent > maxConcurrent) maxConcurrent = concurrent;
    final pending = gate;
    final failure = error;
    error = null;
    try {
      if (failure != null) throw failure;
      if (pending != null) return await pending.future;
      if (result.status != 'available') return result;
      return NetworkDiagnosis(
        observedAt: result.observedAt,
        selectionBasis: result.selectionBasis,
        status: result.status,
        target: result.target,
        route: result.route,
        probe: NetworkDiagnosticProbe(
          probe ? 'no_reply' : 'not_requested',
          null,
        ),
        protocolSocket: NetworkProtocolSocket(
          state: 'open',
          runGeneration: BigInt.one,
          localEndpoint: const NetworkEndpoint('192.168.1.2', 61440),
          remoteEndpoint: const NetworkEndpoint('10.0.0.1', 61440),
        ),
      );
    } finally {
      concurrent--;
      if (identical(gate, pending)) gate = null;
    }
  }

  @override
  Future<NetworkInterfacesSnapshot> networkInterfaces() async => _network(1);
  @override
  Future<ConfigurationSummary> configurationSetNetworkBindingPolicy({
    required String configurationId,
    required NetworkBindingPolicy policy,
    bool allowInsecureStorage = false,
  }) => throw UnimplementedError();
}
