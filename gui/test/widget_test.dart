import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

void main() {
  testWidgets('wide shell uses horizontal navigation and switches sections', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(_app());
    await tester.pump();

    expect(find.byType(NavigationRail), findsNothing);
    expect(find.byType(NavigationBar), findsNothing);
    for (final destination in appDestinations) {
      expect(find.text(destination.label), findsWidgets);
    }
    final homeNode = tester.getSemantics(
      find.byKey(const ValueKey<String>('destination-home')),
    );
    final configurationNode = tester.getSemantics(
      find.byKey(const ValueKey<String>('destination-configuration')),
    );
    final settingsNode = tester.getSemantics(
      find.byKey(const ValueKey<String>('destination-settings')),
    );
    expect(homeNode.flagsCollection.isSelected, ui.Tristate.isTrue);
    expect(configurationNode.flagsCollection.isSelected, ui.Tristate.isFalse);
    expect(settingsNode.flagsCollection.isSelected, ui.Tristate.isFalse);
    expect(find.text('daemon 已就绪'), findsOneWidget);

    await tester.tap(find.text('配置').first);
    await tester.pumpAndSettle();

    expect(find.text('配置 A'), findsOneWidget);
    expect(
      tester
          .getSemantics(
            find.byKey(const ValueKey<String>('destination-configuration')),
          )
          .flagsCollection
          .isSelected,
      ui.Tristate.isTrue,
    );
  });

  testWidgets('narrow shell uses bottom navigation and switches sections', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(_app());
    await tester.pump();

    expect(find.byType(NavigationBar), findsOneWidget);
    expect(find.byType(NavigationRail), findsNothing);

    await tester.tap(find.text('设置'));
    await tester.pumpAndSettle();

    expect(find.text('启动与路径'), findsOneWidget);
    final app = tester.widget<MaterialApp>(find.byType(MaterialApp));
    expect(app.title, 'Sidravia');
  });

  testWidgets('selection survives a responsive layout change', (tester) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(_app());
    await tester.pump();
    await tester.tap(find.text('配置'));
    await tester.pumpAndSettle();
    await tester.binding.setSurfaceSize(const Size(1024, 720));
    await tester.pumpAndSettle();

    expect(find.text('配置 A'), findsOneWidget);
    expect(find.byType(NavigationBar), findsNothing);
  });

  testWidgets('compact header truncates rather than overflowing', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(220, 360));
    tester.binding.platformDispatcher.textScaleFactorTestValue = 2;
    addTearDown(() {
      tester.binding.platformDispatcher.clearTextScaleFactorTestValue();
      tester.binding.setSurfaceSize(null);
    });

    await tester.pumpWidget(_app());
    await tester.pump();

    expect(tester.takeException(), isNull);
    expect(find.text('Sidravia'), findsOneWidget);
  });

  testWidgets(
    'home copy distinguishes ready authentication, stale, and failure',
    (tester) async {
      final authenticated = GuiController(
        bootstrapper: _Bootstrapper(),
        connector: (_) async => _Client(
          sessions: const [
            SessionSummary(
              id: 'session-a',
              displayName: '校园网络',
              state: 'authenticated',
              intent: 'maintain_authentication',
            ),
          ],
        ),
        pollDelay: const Duration(days: 1),
      );
      await tester.pumpWidget(SidraviaApp(controller: authenticated));
      await tester.pump();
      expect(find.text('会话 校园网络 已认证。'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();

      final failing = GuiController(
        bootstrapper: _FailureBootstrapper(),
        pollDelay: const Duration(days: 1),
      );
      await failing.start();
      await tester.pumpWidget(SidraviaApp(controller: failing));
      await tester.pump();
      expect(find.text('无法接入 daemon'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();

      final stale = GuiController(
        bootstrapper: _SequencedBootstrapper(),
        connector: (_) async => _Client(),
        pollDelay: const Duration(days: 1),
      );
      await stale.start();
      await stale.retry();
      expect(stale.state, GuiConnectionState.stale);
      await tester.pumpWidget(
        MaterialApp(
          home: HomePage(controller: stale, onOpenConfiguration: () {}),
        ),
      );
      expect(find.text('状态暂时过期'), findsOneWidget);
      expect(find.textContaining('保留上一次完整读取的状态'), findsOneWidget);
      stale.dispose();
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    'configuration UI keeps zero, one, and multiple snapshots distinct',
    (tester) async {
      for (final configurations in [
        const <ConfigurationSummary>[],
        const [
          ConfigurationSummary(
            id: 'cfg-a',
            displayName: '配置 A',
            institutionDisplayName: '示例学校',
            username: 'fixture-user',
            credentialStored: true,
            storageProtection: 'protected',
          ),
        ],
        const [
          ConfigurationSummary(
            id: 'cfg-a',
            displayName: '配置 A',
            institutionDisplayName: '示例学校',
            username: 'fixture-user',
            credentialStored: true,
            storageProtection: 'protected',
          ),
          ConfigurationSummary(
            id: 'cfg-b',
            displayName: '配置 B',
            institutionDisplayName: '另一学校',
            username: 'fixture-user-b',
            credentialStored: true,
            storageProtection: 'protected',
          ),
        ],
      ]) {
        final controller = GuiController(
          bootstrapper: _Bootstrapper(),
          connector: (_) async => _Client(configurations: configurations),
          pollDelay: const Duration(days: 1),
        );
        await controller.start();
        await tester.pumpWidget(SidraviaApp(controller: controller));
        await tester.pumpAndSettle();
        await tester.tap(find.text('配置').first);
        await tester.pumpAndSettle();
        switch (configurations.length) {
          case 0:
            expect(find.text('尚无登录配置'), findsOneWidget);
          case 1:
            expect(find.text('配置 A'), findsOneWidget);
            expect(find.textContaining('示例学校'), findsOneWidget);
            expect(find.textContaining('fixture-user'), findsOneWidget);
          default:
            expect(find.text('存在多个登录配置'), findsOneWidget);
            expect(find.textContaining('不会选择或修改'), findsOneWidget);
            expect(find.text('配置 A'), findsNothing);
            expect(find.text('配置 B'), findsNothing);
        }
        await tester.pumpWidget(const SizedBox.shrink());
        await tester.pump();
      }
    },
  );
}

SidraviaApp _app() => SidraviaApp(
  controller: GuiController(
    bootstrapper: _Bootstrapper(),
    connector: (_) async => _Client(),
    pollDelay: const Duration(days: 1),
  ),
);

class _Bootstrapper implements GuiBootstrapper {
  @override
  Future<GuiBootstrapResult> bootstrap() async => GuiBootstrapResult.success(
    GuiBootstrap(
      endpoint: Uri.parse('ws://127.0.0.1:4711/ipc'),
      token: '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
      productVersion: 'fixture',
      buildId: 'fixture',
      daemonPid: 1,
      mode: 'desktop',
    ),
  );
}

class _FailureBootstrapper implements GuiBootstrapper {
  @override
  Future<GuiBootstrapResult> bootstrap() async =>
      const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
}

class _SequencedBootstrapper implements GuiBootstrapper {
  var calls = 0;

  @override
  Future<GuiBootstrapResult> bootstrap() async {
    calls++;
    return calls == 1
        ? _Bootstrapper().bootstrap()
        : const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
  }
}

class _Client implements SidraviaIpcClient {
  _Client({
    this.configurations = const [
      ConfigurationSummary(
        id: 'cfg-a',
        displayName: '配置 A',
        institutionDisplayName: '示例学校',
        username: 'fixture-user',
        credentialStored: true,
        storageProtection: 'protected',
      ),
    ],
    this.sessions = const [],
  });

  final List<ConfigurationSummary> configurations;
  final List<SessionSummary> sessions;

  @override
  Future<DaemonStatus> daemonStatus() async => const DaemonStatus(
    productVersion: 'fixture',
    buildId: 'fixture',
    pid: 1,
    status: 'running',
    mode: 'desktop',
  );

  @override
  Future<List<InstitutionProfile>> profileList() async => const [];

  @override
  Future<List<ConfigurationSummary>> configurationList() async =>
      configurations;

  @override
  Future<List<SessionSummary>> sessionList() async => sessions;

  @override
  Future<void> close() async {}
}
