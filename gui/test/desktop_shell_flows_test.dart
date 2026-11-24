import 'support/configuration_editing.dart';
import 'support/runtime_snapshot.dart';
import 'support/announcement_shell_fixture.dart' as school_dispatch;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

class _FailingClient extends OfflineDemoClient {
  bool fail = true;
  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    NetworkBindingPolicy? networkBindingPolicy,
    bool allowInsecureStorage = false,
  }) async {
    if (fail) throw const IpcRequestFailure('session_state_conflict');
    return super.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
      networkBindingPolicy: networkBindingPolicy,
      allowInsecureStorage: allowInsecureStorage,
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
      expect(find.text('保存修改'), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);
      r = tester.getRect(header);
      await tester.tapAt(Offset(r.center.dx, r.bottom - 1));
      await tester.pumpAndSettle();
      expect(find.text('保存修改'), findsOneWidget);
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
    await tester.ensureVisible(
      find.byKey(const ValueKey('appearance-selector')),
    );
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('appearance-selector')));
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
    await tester.ensureVisible(
      find.byKey(const ValueKey('appearance-selector')),
    );
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('appearance-selector')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('浅色').last);
    await tester.pumpAndSettle();
    expect(
      Theme.of(tester.element(find.text('连接配置'))).brightness,
      Brightness.light,
    );
    await tester.ensureVisible(
      find.byKey(const ValueKey('appearance-selector')),
    );
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('appearance-selector')));
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
  testWidgets('form validates, retains failure draft and saves actual client', (
    tester,
  ) async {
    final client = _FailingClient();
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
    await tester.ensureVisible(find.text('保存修改'));
    await tester.pump();
    await tester.tap(find.text('保存修改'));
    await tester.pumpAndSettle();
    if (await tapConfigurationSaveConfirmation(tester)) {
      await tester.pump();
    }
    await tester.pumpAndSettle();
    expect(find.text('请输入账号'), findsOneWidget);
    await tester.enterText(find.byType(TextField).first, 'demo-new');
    await openPasswordEditor(tester);
    await tester.enterText(find.byType(TextField).last, 'fake-only');
    await tester.ensureVisible(find.text('保存修改'));
    await tester.pump();
    await tester.tap(find.text('保存修改'));
    await tester.pumpAndSettle();
    if (await tapConfigurationSaveConfirmation(tester)) {
      await tester.pump();
    }
    await tester.pumpAndSettle();
    expect(find.text('当前状态无法执行此操作'), findsOneWidget);
    expect(
      tester.widget<TextField>(find.byType(TextField).last).controller!.text,
      'fake-only',
    );
    client.fail = false;
    await tester.ensureVisible(find.text('保存修改'));
    await tester.pump();
    await tester.tap(find.text('保存修改'));
    await tester.pumpAndSettle();
    if (await tapConfigurationSaveConfirmation(tester)) {
      await tester.pump();
    }
    await tester.pumpAndSettle();
    expect(find.byType(HomePage), findsOneWidget);
    expect(find.text('demo-new'), findsOneWidget);
    expect(controller.notice, isNull);
    await tester.pumpWidget(const SizedBox.shrink());
    controller.dispose();
    client.dispose();
  });
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
          await tester.ensureVisible(find.text('连接与诊断'));
          await tester.pump();
          await tester.tap(find.text('连接与诊断'));
          await tester.pumpAndSettle();
          expect(find.text('核心服务状态'), findsNothing);
          expect(displayedSnapshot(tester)['backend'], isNotNull);
          await tester.tap(find.byTooltip('刷新快照'));
          await tester.pump();
          expect(displayedSnapshot(tester)['backend'], isNotNull);
          await tester.ensureVisible(find.byTooltip('返回'));
          await tester.pump();
          await tester.tap(find.byTooltip('返回'));
          await tester.pumpAndSettle();
          await tester.ensureVisible(find.text('连接配置'));
          await tester.pump();
          await tester.tap(find.text('连接配置'));
          await tester.pumpAndSettle();
          await tester.ensureVisible(find.text('保存修改'));
          await tester.pump();
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox.shrink());
          controller.dispose();
          client.dispose();
        },
      );
    }
  }
  testWidgets(
    'school entry invokes in-app adapter and preserves shell Escape behavior',
    (tester) async {
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      final announcements = await school_dispatch.manualAnnouncements(
        tester,
        controller,
      );
      await announcements.start();
      try {
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
        await tester.tap(find.byType(HomeAnnouncementEntryView));
        await tester.pumpAndSettle();
        expect(find.byType(Dialog), findsNothing);
        expect(find.text('当前环境暂不支持应用内公告'), findsOneWidget);
        await tester.sendKeyEvent(LogicalKeyboardKey.escape);
        await tester.pumpAndSettle();
        expect(find.byType(Dialog), findsNothing);
      } finally {
        await tester.pump();
        await school_dispatch.pumpCleanup(tester, () async {
          await announcements.close();
          await controller.close();
        });
        await tester.pumpWidget(const SizedBox.shrink());
        controller.dispose();
        announcements.dispose();
        client.dispose();
      }
    },
  );
}
