import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/shared/widgets/connection_issue_card.dart';

Future<void> tap(WidgetTester tester, String text) async {
  await tester.ensureVisible(find.text(text));
  await tester.tap(find.text(text));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets(
    'one page routes from home/settings and restores technical preference',
    (tester) async {
      final store = MemoryGuiSettingsStore();
      final appearance = Appearance(store: store);
      await appearance.initialize();
      final client = _Client(SessionState.authenticated);
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await c.start();
      try {
        await tester.pumpWidget(
          AppearanceScope(
            appearance: appearance,
            child: MaterialApp(home: SidraviaShell(controller: c)),
          ),
        );
        await tester.pumpAndSettle();
        await tap(tester, '连接与诊断');
        expect(find.byType(AdvancedPage), findsOneWidget);
        expect(find.text('核心服务状态'), findsNothing);
        expect(find.text('10.0.0.1:61440'), findsOneWidget);
        expect(find.text('198.51.100.5:61440'), findsNothing);
        expect(client.probes, [false]);
        await tap(tester, '技术信息');
        await appearance.settled;
        expect(appearance.value, ThemeMode.system);
        expect(store.settings!.diagnosticsExpanded, isTrue);
        expect(client.probes, [false]);
        expect(find.text('198.51.100.5:61440'), findsOneWidget);
        await tap(tester, '执行有限 IP 探测');
        expect(client.probes.last, isTrue);
        await tap(tester, '返回');
        expect(find.byType(HomePage), findsOneWidget);
        await tester.tap(find.byTooltip('设置'));
        await tester.pumpAndSettle();
        await tap(tester, '连接与诊断');
        expect(find.text('核心服务状态'), findsOneWidget);
        await tap(tester, '返回');
        expect(find.byType(SettingsPage), findsOneWidget);
        final restored = Appearance(store: store);
        await restored.initialize();
        expect(restored.diagnosticsExpanded, isTrue);
        restored.dispose();
      } finally {
        await tester.pumpWidget(const SizedBox());
        await c.close();
        c.dispose();
        client.dispose();
        await appearance.settled;
        appearance.dispose();
      }
    },
  );
  for (final state in [
    SessionState.blockedByError,
    SessionState.waitingBeforeRetry,
    SessionState.authenticated,
    SessionState.waitingForNetwork,
  ]) {
    testWidgets('fault card projects real error only in $state', (
      tester,
    ) async {
      final client = _Client(state);
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await c.start();
      try {
        await tester.pumpWidget(
          MaterialApp(home: SidraviaShell(controller: c)),
        );
        await tester.pumpAndSettle();
        final fault =
            state == SessionState.blockedByError ||
            state == SessionState.waitingBeforeRetry;
        expect(
          find.byType(ConnectionIssueCard),
          fault ? findsOneWidget : findsNothing,
        );
        if (fault) {
          expect(find.text('backend_code'), findsOneWidget);
          expect(find.text('后端实际描述'), findsOneWidget);
        }
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: AdvancedPage(controller: c, onBack: () {}),
            ),
          ),
        );
        await tester.pumpAndSettle();
        expect(
          find.byType(ConnectionIssueCard),
          fault ? findsOneWidget : findsNothing,
        );
        if (fault) {
          expect(find.text('backend_code'), findsOneWidget);
          expect(find.text('后端实际描述'), findsOneWidget);
        }
      } finally {
        await tester.pumpWidget(const SizedBox());
        await c.close();
        c.dispose();
        client.dispose();
      }
    });
  }
  for (final width in [360.0, 400.0]) {
    testWidgets('advanced reset and scaled scrolling at $width', (
      tester,
    ) async {
      await tester.binding.setSurfaceSize(
        Size(width, width == 360 ? 640 : 690),
      );
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final client = _Client(SessionState.blockedByError);
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await c.start();
      Widget mount(Widget page) => MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(context)
              .copyWith(textScaler: const TextScaler.linear(2)),
          child: child!,
        ),
        home: Scaffold(body: page),
      );
      try {
        await tester.pumpWidget(
          mount(ConfigurationPage(controller: c, onBack: () {})),
        );
        await tester.pumpAndSettle();
        expect(find.text('移除当前会话'), findsNothing);
        await tester.pumpWidget(
          mount(AdvancedPage(controller: c, onBack: () {})),
        );
        await tester.pumpAndSettle();
        await tap(tester, '技术信息');
        await tap(tester, '移除当前会话');
        await tap(tester, '取消');
        expect(client.removals, isEmpty);
        await tap(tester, '移除当前会话');
        await tap(tester, '确认');
        expect(client.removals, ['captured-session']);
        expect(find.text('当前状态无法执行此操作'), findsOneWidget);
        expect(tester.takeException(), isNull);
      } finally {
        await tester.pumpWidget(const SizedBox());
        await c.close();
        c.dispose();
        client.dispose();
      }
    });
  }
}

class _Client extends OfflineDemoClient {
  _Client(this.state);
  final SessionState state;
  final probes = <bool>[];
  final removals = <String>[];
  @override
  Future<List<SessionSummary>> sessionList() async => [
    SessionSummary(
      id: 'captured-session',
      configurationId: 'demo-config',
      displayName: '',
      accountName: 'student01',
      state: state,
      intent: SessionIntent.maintainAuthentication,
      cleanupRequired: true,
      lastAuthenticationFailure: const SessionAuthenticationFailure(
        code: 'backend_code',
        description: '后端实际描述',
        handlingRecommendation: '后端实际建议',
      ),
      protocolSocket: NetworkProtocolSocket(
        state: 'open',
        runGeneration: BigInt.one,
        remoteEndpoint: const NetworkEndpoint('10.0.0.1', 61440),
      ),
      selectedNetworkBinding: const SessionNetworkBinding(
        interfaceId: 'demo-iface',
        displayName: '实际网卡',
        localIpv4Address: '192.0.2.4',
      ),
    ),
  ];
  @override
  Future<NetworkDiagnosis> networkDiagnose({
    String? configurationId,
    String? sessionId,
    bool probe = false,
  }) async {
    probes.add(probe);
    return const NetworkDiagnosis(
      observedAt: '2026-10-10T00:00:00Z',
      selectionBasis: 'os_route_proposal',
      status: 'available',
      target: NetworkEndpoint('198.51.100.5', 61440),
    );
  }

  @override
  Future<SessionRemoveResult> sessionRemove(String sessionId) async {
    removals.add(sessionId);
    throw const IpcRequestFailure('session_state_conflict');
  }
}
