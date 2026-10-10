import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

Future<void> open(WidgetTester tester, GuiController c) async {
  await tester.pumpWidget(MaterialApp(home: SidraviaShell(controller: c)));
  await tester.pumpAndSettle();
  await tester.tap(find.byKey(const ValueKey('home-configuration-button')));
  await tester.pumpAndSettle();
}

void main() {
  for (final scenario in [
    'authenticated',
    'authenticating',
    'waiting_for_network',
    'waiting_before_retry',
    'suspended',
  ]) {
    testWidgets('save choices and atomic order for $scenario', (tester) async {
      final client = OfflineDemoClient()..selectScenario(scenario);
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await c.start();
      try {
        await open(tester, c);
        await tester.tap(find.text('保存更改'));
        await tester.pump();
        expect(find.byType(AlertDialog), findsNothing);
        expect(client.operations, isNot(contains('configuration.update')));
        expect(find.text('更改密码'), findsOneWidget);
        await tester.tap(find.text('更改密码'));
        await tester.pump();
        await tester.enterText(find.byType(TextField).last, 'temporary');
        await tester.tap(find.text('取消更改'));
        await tester.pump();
        expect(find.byType(TextField), findsOneWidget);
        await tester.enterText(find.byType(TextField).first, 'changed');
        await tester.tap(find.text('保存更改'));
        await tester.pumpAndSettle();
        final active = scenario != 'suspended';
        expect(
          find.text(
            scenario == 'authenticated'
                ? '保存并断开'
                : active
                ? '保存并停止'
                : '保存',
          ),
          findsOneWidget,
        );
        expect(
          find.widgetWithText(FilledButton, active ? '保存并重新连接' : '保存并连接'),
          findsOneWidget,
        );
        await tester.tap(find.text('取消').last);
        await tester.pumpAndSettle();
        expect(client.operations, isNot(contains('configuration.update')));
        expect(
          tester.widget<TextField>(find.byType(TextField)).controller!.text,
          'changed',
        );
        await tester.tap(find.text('保存更改'));
        await tester.pumpAndSettle();
        await tester.tap(find.text(active ? '保存并重新连接' : '保存并连接'));
        await tester.pumpAndSettle();
        expect(find.byType(HomePage), findsOneWidget);
        expect(
          client.operations.where((v) => v == 'configuration.update'),
          hasLength(1),
        );
        expect(
          client.operations.indexOf('configuration.update'),
          lessThan(client.operations.indexOf('session.start_configuration')),
        );
      } finally {
        await tester.pumpWidget(const SizedBox());
        await c.close();
        c.dispose();
        client.dispose();
      }
    });
  }
  testWidgets('first draft selects binding before atomic create, save only', (
    tester,
  ) async {
    final client = OfflineDemoClient()..selectScenario('empty');
    final c = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    await c.start();
    try {
      await open(tester, c);
      await tester.enterText(find.byType(TextField).first, 'first');
      await tester.enterText(find.byType(TextField).last, 'secret');
      await tester.ensureVisible(find.text('下次连接的网络策略'));
      await tester.tap(find.text('下次连接的网络策略'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('192.0.2.4/24 · 模拟以太网 · demo-iface'));
      await tester.pumpAndSettle();
      expect(c.snapshot!.configurations, isEmpty);
      await tester.ensureVisible(find.text('保存配置'));
      await tester.tap(find.text('保存配置'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('保存'));
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);
      expect(
        c.snapshot!.configurations.single.networkBindingPolicy,
        NetworkBindingPolicy.explicit('demo-iface', '192.0.2.4'),
      );
      expect(
        client.operations.where((v) => v == 'configuration.create'),
        hasLength(1),
      );
      expect(client.operations, isNot(contains('session.start_configuration')));
    } finally {
      await tester.pumpWidget(const SizedBox());
      await c.close();
      c.dispose();
      client.dispose();
    }
  });
  testWidgets('focused keyboard and header share dirty leave prompt', (
    tester,
  ) async {
    final client = OfflineDemoClient();
    final c = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    await c.start();
    try {
      await open(tester, c);
      await tester.enterText(find.byType(TextField), 'draft');
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();
      expect(find.widgetWithText(FilledButton, '继续编辑'), findsOneWidget);
      await tester.tap(find.text('继续编辑'));
      await tester.pumpAndSettle();
      expect(find.byType(ConfigurationPage), findsOneWidget);
      await tester.tap(find.text('返回'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('放弃修改'));
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);
      expect(client.operations, isNot(contains('configuration.update')));
    } finally {
      await tester.pumpWidget(const SizedBox());
      await c.close();
      c.dispose();
      client.dispose();
    }
  });
  for (final mode in ['cleanup', 'unknown', 'refresh', 'connection']) {
    testWidgets('visible $mode outcome retains draft or returns home', (
      tester,
    ) async {
      final client = _FaultClient(mode);
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
        reconnectDelay: const Duration(days: 1),
      );
      await c.start();
      try {
        await open(tester, c);
        await tester.enterText(find.byType(TextField), 'retained-draft');
        await tester.tap(find.text('更改密码'));
        await tester.pump();
        await tester.enterText(
          find.byType(TextField).last,
          'retained-password',
        );
        await tester.tap(find.text('保存更改'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('保存并重新连接'));
        await tester.pumpAndSettle();
        expect(client.updates, 1);
        if (mode == 'connection') {
          expect(find.byType(HomePage), findsOneWidget);
          expect(client.starts, 1);
          expect(c.notice, isNotNull);
        } else {
          expect(find.byType(ConfigurationPage), findsOneWidget);
          expect(client.starts, 0);
          expect(
            tester
                .widget<TextField>(find.byType(TextField).first)
                .controller!
                .text,
            'retained-draft',
          );
          expect(
            tester
                .widget<TextField>(find.byType(TextField).last)
                .controller!
                .text,
            'retained-password',
          );
          final message = mode == 'cleanup'
              ? '配置已提交，但旧会话清理失败；请清理会话后再连接。'
              : mode == 'unknown'
              ? '保存结果未知，请恢复通信后确认配置；未自动连接。'
              : '配置已保存，但刷新失败；请恢复通信后确认，未自动连接。';
          await tester.ensureVisible(find.text(message));
          expect(find.text(message), findsOneWidget);
          if (mode != 'cleanup') {
            expect(
              tester.widget<TextField>(find.byType(TextField).first).enabled,
              isFalse,
            );
          }
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
    testWidgets(
      'missing credential and long unavailable binding at $width scale 2',
      (tester) async {
        await tester.binding.setSurfaceSize(Size(width, 800));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        final client = _FaultClient('missing');
        final c = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client,
          pollDelay: const Duration(days: 1),
        );
        await c.start();
        try {
          await tester.pumpWidget(
            MaterialApp(
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(context)
                    .copyWith(textScaler: const TextScaler.linear(2)),
                child: child!,
              ),
              home: Scaffold(
                body: ConfigurationPage(controller: c, onBack: () {}),
              ),
            ),
          );
          await tester.pumpAndSettle();
          expect(find.textContaining('密码已保存'), findsNothing);
          expect(find.byType(TextField), findsNWidgets(2));
          await tester.ensureVisible(find.text('下次连接的网络策略'));
          await tester.tap(find.text('下次连接的网络策略'));
          await tester.pumpAndSettle();
          expect(find.textContaining('指定绑定已不可用'), findsOneWidget);
          expect(
            c.snapshot!.configurations.single.networkBindingPolicy.interfaceId,
            _longId,
          );
          await tester.ensureVisible(find.text('保存更改'));
          await tester.pumpAndSettle();
          expect(tester.takeException(), isNull);
        } finally {
          await tester.pumpWidget(const SizedBox());
          await c.close();
          c.dispose();
          client.dispose();
        }
      },
    );
  }
}

const _longId =
    'unavailable-interface-with-a-very-long-stable-identity-abcdefghijklmnopqrstuvwxyz';

class _FaultClient extends OfflineDemoClient {
  _FaultClient(this.mode);
  final String mode;
  bool written = false;
  int updates = 0, starts = 0;
  @override
  Future<List<ConfigurationSummary>> configurationList() async {
    final rows = await super.configurationList();
    if (mode != 'missing') return rows;
    final c = rows.single;
    return [
      ConfigurationSummary(
        id: c.id,
        displayName: c.displayName,
        institutionProfileId: c.institutionProfileId,
        institutionDisplayName: c.institutionDisplayName,
        authenticationProtocolId: c.authenticationProtocolId,
        username: c.username,
        credentialStored: false,
        storageProtection: c.storageProtection,
        networkBindingPolicy: NetworkBindingPolicy.explicit(
          _longId,
          '192.0.2.99',
        ),
      ),
    ];
  }

  @override
  Future<DaemonStatus> daemonStatus() async {
    if (written && mode == 'refresh') {
      throw const IpcTransportException('ipc_disconnected');
    }
    return super.daemonStatus();
  }

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    NetworkBindingPolicy? networkBindingPolicy,
    bool allowInsecureStorage = false,
  }) async {
    updates++;
    final result = await super.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
      networkBindingPolicy: networkBindingPolicy,
      allowInsecureStorage: allowInsecureStorage,
    );
    written = true;
    if (mode == 'cleanup') {
      throw const IpcRequestFailure(
        'configuration_session_invalidation_failed',
      );
    }
    if (mode == 'unknown') {
      throw const IpcTransportException('ipc_disconnected');
    }
    return result;
  }

  @override
  Future<SessionSummary> sessionStartConfiguration(
    String configurationId,
  ) async {
    starts++;
    if (mode == 'connection') throw const IpcRequestFailure('invalid_state');
    return super.sessionStartConfiguration(configurationId);
  }
}
