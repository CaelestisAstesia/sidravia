import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
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

  for (final size in const [
    Size(360, 640),
    Size(400, 690),
    Size(900, 720),
    Size(1440, 900),
  ]) {
    testWidgets('page navigation and home announcements at $size', (
      tester,
    ) async {
      final now = DateTime.utc(2026, 10, 10);
      final client = OfflineDemoClient(now: now);
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      final fetcher = OfflineAnnouncementFetcher(now);
      final announcements = AnnouncementController(
        endpoint: OfflineAnnouncementFetcher.endpoint,
        store: MemoryAnnouncementStore(),
        fetcher: fetcher,
        refreshInterval: Duration.zero,
        nowUtc: () => now,
      );
      try {
        await controller.start();
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
        await tapVisible(find.text('校园网维护安排'));
        expect(
          find.byKey(const ValueKey('announcement-panel')),
          findsOneWidget,
        );
        expect(find.textContaining('这是离线演示公告。'), findsOneWidget);
        await tapVisible(find.byTooltip('关闭公告'));

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
        await tapVisible(find.text('诊断'));
        expect(find.byType(AdvancedPage), findsOneWidget);
        expect(
          tester.widget<AdvancedPage>(find.byType(AdvancedPage)).detailsOnly,
          isFalse,
        );
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
        await tester.pumpWidget(const SizedBox.shrink());
        await controller.close();
        controller.dispose();
        announcements.dispose();
        client.dispose();
        await tester.binding.setSurfaceSize(null);
      }
    });
  }
}
