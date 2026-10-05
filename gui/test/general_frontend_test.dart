import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

const states = {
  'authenticated': ['已连接', '断开连接', '连接详情', 'session.stop'],
  'suspended': ['未连接', '开始连接', '更改配置', 'session.start_configuration'],
  'authenticating': ['正在认证', '取消连接', '连接详情', 'session.stop'],
  'waiting_for_network': ['等待网络', '取消等待', '连接详情', 'session.stop'],
  'waiting_before_retry': ['等待重试', '立即重试', '停止重试', 'session.restart'],
  'blocked_by_error': ['认证失败', '重新连接', '更改配置', 'session.restart'],
  'empty': ['尚未配置', '添加配置', '连接设置', 'configuration'],
};

class FailingClient extends OfflineDemoClient {
  bool fail = true;
  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
  }) async {
    if (fail) throw const IpcRequestFailure('session_state_conflict');
    return super.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
    );
  }
}

void main() {
  testWidgets(
    'Windows formal top edges navigate; Tab Enter and Escape return',
    (tester) async {
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light().copyWith(platform: TargetPlatform.windows),
          home: SidraviaShell(controller: controller),
        ),
      );
      await tester.pumpAndSettle();
      final header = find.byKey(const ValueKey('home-configuration-button'));
      final settings = find.byKey(const ValueKey('home-settings-button'));
      var r = tester.getRect(header);
      expect(r.height, 42);
      expect(tester.getRect(settings).height, 42);
      await tester.tapAt(Offset(r.center.dx, r.top + 1));
      await tester.pumpAndSettle();
      expect(find.text('保存更改'), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);
      r = tester.getRect(header);
      await tester.tapAt(Offset(r.center.dx, r.bottom - 1));
      await tester.pumpAndSettle();
      expect(find.text('保存更改'), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();
      await tester.sendKeyEvent(LogicalKeyboardKey.tab);
      await tester.pump();
      expect(FocusManager.instance.primaryFocus, isNotNull);
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsNothing);
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );

  for (final entry in states.entries) {
    for (final platform in [
      TargetPlatform.windows,
      TargetPlatform.android,
      TargetPlatform.iOS,
    ]) {
      for (final dark in [false, true]) {
        testWidgets(
          'formal HomePage ${entry.key} $platform dark=$dark binds correct action',
          (tester) async {
            final client = OfflineDemoClient()..selectScenario(entry.key);
            final controller = GuiController(
              bootstrapper: OfflineDemoBootstrap(),
              connector: (_) async => client,
            );
            await controller.start();
            await tester.pumpWidget(
              MaterialApp(
                theme: (dark ? AppTheme.dark() : AppTheme.light()).copyWith(
                  platform: platform,
                ),
                home: SidraviaShell(controller: controller),
              ),
            );
            await tester.pumpAndSettle();
            expect(find.byType(HomePage), findsOneWidget);
            expect(find.text(entry.value[0]), findsAtLeastNWidgets(1));
            expect(find.text(entry.value[2]), findsOneWidget);
            expect(find.textContaining('30 秒'), findsNothing);
            if (entry.key == 'blocked_by_error') {
              expect(find.text('credential_invalid'), findsOneWidget);
            }
            await tester.tap(find.text(entry.value[1]));
            await tester.pumpAndSettle();
            if (entry.key == 'empty') {
              expect(find.text('保存配置'), findsOneWidget);
            } else {
              expect(client.operations.last, entry.value[3]);
            }
            await tester.pumpWidget(const SizedBox.shrink());
            controller.dispose();
            client.dispose();
          },
        );
      }
    }
  }
  testWidgets(
    'retry secondary stops the actual session; failure secondary opens configuration',
    (tester) async {
      final client = OfflineDemoClient()
        ..selectScenario('waiting_before_retry');
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(home: SidraviaShell(controller: controller)),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('停止重试'));
      await tester.pumpAndSettle();
      expect(client.operations.last, 'session.stop');
      client.selectScenario('blocked_by_error');
      await controller.start();
      await tester.pumpAndSettle();
      await tester.tap(find.text('更改配置'));
      await tester.pumpAndSettle();
      expect(find.text('保存更改'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );
  testWidgets('production App theme switch affects pages and follows system', (
    tester,
  ) async {
    final client = OfflineDemoClient();
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
    );
    await tester.pumpWidget(SidraviaApp(controller: controller));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('设置'));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(DropdownButton<ThemeMode>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('深色').last);
    await tester.pumpAndSettle();
    expect(
      tester.widget<MaterialApp>(find.byType(MaterialApp)).themeMode,
      ThemeMode.dark,
    );
    expect(
      Theme.of(tester.element(find.text('连接配置'))).brightness,
      Brightness.dark,
    );
    await tester.tap(find.byType(DropdownButton<ThemeMode>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('浅色').last);
    await tester.pumpAndSettle();
    expect(
      Theme.of(tester.element(find.text('连接配置'))).brightness,
      Brightness.light,
    );
    await tester.tap(find.byType(DropdownButton<ThemeMode>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('跟随系统').last);
    await tester.pumpAndSettle();
    tester.platformDispatcher.platformBrightnessTestValue = Brightness.dark;
    await tester.pumpAndSettle();
    expect(
      Theme.of(tester.element(find.text('连接配置'))).brightness,
      Brightness.dark,
    );
    tester.platformDispatcher.clearPlatformBrightnessTestValue();
    await tester.pumpWidget(const SizedBox.shrink());
    client.dispose();
  });
  testWidgets(
    'form validates, preserves failure, clears password and saves actual client',
    (tester) async {
      final client = FailingClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(home: SidraviaShell(controller: controller)),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('吉林大学'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '');
      await tester.tap(find.text('保存更改'));
      await tester.pumpAndSettle();
      expect(find.text('请输入校园网账号。'), findsOneWidget);
      await tester.enterText(find.byType(TextField).first, 'demo-new');
      await tester.enterText(find.byType(TextField).last, 'fake-only');
      await tester.tap(find.text('保存更改'));
      await tester.pumpAndSettle();
      expect(find.text('当前状态无法执行此操作。'), findsOneWidget);
      expect(
        tester.widget<TextField>(find.byType(TextField).last).controller!.text,
        isEmpty,
      );
      client.fail = false;
      await tester.tap(find.text('保存更改'));
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);
      expect(find.text('demo-new'), findsOneWidget);
      expect(controller.notice, isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );
  for (final platform in [
    TargetPlatform.windows,
    TargetPlatform.android,
    TargetPlatform.iOS,
  ]) {
    for (final dark in [false, true]) {
      testWidgets(
        'pages scroll with text scale keyboard safearea $platform dark=$dark',
        (tester) async {
          await tester.binding.setSurfaceSize(const Size(360, 640));
          addTearDown(() => tester.binding.setSurfaceSize(null));
          final client = OfflineDemoClient();
          final controller = GuiController(
            bootstrapper: OfflineDemoBootstrap(),
            connector: (_) async => client,
          );
          await controller.start();
          await tester.pumpWidget(
            MaterialApp(
              theme: (dark ? AppTheme.dark() : AppTheme.light()).copyWith(
                platform: platform,
              ),
              home: MediaQuery(
                data: const MediaQueryData(
                  textScaler: TextScaler.linear(1.6),
                  padding: EdgeInsets.only(top: 24, bottom: 20),
                  viewInsets: EdgeInsets.only(bottom: 180),
                ),
                child: SidraviaShell(controller: controller),
              ),
            ),
          );
          await tester.pumpAndSettle();
          await tester.tap(find.byTooltip('设置'));
          await tester.pumpAndSettle();
          await tester.ensureVisible(find.text('技术诊断'));
          await tester.tap(find.text('技术诊断'));
          await tester.pumpAndSettle();
          expect(find.text('daemon'), findsOneWidget);
          await tester.ensureVisible(find.text('返回'));
          await tester.tap(find.text('返回'));
          await tester.pumpAndSettle();
          await tester.ensureVisible(find.text('连接配置'));
          await tester.tap(find.text('连接配置'));
          await tester.pumpAndSettle();
          await tester.ensureVisible(find.text('保存更改'));
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox.shrink());
          controller.dispose();
          client.dispose();
        },
      );
    }
  }
  testWidgets('announcement bounded dialog marks read and Escape closes', (
    tester,
  ) async {
    final now = DateTime.utc(2026, 10, 5);
    final client = OfflineDemoClient();
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
    );
    final announcements = AnnouncementController(
      endpoint: OfflineAnnouncementFetcher.endpoint,
      store: MemoryAnnouncementStore(),
      fetcher: OfflineAnnouncementFetcher(now),
      nowUtc: () => now,
    );
    await controller.start();
    await announcements.start();
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light().copyWith(platform: TargetPlatform.windows),
        home: SidraviaShell(
          controller: controller,
          announcements: announcements,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byType(HomeNoticeView));
    await tester.pumpAndSettle();
    expect(find.byType(Dialog), findsOneWidget);
    expect(announcements.unreadCount, 0);
    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pumpAndSettle();
    expect(find.byType(Dialog), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
    controller.dispose();
    announcements.dispose();
    client.dispose();
  });
}
