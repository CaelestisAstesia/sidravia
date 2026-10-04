import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/interactive_demo_main.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  testWidgets(
    'interactive entry labels offline mode and uses the production shell',
    (tester) async {
      await tester.pumpWidget(const OfflineDemoApp());
      await tester.pumpAndSettle();
      expect(find.text('离线演示 · 勿输入真实账号或密码'), findsOneWidget);
      expect(find.byType(SidraviaShell), findsOneWidget);
      expect(find.byType(HomePage), findsOneWidget);
      expect(find.byType(HomeView), findsOneWidget);
      await tester.tap(find.text('断开连接'));
      await tester.pumpAndSettle();
      expect(find.text('未连接'), findsOneWidget);
      expect(find.text('已模拟断开连接；没有影响真实网络'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  testWidgets(
    'production shell routes home configuration, settings, details and diagnostics',
    (tester) async {
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light(),
          home: SidraviaShell(controller: controller),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('吉林大学'));
      await tester.pumpAndSettle();
      expect(find.byType(ConfigurationPage), findsOneWidget);
      await tester.enterText(find.byType(TextField).first, 'demo-student02');
      await tester.tap(find.text('保存更改'));
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);
      expect(find.text('demo-student02'), findsOneWidget);
      expect(client.operations, contains('configuration.update'));

      await tester.tap(find.byTooltip('设置'));
      await tester.pumpAndSettle();
      expect(find.byType(SettingsPage), findsOneWidget);
      await tester.tap(find.text('连接配置'));
      await tester.pumpAndSettle();
      expect(find.byType(ConfigurationPage), findsOneWidget);
      await tester.tap(find.text('返回'));
      await tester.pumpAndSettle();
      expect(find.byType(SettingsPage), findsOneWidget);
      await tester.tap(find.byType(SwitchListTile).first);
      await tester.pumpAndSettle();
      expect(controller.capabilities.configuration!.autoLogin, isTrue);
      expect(client.operations, contains('configuration.set_auto_login'));
      await tester.tap(find.text('返回连接'));
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);

      await tester.tap(find.text('连接详情'));
      await tester.pumpAndSettle();
      expect(
        tester.widget<AdvancedPage>(find.byType(AdvancedPage)).detailsOnly,
        isTrue,
      );
      await tester.ensureVisible(find.text('技术诊断'));
      await tester.tap(find.text('技术诊断'));
      await tester.pumpAndSettle();
      expect(
        tester.widget<AdvancedPage>(find.byType(AdvancedPage)).detailsOnly,
        isFalse,
      );
      await tester.tap(find.text('返回'));
      await tester.pumpAndSettle();
      expect(
        tester.widget<AdvancedPage>(find.byType(AdvancedPage)).detailsOnly,
        isTrue,
      );
      await tester.tap(find.text('返回'));
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );

  testWidgets(
    'production home buttons stop and restart through the fake typed client',
    (tester) async {
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(home: SidraviaShell(controller: controller)),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('断开连接'));
      await tester.pumpAndSettle();
      expect(find.text('未连接'), findsOneWidget);
      expect(controller.capabilities.retainedSession!.state, 'suspended');
      expect(client.operations.last, 'session.stop');
      await tester.tap(find.text('开始连接'));
      await tester.pumpAndSettle();
      expect(find.text('已连接'), findsOneWidget);
      expect(controller.capabilities.retainedSession!.state, 'authenticated');
      expect(client.operations.last, 'session.start_configuration');
      expect(client.feedback, contains('没有进行真实认证'));
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );

  testWidgets(
    'production home opens the existing announcement modal and marks it read',
    (tester) async {
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      final now = DateTime.utc(2026, 10, 4);
      final fetcher = OfflineAnnouncementFetcher(now);
      final announcements = AnnouncementController(
        endpoint: OfflineAnnouncementFetcher.endpoint,
        store: MemoryAnnouncementStore(),
        fetcher: fetcher,
        nowUtc: () => now,
      );
      await controller.start();
      await announcements.start();
      await tester.pumpWidget(
        MaterialApp(
          home: SidraviaShell(
            controller: controller,
            announcements: announcements,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(announcements.unreadCount, 1);
      await tester.tap(find.byType(HomeNoticeView));
      await tester.pumpAndSettle();
      expect(find.byType(BottomSheet), findsOneWidget);
      expect(find.textContaining('这是离线演示公告'), findsOneWidget);
      expect(announcements.unreadCount, 0);
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      announcements.dispose();
      client.dispose();
      expect(client.closed, isTrue);
      expect(fetcher.closed, isTrue);
    },
  );
}
