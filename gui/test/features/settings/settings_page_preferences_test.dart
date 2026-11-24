import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/preferences/gui_preferences.dart';
import 'package:sidravia_gui/application/preferences/gui_settings.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

import '../../support/offline_gui_controller.dart';

void main() {
  for (final enabled in [true, false]) {
    testWidgets(
      'desktop settings enabled=$enabled preserve backend and fit narrow text',
      (tester) async {
        const channel = MethodChannel(desktopPresenceChannelName);
        final messenger =
            TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
        messenger.setMockMethodCallHandler(channel, (_) async => 'primary');
        addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
        final desktop = enabled
            ? DesktopPresence(channel: channel)
            : DesktopPresence.disabled();
        await desktop.initialize();
        final appearance = GuiPreferences();
        await appearance.initialize();
        appearance.logLevel = 'info';
        appearance.closeBehavior = GuiCloseBehavior.exit;
        appearance.startHidden = true;
        await appearance.settled;
        final client = OfflineDemoClient();
        final controller = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client,
          pollDelay: const Duration(days: 1),
        );
        await controller.start();
        final before = controller.capabilities.configuration!;
        await tester.binding.setSurfaceSize(const Size(360, 640));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        await tester.pumpWidget(
          GuiPreferencesScope(
            appearance: appearance,
            child: MaterialApp(
              home: MediaQuery(
                data: const MediaQueryData(textScaler: TextScaler.linear(2)),
                child: SettingsPage(
                  controller: controller,
                  desktopPresence: desktop,
                  onNavigate: (_) {},
                  onBack: () {},
                ),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        final selector = find.byKey(const ValueKey('close-behavior-selector'));
        expect(
          find.descendant(of: selector, matching: find.text('退出程序')),
          findsOneWidget,
        );
        expect(tester.widget<TextButton>(selector).onPressed == null, !enabled);
        final toggle = find.byKey(const ValueKey('start-hidden-switch'));
        expect(tester.widget<DesignSwitch>(toggle).value, true);
        expect(tester.widget<DesignSwitch>(toggle).onChanged == null, !enabled);
        expect(find.text('托盘当前不可用'), enabled ? findsNothing : findsNWidgets(2));
        if (enabled) {
          await tester.ensureVisible(toggle);
          await tester.tap(toggle);
          await tester.pump();
          await appearance.settled;
          expect(appearance.startHidden, false);
        } else {
          expect(find.text('桌面托盘不可用；关闭与启动隐藏设置暂不可操作。'), findsNothing);
        }
        expect(
          controller.capabilities.configuration!.autoLogin,
          before.autoLogin,
        );
        expect(
          controller.capabilities.configuration!.autoReconnect,
          before.autoReconnect,
        );
        expect(
          client.operations.where((e) => e.startsWith('configuration.')),
          isEmpty,
        );
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        await controller.close();
        controller.dispose();
        client.dispose();
        desktop.dispose();
        appearance.dispose();
      },
    );
  }

  testWidgets(
    'formal settings reconnect saves true false, failures, guards, no session mutation',
    (tester) async {
      final client = ReconnectClient();
      final controller = offlineGuiController(client);
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light().copyWith(platform: TargetPlatform.windows),
          home: SidraviaShell(controller: controller),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('home-settings-button')));
      await tester.pumpAndSettle();
      expect(find.text('启动Sidravia后自动开始认证'), findsOneWidget);
      expect(find.textContaining('连接中断后自动尝试重新认证'), findsOneWidget);
      expect(find.textContaining('修改后从下次连接生效'), findsNothing);
      expect(find.text('核心、会话与故障信息'), findsOneWidget);
      expect(find.text('技术诊断'), findsNothing);
      final toggle = find.byKey(const ValueKey('auto-reconnect-switch'));
      final old = controller.capabilities.configuration!;
      expect(
        await controller.setAutoReconnect(
          configurationId: 'wrong',
          autoReconnect: false,
        ),
        false,
      );
      expect(client.calls, 0);
      client.fail = true;
      await tester.ensureVisible(toggle);
      await tester.tap(toggle);
      await tester.pumpAndSettle();
      expect(controller.notice, isNotNull);
      expect(
        controller.capabilities.configuration!.autoReconnect,
        old.autoReconnect,
      );
      client.fail = false;
      for (final value in [!old.autoReconnect, old.autoReconnect]) {
        await tester.tap(toggle);
        await tester.pumpAndSettle();
        final c = controller.capabilities.configuration!;
        expect(c.autoReconnect, value);
        expect(c.id, old.id);
        expect(c.username, old.username);
        expect(c.autoLogin, old.autoLogin);
        expect(controller.notice, isNull);
        expect(tester.widget<DesignSwitch>(toggle).value, value);
      }
      client.gate = Completer<void>();
      final request = controller.setAutoReconnect(
        configurationId: old.id,
        autoReconnect: false,
      );
      await tester.pump();
      expect(controller.busy, true);
      expect(tester.widget<DesignSwitch>(toggle).onChanged, isNull);
      expect(find.text('正在处理操作，请稍后修改连接设置'), findsOneWidget);
      expect(
        await controller.setAutoReconnect(
          configurationId: old.id,
          autoReconnect: true,
        ),
        false,
      );
      client.gate!.complete();
      expect(await request, true);
      await tester.pumpAndSettle();
      expect(
        client.operations.where((name) => name.startsWith('session.')),
        isEmpty,
      );
      final currentSession = (await client.sessionList()).single;
      expect(
        await controller.setAutoReconnect(
          configurationId: old.id,
          autoReconnect: true,
        ),
        isTrue,
      );
      expect((await client.sessionList()).single.id, currentSession.id);
      final reconnected = await client.sessionRestart(currentSession.id);
      expect(reconnected.id, isNot(currentSession.id));
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );
  testWidgets(
    'empty configuration reason remains separate from functional copy',
    (tester) async {
      final client = OfflineDemoClient()..selectScenario('empty');
      final controller = offlineGuiController(client);
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(
          home: SettingsPage(
            controller: controller,
            onNavigate: (_) {},
            onBack: () {},
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('请先添加连接配置'), findsOneWidget);
      expect(find.textContaining('连接中断后自动尝试重新认证'), findsOneWidget);
      expect(
        tester
            .widget<DesignSwitch>(
              find.byKey(const ValueKey('auto-reconnect-switch')),
            )
            .onChanged,
        isNull,
      );
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );
}

class ReconnectClient extends OfflineDemoClient {
  bool fail = false;
  int calls = 0;
  Completer<void>? gate;
  @override
  Future<ConfigurationSummary> configurationSetAutoReconnect({
    required String configurationId,
    required bool autoReconnect,
    bool allowInsecureStorage = false,
  }) async {
    calls++;
    if (fail) throw const IpcRequestFailure('session_state_conflict');
    if (gate != null) await gate!.future;
    return super.configurationSetAutoReconnect(
      configurationId: configurationId,
      autoReconnect: autoReconnect,
    );
  }
}
