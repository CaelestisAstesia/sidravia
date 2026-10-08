import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/gui_operation.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/configuration/network_binding_section.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/state_models.dart';
import 'package:sidravia_gui/ipc/state_subscription.dart';

void main() {
  testWidgets(
    'production form binding save preserves account and password drafts',
    (tester) async {
      final client = _Client();
      final c = _controller(client);
      await c.start();
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ConfigurationPage(controller: c, onBack: () {}),
          ),
        ),
      );
      await tester.pump();
      await tester.enterText(find.byType(TextField).first, 'unsaved-account');
      await tester.enterText(find.byType(TextField).last, 'unsaved-password');
      final menu = find.byType(DropdownButtonFormField<NetworkBindingPolicy>);
      await tester.ensureVisible(menu);
      await tester.tap(menu);
      await tester.pumpAndSettle();
      expect(
        find.text('192.168.1.2/24 · Ethernet · if-1').last,
        findsOneWidget,
      );
      expect(find.textContaining('if-down'), findsNothing);
      expect(find.textContaining('192.168.1.3'), findsNothing);
      await tester.tap(find.text('192.168.1.2/24 · Ethernet · if-1').last);
      await tester.pumpAndSettle();
      final save = find.text('保存网络绑定');
      await tester.ensureVisible(save);
      await tester.tap(save);
      await _pumpUntil(tester, () => !c.busy && client.requests.length == 1);
      expect(client.requests.single, ('demo-config', _explicit, false));
      expect(c.snapshot!.configurations.single.networkBindingPolicy, _explicit);
      expect(c.snapshot!.sessions, isEmpty);
      expect(
        find
            .byType(TextField)
            .evaluate()
            .map((e) => (e.widget as TextField).controller!.text),
        ['unsaved-account', 'unsaved-password'],
      );
      expect(
        client.operations.where(
          (op) => op.startsWith('session.') || op == 'configuration.update',
        ),
        isEmpty,
      );
      expect(find.text('网络绑定已保存；请手动连接以使用新策略。'), findsOneWidget);
      await _finish(tester, c, client);
    },
  );

  testWidgets(
    'narrow binding choices show address first and complete selected identity',
    (tester) async {
      tester.view.physicalSize = const Size(640, 1400);
      tester.view.devicePixelRatio = 2;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      tester.platformDispatcher.textScaleFactorTestValue = 2;
      addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);

      final firstId =
          'ethernet-interface-with-a-very-long-stable-identifier-001';
      final secondId =
          'ethernet-interface-with-a-very-long-stable-identifier-002';
      final network = NetworkInterfacesSnapshot(
        available: true,
        revision: BigInt.one,
        interfaces: [
          for (final (id, address) in [
            (firstId, '192.168.40.11'),
            (secondId, '192.168.40.12'),
          ])
            NetworkInterfaceRow(
              interfaceId: id,
              displayName: 'Campus Ethernet Adapter with a long friendly name',
              operationalState: 'up',
              physicalMedium: 'wired',
              hardwareBacked: true,
              physicalConnectorPresent: true,
              filterInterface: false,
              endpointInterface: false,
              addressAssignmentMethod: 'dhcp',
              ipv4Assignments: [
                NetworkIPv4Assignment(
                  address: address,
                  prefixLength: 24,
                  automaticCandidate: true,
                  explicitBindable: true,
                ),
              ],
            ),
        ],
      );
      final client = _Client(
        policy: NetworkBindingPolicy.explicit(firstId, '192.168.40.11'),
        network: network,
      );
      final c = _controller(client);
      await c.start();
      await tester.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(
              size: Size(320, 700),
              textScaler: TextScaler.linear(2),
            ),
            child: Scaffold(
              body: SingleChildScrollView(
                child: SizedBox(
                  width: 320,
                  child: NetworkBindingSection(controller: c),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pump();
      final dropdown = find.byType(
        DropdownButtonFormField<NetworkBindingPolicy>,
      );
      await tester.ensureVisible(dropdown);
      await tester.tap(dropdown);
      await tester.pumpAndSettle();
      expect(
        find.text(
          '192.168.40.11/24 · Campus Ethernet Adapter with a long friendly name · $firstId',
        ),
        findsNWidgets(2),
      );
      expect(
        find.text(
          '192.168.40.12/24 · Campus Ethernet Adapter with a long friendly name · $secondId',
        ),
        findsOneWidget,
      );
      await tester.tap(
        find
            .text(
              '192.168.40.12/24 · Campus Ethernet Adapter with a long friendly name · $secondId',
            )
            .last,
      );
      await tester.pump();
      expect(
        find.text('网卡：Campus Ethernet Adapter with a long friendly name'),
        findsOneWidget,
      );
      expect(find.text('接口 ID：$secondId'), findsOneWidget);
      expect(find.text('IPv4：192.168.40.12'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await _finish(tester, c, client);
    },
  );

  testWidgets(
    'live invalidation retains explicit draft and disables save without fallback',
    (tester) async {
      final client = _Client(policy: _explicit);
      final c = _controller(client);
      await c.start();
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: NetworkBindingSection(controller: c)),
        ),
      );
      await tester.pump();
      expect(find.text('if-1 · 192.168.1.2'), findsOneWidget);
      expect(find.textContaining('Current Wi-Fi'), findsOneWidget);
      client.owner.stream.add(NetworkChanged(_network(2, up: false)));
      await tester.pump();
      expect(find.textContaining('指定绑定已不可用'), findsOneWidget);
      expect(find.textContaining('不会自动回退'), findsOneWidget);
      final save = tester.widget<FilledButton>(
        find.widgetWithText(FilledButton, '保存网络绑定'),
      );
      expect(save.onPressed, null);
      expect(c.snapshot!.configurations.single.networkBindingPolicy, _explicit);
      await _finish(tester, c, client);
    },
  );

  testWidgets(
    'preview and initial configuration give guidance; changed target requires reopening',
    (tester) async {
      final preview = OfflineDemoClient()..selectScenario('empty');
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => preview,
        pollDelay: const Duration(days: 1),
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ConfigurationPage(controller: controller, onBack: () {}),
          ),
        ),
      );
      await tester.pump();
      expect(find.text('请先保存连接配置，再选择网络绑定。'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      await tester.runAsync(controller.close);
      controller.dispose();
      preview.dispose();
      final client = _Client();
      final c = _controller(client);
      await c.start();
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: NetworkBindingSection(controller: c)),
        ),
      );
      await tester.pump();
      client.configurations = [_configuration(id: 'replacement')];
      client.owner.stream.add(
        SessionRemoved(sessionId: 's', revision: BigInt.one),
      );
      await tester.runAsync(
        () => c.setAutoReconnect(
          configurationId: 'demo-config',
          autoReconnect: true,
        ),
      );
      await tester.pump();
      expect(find.text('连接配置已变化，请重新打开后编辑网络绑定。'), findsOneWidget);
      expect(
        tester
            .widget<FilledButton>(find.widgetWithText(FilledButton, '保存网络绑定'))
            .onPressed,
        null,
      );
      await _finish(tester, c, client);
    },
  );

  test('automatic and explicit saves use typed parameters, no-op does not retire or write', () async {
    final client = _Client();
    final c = _controller(client);
    await c.start();
    expect(
      await c.setNetworkBinding(
        configurationId: 'demo-config',
        policy: const NetworkBindingPolicy.automatic(),
      ),
      true,
    );
    expect(client.requests, isEmpty);
    expect(c.snapshot!.sessions, hasLength(1));
    expect(
      await c.setNetworkBinding(
        configurationId: 'demo-config',
        policy: _explicit,
      ),
      true,
    );
    expect(client.requests.single, ('demo-config', _explicit, false));
    expect(c.snapshot!.sessions, isEmpty);
    expect(
      await c.setNetworkBinding(
        configurationId: 'demo-config',
        policy: const NetworkBindingPolicy.automatic(),
      ),
      true,
    );
    expect(client.requests.last.$2, const NetworkBindingPolicy.automatic());
    expect(client.operations.where((op) => op.startsWith('session.')), isEmpty);
    await c.close();
    c.dispose();
    client.dispose();
  });

  test(
    'unavailable, ineligible, wrong identity and ambiguity refuse saves',
    () async {
      final client = _Client();
      final c = _controller(client);
      await c.start();
      expect(
        await c.setNetworkBinding(configurationId: 'other', policy: _explicit),
        false,
      );
      expect(
        await c.setNetworkBinding(
          configurationId: 'demo-config',
          policy: NetworkBindingPolicy.explicit('if-down', '10.0.0.2'),
        ),
        false,
      );
      client.owner.stream.add(NetworkChanged(_network(2, up: false)));
      expect(
        await c.setNetworkBinding(
          configurationId: 'demo-config',
          policy: _explicit,
        ),
        false,
      );
      client.owner.stream.add(
        NetworkChanged(
          NetworkInterfacesSnapshot(
            available: false,
            revision: BigInt.from(3),
            interfaces: const [],
          ),
        ),
      );
      expect(
        await c.setNetworkBinding(
          configurationId: 'demo-config',
          policy: const NetworkBindingPolicy.automatic(),
        ),
        false,
      );
      client.owner.stream.add(NetworkChanged(_network(4)));
      client.owner.stream.add(
        SessionChanged(_session('unrelated', configurationId: 'other')),
      );
      expect(
        await c.setNetworkBinding(
          configurationId: 'demo-config',
          policy: _explicit,
        ),
        false,
      );
      expect(client.requests, isEmpty);
      await c.close();
      c.dispose();
      client.dispose();
    },
  );

  test(
    'dispatch rechecks network after a serialized diagnosis request',
    () async {
      final client = _Client();
      final c = _controller(client);
      await c.start();
      client.diagnosisGate = Completer<NetworkDiagnosis>();
      c.setDiagnosisVisible(true);
      await pumpEventQueue();
      final saving = c.setNetworkBinding(
        configurationId: 'demo-config',
        policy: _explicit,
      );
      await pumpEventQueue();
      expect(client.requests, isEmpty);
      c.setDiagnosisVisible(false);
      client.owner.stream.add(NetworkChanged(_network(2, up: false)));
      client.diagnosisGate!.complete(_diagnosis);
      expect(await saving, false);
      expect(client.requests, isEmpty);
      await c.close();
      c.dispose();
      client.dispose();
    },
  );

  test('storage confirmation is operation-scoped and eligibility rechecked after consent', () async {
    for (final invalidate in [false, true]) {
      final client = _Client()..requireConsent = true;
      final c = _controller(client);
      await c.start();
      final confirmations = <GuiOperation>[];
      final ok = await c.setNetworkBinding(
        configurationId: 'demo-config',
        policy: _explicit,
        onInsecureStorageConfirmation: (operation) async {
          confirmations.add(operation);
          if (invalidate) {
            client.owner.stream.add(NetworkChanged(_network(2, up: false)));
          }
          return true;
        },
      );
      expect(confirmations, [GuiOperation.updateConfiguration]);
      expect(ok, !invalidate);
      expect(
        client.requests.map((r) => r.$3),
        invalidate ? [false] : [false, true],
      );
      await c.close();
      c.dispose();
      client.dispose();
    }
  });

  test('stable failures remain visible and committed cleanup failure rereads policy', () async {
    for (final code in [
      'network_binding_unavailable',
      'configuration_session_invalidation_failed',
    ]) {
      final client = _Client()..errorCode = code;
      final c = _controller(client);
      await c.start();
      final reads = client.configReads;
      expect(
        await c.setNetworkBinding(
          configurationId: 'demo-config',
          policy: _explicit,
        ),
        false,
      );
      expect(c.networkBindingErrorCode, code);
      expect(c.notice, isNotNull);
      if (code == 'configuration_session_invalidation_failed') {
        expect(client.configReads, greaterThan(reads));
        expect(
          c.snapshot!.configurations.single.networkBindingPolicy,
          _explicit,
        );
        expect(c.sessionNeedsReset, true);
        expect(c.notice, contains('配置已提交'));
      }
      await c.close();
      c.dispose();
      client.dispose();
    }
  });

  test('transport loss disables binding edits, recovered bootstrap uses new network facts', () async {
    final first = _Client();
    final next = _Client(network: _network(1, up: false));
    var connections = 0;
    final c = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => connections++ == 0 ? first : next,
      reconnectDelay: const Duration(days: 1),
    );
    await c.start();
    first.owner.stream.addError(
      const IpcTransportException('ipc_disconnected'),
    );
    await pumpEventQueue();
    expect(c.canSaveNetworkBinding('demo-config', _explicit), false);
    await c.retry();
    expect(c.canSaveNetworkBinding('demo-config', _explicit), false);
    expect(
      c.canSaveNetworkBinding(
        'demo-config',
        const NetworkBindingPolicy.automatic(),
      ),
      true,
    );
    await c.close();
    c.dispose();
    first.dispose();
    next.dispose();
  });
}

Future<void> _pumpUntil(WidgetTester tester, bool Function() condition) async {
  for (var i = 0; i < 40; i++) {
    await tester.pump(const Duration(milliseconds: 5));
    if (condition()) {
      await tester.pump();
      return;
    }
  }
  fail('Bounded UI state wait did not complete');
}

Future<void> _finish(
  WidgetTester tester,
  GuiController c,
  _Client client,
) async {
  await tester.pumpWidget(const SizedBox());
  await tester.runAsync(c.close);
  c.dispose();
  client.dispose();
}

GuiController _controller(_Client client) => GuiController(
  bootstrapper: OfflineDemoBootstrap(),
  connector: (_) async => client,
  pollDelay: const Duration(days: 1),
  reconnectDelay: const Duration(days: 1),
);
final _explicit = NetworkBindingPolicy.explicit('if-1', '192.168.1.2');
const _diagnosis = NetworkDiagnosis(
  observedAt: '2026-10-08T01:02:03Z',
  selectionBasis: 'session_binding',
  status: 'route_unavailable',
  target: NetworkEndpoint('10.0.0.1', 61440),
);
ConfigurationSummary _configuration({
  String id = 'demo-config',
  NetworkBindingPolicy? policy,
}) => ConfigurationSummary(
  id: id,
  displayName: '',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom-5.2.0-d',
  username: 'u',
  credentialStored: true,
  storageProtection: 'protected',
  networkBindingPolicy: policy ?? const NetworkBindingPolicy.automatic(),
);
SessionSummary _session(
  String id, {
  String configurationId = 'demo-config',
  bool cleanup = false,
}) => SessionSummary(
  id: id,
  configurationId: configurationId,
  displayName: '',
  accountName: 'u',
  state: SessionState.suspended,
  intent: SessionIntent.suspendAuthentication,
  revision: BigInt.one,
  cleanupRequired: cleanup,
  selectedNetworkBinding: const SessionNetworkBinding(
    interfaceId: 'current-if',
    displayName: 'Current Wi-Fi',
    localIpv4Address: '10.0.0.2',
  ),
);
NetworkInterfacesSnapshot _network(int revision, {bool up = true}) =>
    NetworkInterfacesSnapshot(
      available: true,
      revision: BigInt.from(revision),
      observedAt: '2026-10-08T01:02:03Z',
      interfaces: [
        for (final (id, name, state, address, bindable) in [
          ('if-1', 'Ethernet', up ? 'up' : 'down', '192.168.1.2', true),
          ('if-down', 'Down', 'down', '10.0.0.2', true),
          ('if-unbindable', 'Virtual', 'up', '192.168.1.3', false),
        ])
          NetworkInterfaceRow(
            interfaceId: id,
            displayName: name,
            operationalState: state,
            physicalMedium: 'wired',
            hardwareBacked: true,
            physicalConnectorPresent: true,
            filterInterface: false,
            endpointInterface: false,
            addressAssignmentMethod: 'dhcp',
            ipv4Assignments: [
              NetworkIPv4Assignment(
                address: address,
                prefixLength: 24,
                automaticCandidate: bindable,
                explicitBindable: bindable,
              ),
            ],
          ),
      ],
    );

class _Owner implements StateSubscription {
  _Owner(NetworkInterfacesSnapshot network)
    : initial = StateBootstrap(sessions: [_session('s')], network: network);
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
  _Client({NetworkBindingPolicy? policy, NetworkInterfacesSnapshot? network})
    : owner = _Owner(network ?? _network(1)),
      configurations = [_configuration(policy: policy)];
  final _Owner owner;
  List<ConfigurationSummary> configurations;
  final requests = <(String, NetworkBindingPolicy, bool)>[];
  String? errorCode;
  bool requireConsent = false;
  int configReads = 0;
  Completer<NetworkDiagnosis>? diagnosisGate;
  @override
  Future<List<ConfigurationSummary>> configurationList() async {
    configReads++;
    return configurations;
  }

  @override
  Future<StateSubscription> subscribeStateEvents() async => owner;
  @override
  Future<ConfigurationSummary> configurationSetNetworkBindingPolicy({
    required String configurationId,
    required NetworkBindingPolicy policy,
    bool allowInsecureStorage = false,
  }) async {
    requests.add((configurationId, policy, allowInsecureStorage));
    if (requireConsent && !allowInsecureStorage) {
      throw const IpcRequestFailure('insecure_storage_confirmation_required');
    }
    final code = errorCode;
    if (code != null && code != 'configuration_session_invalidation_failed') {
      throw IpcRequestFailure(code);
    }
    configurations = [_configuration(id: configurationId, policy: policy)];
    if (code == 'configuration_session_invalidation_failed') {
      owner.stream.add(SessionChanged(_session('s', cleanup: true)));
      throw IpcRequestFailure(code!);
    }
    owner.stream.add(SessionRemoved(sessionId: 's', revision: BigInt.one));
    return configurations.single;
  }

  @override
  Future<NetworkInterfacesSnapshot> networkInterfaces() =>
      throw StateError('GUI must use existing state snapshot');
  @override
  Future<NetworkDiagnosis> networkDiagnose({
    String? configurationId,
    String? sessionId,
    bool probe = false,
  }) => diagnosisGate?.future ?? Future.value(_diagnosis);
}
