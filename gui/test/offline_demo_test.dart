import 'school_announcement_controller_test.dart' as school_dispatch;

import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/interactive_demo_main.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  test(
    'offline reconnect preference waits for manual resume or restart',
    () async {
      final client = OfflineDemoClient();
      final before = (await client.sessionList()).single;
      await client.configurationSetAutoReconnect(
        configurationId: 'demo-config',
        autoReconnect: true,
      );

      final activeEnsure = await client.sessionEnsureRunning(before.id);
      expect(activeEnsure.id, before.id);
      expect(activeEnsure.revision, before.revision);
      expect(
        activeEnsure.authenticationEstablishedAt,
        before.authenticationEstablishedAt,
      );
      final activeStart = await client.sessionStartConfiguration('demo-config');
      expect(activeStart.id, before.id);
      expect(activeStart.revision, before.revision);

      await client.sessionStop(before.id);
      final resumed = await client.sessionEnsureRunning(before.id);
      expect(resumed.id, isNot(before.id));
      expect(resumed.state, SessionState.authenticated);

      client.selectScenario('stopping');
      final stopping = (await client.sessionList()).single;
      await client.configurationSetAutoReconnect(
        configurationId: 'demo-config',
        autoReconnect: false,
      );
      final stoppedResume = await client.sessionEnsureRunning(stopping.id);
      expect(stoppedResume.id, isNot(stopping.id));
      expect(stoppedResume.state, SessionState.authenticated);
      client.dispose();
    },
  );

  test(
    'offline metadata password and preferences preserve explicit binding',
    () async {
      final client = OfflineDemoClient()..selectScenario('empty');
      final policy = NetworkBindingPolicy.explicit('lo', '127.0.0.1');
      final created = await client.configurationCreate(
        networkBindingPolicy: policy,
        institutionProfileId: 'jlu',
        username: 'u',
        password: 'p',
        autoLogin: false,
        autoReconnect: true,
        allowInsecureStorage: false,
      );
      final updated = await client.configurationUpdate(
        configurationId: created.id,
        institutionProfileId: 'jlu',
        username: 'new',
        password: 'replacement',
      );
      expect(updated.networkBindingPolicy, policy);
      expect(
        (await client.configurationSetAutoLogin(
          configurationId: created.id,
          autoLogin: true,
        )).networkBindingPolicy,
        policy,
      );
      expect(
        (await client.configurationSetAutoReconnect(
          configurationId: created.id,
          autoReconnect: false,
        )).networkBindingPolicy,
        policy,
      );
      expect(
        (await client.configurationSetPassword(
          configurationId: created.id,
          password: 'new',
        )).networkBindingPolicy,
        policy,
      );
      client.dispose();
    },
  );

  test(
    'GUI create defaults agree with explicit demo input semantics',
    () async {
      final client = OfflineDemoClient()..selectScenario('empty');
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
        isTrue,
      );
      expect(
        c.snapshot!.configurations.single.networkBindingPolicy,
        const NetworkBindingPolicy.automatic(),
      );
      expect(c.snapshot!.configurations.single.autoLogin, isFalse);
      expect(c.snapshot!.configurations.single.autoReconnect, isTrue);
      c.dispose();
      client.dispose();
      final explicit = OfflineDemoClient()..selectScenario('empty');
      final result = await explicit.configurationCreate(
        networkBindingPolicy: const NetworkBindingPolicy.automatic(),
        institutionProfileId: 'jlu',
        username: 'u',
        password: 'p',
        autoLogin: true,
        autoReconnect: false,
        allowInsecureStorage: false,
      );
      expect(result.autoLogin, isTrue);
      expect(result.autoReconnect, isFalse);
      explicit.dispose();
    },
  );

  testWidgets(
    'developer controls switch actual state feed and theme outside shell',
    (tester) async {
      try {
        await tester.pumpWidget(const OfflineDemoApp());
        await tester.pumpAndSettle();
        await tester.tap(find.byTooltip('演示场景'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('等待重试').last);
        await tester.pumpAndSettle();
        expect(find.text('停止重试'), findsOneWidget);
        await tester.tap(find.text('离线托盘预览'));
        await tester.pumpAndSettle();
        expect(find.text('等待重试'), findsNWidgets(2));
        expect(find.text('显示主界面'), findsOneWidget);
        expect(find.text('退出 Sidravia'), findsOneWidget);
        await tester.tap(find.text('显示主界面'));
        await tester.pumpAndSettle();
        expect(find.byType(HomeNoticeView), findsOneWidget);
        expect(find.byKey(const ValueKey('home-notice')), findsOneWidget);
        expect(find.text('吉林大学公告'), findsOneWidget);
        await tester.ensureVisible(find.byType(HomeNoticeView));
        await tester.pump();
        await tester.tap(find.byType(HomeNoticeView));
        await tester.pumpAndSettle();
        expect(find.text('离线公告占位预览'), findsOneWidget);
        await tester.tap(find.text('关闭'));
        await tester.pumpAndSettle();
        await tester.tap(find.byTooltip('演示外观'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('深色').last);
        await tester.pumpAndSettle();
        expect(
          Theme.of(tester.element(find.byType(HomePage))).brightness,
          Brightness.dark,
        );
      } finally {
        await school_dispatch.closeShellOwners(tester);
        await tester.pumpWidget(const SizedBox.shrink());
      }
    },
  );

  testWidgets(
    'interactive entry labels offline mode and uses the production shell',
    (tester) async {
      try {
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
      } finally {
        await school_dispatch.closeShellOwners(tester);
        await tester.pumpWidget(const SizedBox.shrink());
      }
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
      await tester.ensureVisible(find.text('保存修改'));
      await tester.tap(find.text('保存修改'));
      await tester.pumpAndSettle();
      expect(find.text('保存连接配置？'), findsOneWidget);
      await tester.tap(find.text('保存并断开'));
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
      await tester.tap(find.byKey(const ValueKey('auto-login-switch')));
      await tester.pumpAndSettle();
      expect(controller.capabilities.configuration!.autoLogin, isTrue);
      expect(client.operations, contains('configuration.set_auto_login'));
      await tester.tap(find.text('返回仪表盘'));
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);

      expect(controller.snapshot!.sessions, isEmpty);
      await controller.startConfiguration('demo-config');
      await tester.pumpAndSettle();
      await tester.tap(find.text('连接与诊断'));
      await tester.pumpAndSettle();
      expect(find.byType(AdvancedPage), findsOneWidget);
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
      expect(
        controller.capabilities.retainedSession!.state,
        SessionState.suspended,
      );
      expect(client.operations.last, 'session.stop');
      await tester.tap(find.text('开始连接'));
      await tester.pumpAndSettle();
      expect(find.text('已连接'), findsOneWidget);
      expect(
        controller.capabilities.retainedSession!.state,
        SessionState.authenticated,
      );
      expect(client.operations.last, 'session.start_configuration');
      expect(client.feedback, contains('没有进行真实认证'));
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );

  testWidgets(
    'production home keeps the manual school entry with explicit Unsupported feedback',
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
        expect(find.byKey(const ValueKey('home-notice')), findsOneWidget);
        await tester.tap(find.byType(HomeNoticeView));
        await tester.pumpAndSettle();
        expect(find.byType(Dialog), findsNothing);
        expect(find.text('当前环境暂不支持应用内公告'), findsOneWidget);
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
      expect(client.closed, isTrue);
    },
  );
}
