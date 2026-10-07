import 'package:flutter/material.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/gui_snapshot.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
  testWidgets('captured edit and header intents cannot navigate from A to B', (
    tester,
  ) async {
    final client = NavigationClient();
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    await controller.start();
    final destinations = <AppPage>[];
    await tester.pumpWidget(
      MaterialApp(
        home: HomePage(controller: controller, onNavigate: destinations.add),
      ),
    );
    await tester.pumpAndSettle();
    final old = tester.widget<HomeView>(find.byType(HomeView)).data;
    old.onSecondary!();
    expect(destinations, [AppPage.configuration]);
    destinations.clear();
    client.configuration = otherConfiguration;
    await controller.retry();
    old.onSecondary!();
    old.onHeader();
    expect(destinations, isEmpty);
    await tester.pumpAndSettle();
    tester.widget<HomeView>(find.byType(HomeView)).data.onSecondary!();
    expect(destinations, [AppPage.configuration]);
    expect(client.operations, isEmpty);
    await tester.pumpWidget(const SizedBox.shrink());
    controller.dispose();
    client.dispose();
  });
  final matrix =
      <
        SessionState,
        (
          String,
          String,
          GuiConnectionActionKind,
          String,
          GuiConnectionActionKind,
          bool,
        )
      >{
        SessionState.authenticated: (
          '已连接',
          '断开连接',
          GuiConnectionActionKind.stop,
          '连接详情',
          GuiConnectionActionKind.showDetails,
          true,
        ),
        SessionState.suspended: (
          '未连接',
          '开始连接',
          GuiConnectionActionKind.start,
          '更改配置',
          GuiConnectionActionKind.editConfiguration,
          true,
        ),
        SessionState.authenticating: (
          '正在认证',
          '取消连接',
          GuiConnectionActionKind.stop,
          '连接详情',
          GuiConnectionActionKind.showDetails,
          true,
        ),
        SessionState.waitingForNetwork: (
          '等待网络',
          '取消等待',
          GuiConnectionActionKind.stop,
          '连接详情',
          GuiConnectionActionKind.showDetails,
          true,
        ),
        SessionState.waitingBeforeRetry: (
          '等待重试',
          '立即重试',
          GuiConnectionActionKind.reconnect,
          '停止重试',
          GuiConnectionActionKind.stop,
          true,
        ),
        SessionState.blockedByError: (
          '认证失败',
          '重新连接',
          GuiConnectionActionKind.reconnect,
          '更改配置',
          GuiConnectionActionKind.editConfiguration,
          true,
        ),
        SessionState.stopping: (
          '正在断开',
          '正在断开',
          GuiConnectionActionKind.stop,
          '连接详情',
          GuiConnectionActionKind.showDetails,
          false,
        ),
      };
  for (final entry in matrix.entries) {
    test('canonical projection ${entry.key.name}', () {
      final p = ConnectionPresentation.project(
        caps(sessions: [session(entry.key)]),
      );
      final (title, label, action, secondary, secondaryAction, enabled) =
          entry.value;
      expect(p.statusTitle, title);
      expect(p.primaryLabel, label);
      expect(p.primaryAction.kind, action);
      expect(p.secondaryLabel, secondary);
      expect(p.secondaryAction.kind, secondaryAction);
      expect(p.primaryEnabled, enabled);
      expect(
        p.primaryAction.targetId,
        action == GuiConnectionActionKind.start
            ? 'demo-config'
            : 'demo-session',
      );
      expect(
        ConnectionPresentation.project(
          caps(sessions: [session(entry.key)], busy: true),
        ).primaryEnabled,
        isFalse,
      );
    });
  }
  test('connection lifecycle and topology stay distinct and fail closed', () {
    for (final state in GuiConnectionState.values) {
      final p = ConnectionPresentation.project(caps(state: state));
      if (state != GuiConnectionState.ready) {
        expect(p.statusTitle, isNot('尚未配置'));
        expect(
          p.primaryEnabled,
          [GuiConnectionState.failed, GuiConnectionState.stale].contains(state),
        );
      }
    }
    final empty = ConnectionPresentation.project(caps(configurations: []));
    expect(empty.primaryAction.kind, GuiConnectionActionKind.addConfiguration);
    expect(empty.primaryEnabled, isTrue);
    for (final pair in <(List<ConfigurationSummary>, List<SessionSummary>)>[
      ([configuration, otherConfiguration], []),
      (
        [configuration],
        [
          session(SessionState.authenticated),
          session(SessionState.suspended, id: 'extra'),
        ],
      ),
      (
        [configuration],
        [session(SessionState.authenticated, configurationId: 'foreign')],
      ),
      (
        [configuration],
        [session(SessionState.authenticated, configurationId: null)],
      ),
      ([], [session(SessionState.authenticated, configurationId: null)]),
    ]) {
      final p = ConnectionPresentation.project(
        caps(configurations: pair.$1, sessions: pair.$2),
      );
      expect(p.statusTitle, isNot('尚未配置'));
      expect(p.statusTitle, isNot('未连接'));
      expect(p.primaryEnabled, isFalse);
    }
  });
  testWidgets('stopping Home binding disables stop and shares Details title', (
    tester,
  ) async {
    final client = OfflineDemoClient()..selectScenario('stopping');
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    await controller.start();
    await tester.pumpWidget(
      MaterialApp(home: SidraviaShell(controller: controller)),
    );
    await tester.pumpAndSettle();
    final view = tester.widget<HomeView>(find.byType(HomeView));
    expect(view.data.state, controller.connectionPresentation.statusTitle);
    expect(view.data.onPrimary, isNull);
    expect(find.text('未连接'), findsNothing);
    expect(await controller.stopSession('demo-session'), isFalse);
    expect(await controller.restartSession('demo-session'), isFalse);
    expect(await controller.startConfiguration('demo-config'), isFalse);
    expect(client.operations.where((op) => op == 'session.stop'), isEmpty);
    await tester.tap(find.text('连接详情'));
    await tester.pumpAndSettle();
    expect(find.text('正在断开'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    controller.dispose();
    client.dispose();
  });
  testWidgets(
    'ambiguous topology visible in Home Settings and raw Diagnostics',
    (tester) async {
      final client = TopologyClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(home: SidraviaShell(controller: controller)),
      );
      await tester.pumpAndSettle();
      expect(find.text('尚未配置'), findsNothing);
      expect(find.text('检测到多个连接配置'), findsOneWidget);
      await tester.tap(find.byTooltip('设置'));
      await tester.pumpAndSettle();
      expect(find.byType(SettingsPage), findsOneWidget);
      expect(find.textContaining('存在多个连接配置'), findsAtLeastNWidgets(1));
      await tester.tap(find.text('诊断'));
      await tester.pumpAndSettle();
      expect(find.byType(AdvancedPage), findsOneWidget);
      expect(find.text('configuration_count'), findsOneWidget);
      expect(find.text('session_count'), findsOneWidget);
      expect(find.text('2'), findsOneWidget);
      expect(find.text('orphan'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );
  test('controller refuses mismatched and ambiguous write targets', () async {
    final client = TopologyClient();
    final c = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    await c.start();
    expect(
      await c.createConfiguration(
        institutionProfileId: 'jlu',
        username: 'u',
        password: 'p',
      ),
      isFalse,
    );
    expect(
      await c.updateConfiguration(
        configurationId: 'demo-config',
        institutionProfileId: 'jlu',
        username: 'u',
      ),
      isFalse,
    );
    expect(
      await c.setPassword(configurationId: 'demo-config', password: 'p'),
      isFalse,
    );
    expect(await c.startConfiguration('demo-config'), isFalse);
    expect(await c.stopSession('orphan'), isFalse);
    expect(await c.restartSession('orphan'), isFalse);
    expect(await c.ensureSessionRunning('orphan'), isFalse);
    expect(client.operations, isEmpty);
    c.dispose();
    client.dispose();
  });
}

GuiCapabilities caps({
  List<ConfigurationSummary> configurations = const [configuration],
  List<SessionSummary> sessions = const [],
  GuiConnectionState state = GuiConnectionState.ready,
  bool busy = false,
}) => GuiCapabilities(
  state: state,
  busy: busy,
  snapshot: GuiSnapshot(
    daemon: const DaemonStatus(
      productVersion: 'v',
      buildId: 'b',
      pid: 1,
      status: 'running',
      mode: 'desktop',
    ),
    profiles: const [OfflineDemoClient.profile],
    configurations: configurations,
    sessions: sessions,
  ),
);
SessionSummary session(
  SessionState state, {
  String id = 'demo-session',
  String? configurationId = 'demo-config',
}) => SessionSummary(
  id: id,
  displayName: '',
  accountName: 'u',
  state: state,
  intent: SessionIntent.maintainAuthentication,
  configurationId: configurationId,
);
const configuration = ConfigurationSummary(
  id: 'demo-config',
  displayName: '',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'd',
  username: 'u',
  credentialStored: true,
  storageProtection: 'protected',
);
const otherConfiguration = ConfigurationSummary(
  id: 'other',
  displayName: '',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'd',
  username: 'other',
  credentialStored: true,
  storageProtection: 'protected',
);

class TopologyClient extends OfflineDemoClient {
  @override
  Future<List<ConfigurationSummary>> configurationList() async => [
    configuration,
    otherConfiguration,
  ];
  @override
  Future<List<SessionSummary>> sessionList() async => [
    session(SessionState.authenticated, id: 'orphan', configurationId: null),
  ];
}

class NavigationClient extends OfflineDemoClient {
  ConfigurationSummary configuration = const ConfigurationSummary(
    id: 'demo-config',
    displayName: '',
    institutionProfileId: 'jlu',
    institutionDisplayName: '吉林大学',
    authenticationProtocolId: 'd',
    username: 'u',
    credentialStored: true,
    storageProtection: 'protected',
  );
  @override
  Future<List<ConfigurationSummary>> configurationList() async => [
    configuration,
  ];
  @override
  Future<List<SessionSummary>> sessionList() async => [];
}
