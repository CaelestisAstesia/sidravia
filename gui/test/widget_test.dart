import 'dart:async';

import 'school_announcement_controller_test.dart' as school_dispatch;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/desktop/tray_popover.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';

void main() {
  testWidgets('HTML MVP uses a single surface without the legacy sidebar', (
    tester,
  ) async {
    final controller = GuiController(
      bootstrapper: const UnsupportedGuiBootstrapper(),
    );
    try {
      await tester.pumpWidget(
        MaterialApp(home: SidraviaShell(controller: controller)),
      );
      expect(find.text('正在连接服务'), findsOneWidget);
      expect(find.text('尚未配置'), findsNothing);
      expect(find.byTooltip('设置'), findsOneWidget);
      expect(find.byKey(const ValueKey<String>('wide-sidebar')), findsNothing);
    } finally {
      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
    }
  });

  testWidgets('tray navigation uses the existing destination stack', (
    tester,
  ) async {
    final controller = GuiController(
      bootstrapper: const UnsupportedGuiBootstrapper(),
    );
    final shellKey = GlobalKey<SidraviaShellState>();
    try {
      await tester.pumpWidget(
        MaterialApp(
          home: SidraviaShell(key: shellKey, controller: controller),
        ),
      );
      expect(
        await shellKey.currentState!.navigateFromTray(AppPage.settings),
        isTrue,
      );
      await tester.pumpAndSettle();
      expect(find.byType(SettingsPage), findsOneWidget);
      expect(
        await shellKey.currentState!.navigateFromTray(AppPage.home),
        isTrue,
      );
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);
    } finally {
      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
    }
  });

  testWidgets('shell forwards log opener to diagnostics', (tester) async {
    final controller = GuiController(
      bootstrapper: const UnsupportedGuiBootstrapper(),
    );
    final key = GlobalKey<SidraviaShellState>();
    var calls = 0;
    try {
      await tester.pumpWidget(
        MaterialApp(
          home: SidraviaShell(
            key: key,
            controller: controller,
            onOpenLogDirectory: () async {
              calls++;
              return true;
            },
          ),
        ),
      );
      await key.currentState!.navigateFromTray(AppPage.diagnostics);
      await tester.pumpAndSettle();
      await tester.tap(find.text('技术信息'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('打开日志目录'));
      await tester.tap(find.text('打开日志目录'));
      await tester.pumpAndSettle();
      expect(calls, 1);
    } finally {
      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
    }
  });

  testWidgets('tray create is revalidated after restoring the main window', (
    tester,
  ) async {
    final restore = Completer<void>();
    const channel = MethodChannel(desktopPresenceChannelName);
    Future<Object?> nativeCallHandler(MethodCall call) async {
      if (call.method == 'initialize') return 'primary';
      if (call.method == 'getVisibility') return true;
      if (call.method == 'restoreMain') {
        await restore.future;
      }
      return null;
    }

    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, nativeCallHandler);
    final client = OfflineDemoClient()..selectScenario('empty');
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
    );
    final desktop = DesktopPresence();
    await desktop.initialize();
    try {
      await tester.pumpWidget(
        SidraviaApp(controller: controller, desktopPresence: desktop),
      );
      await tester.pumpAndSettle();
      expect(find.text('尚未配置'), findsAtLeastNWidgets(1));

      final pending = desktop.onTrayAction!(
        const TrayIntent(TrayIntentKind.addConfiguration),
      );
      await tester.pump();
      expect(
        await controller.createConfiguration(
          institutionProfileId: 'jlu',
          username: 'demo',
          password: 'secret',
        ),
        isTrue,
      );
      restore.complete();
      await pending;
      await tester.pumpAndSettle();
      expect(find.byType(ConfigurationPage), findsNothing);
      expect(find.byType(HomePage), findsOneWidget);
    } finally {
      if (!restore.isCompleted) restore.complete();
      await tester.pumpWidget(const SizedBox.shrink());
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, null);
      client.dispose();
    }
  });

  for (final size in const [
    Size(360, 640),
    Size(400, 690),
    Size(900, 720),
    Size(1440, 900),
  ]) {
    testWidgets('page navigation and home announcements at $size', (
      tester,
    ) async {
      final client = OfflineDemoClient(now: DateTime.utc(2026, 10, 10));
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      final announcements = await school_dispatch.manualAnnouncements(
        tester,
        controller,
      );
      try {
        await announcements.start();
        await tester.binding.setSurfaceSize(size);
        await tester.pumpWidget(
          MaterialApp(
            theme: ThemeData(platform: TargetPlatform.windows),
            home: SidraviaShell(
              controller: controller,
              announcements: announcements,
            ),
          ),
        );
        await tester.pumpAndSettle();

        Future<void> tapVisible(Finder target) async {
          await tester.ensureVisible(target);
          await tester.tap(target);
          await tester.pumpAndSettle();
          expect(tester.takeException(), isNull);
          expect(find.byKey(const ValueKey('desktop-header')), findsNothing);
          expect(
            find.byKey(const ValueKey('desktop-announcement-entry')),
            findsNothing,
          );
        }

        expect(find.byType(HomePage), findsOneWidget);
        expect(tester.takeException(), isNull);
        await tapVisible(find.text('查看吉林大学公告'));
        expect(find.text('当前环境暂不支持应用内公告'), findsOneWidget);
        expect(find.byType(Dialog), findsNothing);

        await tapVisible(
          find.byKey(const ValueKey('home-configuration-button')),
        );
        expect(find.byType(ConfigurationPage), findsOneWidget);
        await tapVisible(find.text('返回'));
        expect(find.byType(HomePage), findsOneWidget);

        await tapVisible(find.byTooltip('设置'));
        expect(find.byType(SettingsPage), findsOneWidget);
        await tapVisible(find.text('连接配置'));
        expect(find.byType(ConfigurationPage), findsOneWidget);
        await tapVisible(find.text('返回'));
        expect(find.byType(SettingsPage), findsOneWidget);
        await tapVisible(find.text('连接与诊断'));
        expect(find.byType(AdvancedPage), findsOneWidget);
        expect(find.byType(AdvancedPage), findsOneWidget);
        await tapVisible(find.text('返回'));
        expect(find.byType(SettingsPage), findsOneWidget);
        await tapVisible(find.text('返回连接'));
        expect(find.byType(HomePage), findsOneWidget);

        await tapVisible(find.byTooltip('设置'));
        await tester.sendKeyEvent(LogicalKeyboardKey.escape);
        await tester.pumpAndSettle();
        expect(find.byType(HomePage), findsOneWidget);
        await tapVisible(find.byTooltip('设置'));
        await tester.sendKeyDownEvent(LogicalKeyboardKey.altLeft);
        await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
        await tester.sendKeyUpEvent(LogicalKeyboardKey.altLeft);
        await tester.pumpAndSettle();
        expect(find.byType(HomePage), findsOneWidget);
        expect(tester.takeException(), isNull);
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
        await tester.binding.setSurfaceSize(null);
      }
    });
  }
}
