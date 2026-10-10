import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/gui_operation.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/configuration/network_binding_section.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/state_models.dart';
import 'package:sidravia_gui/ipc/state_subscription.dart';

void main() {
  testWidgets(
    'controlled binding preserves unavailable selection and filters choices',
    (tester) async {
      final client = _Preview();
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await c.start();
      var draft = _explicit;
      try {
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: StatefulBuilder(
                builder: (context, setState) => NetworkBindingSection(
                  controller: c,
                  policy: draft,
                  onChanged: (value) => setState(() => draft = value),
                ),
              ),
            ),
          ),
        );
        await tester.tap(find.text('下次连接的网络策略'));
        await tester.pumpAndSettle();
        expect(find.text('192.168.1.2/24 · Ethernet · if-1'), findsNWidgets(2));
        expect(find.textContaining('if-down'), findsNothing);
        expect(find.textContaining('192.168.1.3'), findsNothing);
        await tester.tap(find.text('自动选择可用网卡'));
        await tester.pump();
        expect(draft, const NetworkBindingPolicy.automatic());
        expect(client.operations, isNot(contains('configuration.update')));
        draft = _explicit;
        client.up = false;
        await c.setAutoReconnect(
          configurationId: 'demo-config',
          autoReconnect: true,
        );
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: NetworkBindingSection(
                controller: c,
                policy: draft,
                onChanged: (_) {},
              ),
            ),
          ),
        );
        expect(find.textContaining('不会自动回退'), findsOneWidget);
        expect(find.text('接口 ID：if-1'), findsOneWidget);
        expect(draft, _explicit);
      } finally {
        await tester.pumpWidget(const SizedBox());
        var closed = false;
        final closing = c.close().then((_) => closed = true);
        for (var i = 0; i < 20 && !closed; i++) {
          await tester.pump();
        }
        await closing;
        c.dispose();
        client.dispose();
      }
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

class _Preview extends OfflineDemoClient {
  bool up = true;
  @override
  Future<NetworkInterfacesSnapshot> networkInterfaces() async =>
      _network(up ? 1 : 2, up: up);
}
