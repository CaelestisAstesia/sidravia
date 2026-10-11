import 'dart:async';

import 'package:flutter/services.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

import 'home_responsive_test.dart' as fonts;

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
        final appearance = Appearance();
        await appearance.initialize();
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
          AppearanceScope(
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
          tester.widget<DropdownButton<GuiCloseBehavior>>(selector).value,
          GuiCloseBehavior.exit,
        );
        expect(
          tester.widget<DropdownButton<GuiCloseBehavior>>(selector).onChanged ==
              null,
          !enabled,
        );
        final toggle = find.byKey(const ValueKey('start-hidden-switch'));
        expect(tester.widget<DesignSwitch>(toggle).value, true);
        expect(tester.widget<DesignSwitch>(toggle).onChanged == null, !enabled);
        if (enabled) {
          await tester.ensureVisible(toggle);
          await tester.tap(toggle);
          await tester.pump();
          await appearance.settled;
          expect(appearance.startHidden, false);
        } else {
          expect(find.text('桌面托盘不可用；关闭与启动隐藏设置暂不可操作。'), findsOneWidget);
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
      await tester.tap(find.byKey(const ValueKey('home-settings-button')));
      await tester.pumpAndSettle();
      expect(find.text('启动Sidravia后自动开始认证'), findsOneWidget);
      expect(find.text('连接中断后自动尝试重新认证'), findsOneWidget);
      expect(find.text('修改后从下次连接生效。'), findsOneWidget);
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
      expect(find.text('正在处理操作，请稍后修改连接设置。'), findsOneWidget);
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
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
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
      expect(find.text('请先添加连接配置。'), findsOneWidget);
      expect(find.text('连接中断后自动尝试重新认证'), findsOneWidget);
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
  testWidgets('appearance dropdown Tab keyboard activation and selection', (
    tester,
  ) async {
    final mode = ValueNotifier(ThemeMode.system);
    addTearDown(mode.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ValueListenableBuilder<ThemeMode>(
            valueListenable: mode,
            builder: (_, value, _) => Center(
              child: AppearanceSelector(
                value: value,
                onChanged: (value) => mode.value = value,
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.sendKeyEvent(LogicalKeyboardKey.tab);
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pumpAndSettle();
    expect(find.byType(DropdownMenuItem<ThemeMode>), findsWidgets);
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pumpAndSettle();
    expect(mode.value, ThemeMode.light);
  });

  test(
    'late startup read never overwrites user selection; saves serialize',
    () async {
      final store = ControlledStore();
      final appearance = Appearance(store: store);
      appearance.value = ThemeMode.dark;
      appearance.value = ThemeMode.light;
      appearance.value = ThemeMode.system;
      store.readResult.complete(
        const GuiSettings(
          appearanceMode: GuiAppearanceMode.light,
          diagnosticsExpanded: true,
        ),
      );
      await appearance.settled;
      expect(appearance.value, ThemeMode.system);
      expect(
        store.saved
            .map(
              (settings) =>
                  (settings.appearanceMode, settings.diagnosticsExpanded),
            )
            .toList(),
        [
          (GuiAppearanceMode.dark, true),
          (GuiAppearanceMode.light, true),
          (GuiAppearanceMode.system, true),
        ],
      );
      expect(appearance.diagnosticsExpanded, true);
      expect(store.maximumActive, 1);
      appearance.dispose();
    },
  );

  test(
    'late read hydrates untouched appearance and preserves diagnostic choice',
    () async {
      final store = ControlledStore();
      final appearance = Appearance(store: store);
      appearance.diagnosticsExpanded = true;
      store.readResult.complete(
        const GuiSettings(appearanceMode: GuiAppearanceMode.dark),
      );
      await appearance.settled;
      expect(appearance.value, ThemeMode.dark);
      expect(appearance.diagnosticsExpanded, true);
      expect(
        store.saved
            .map(
              (settings) =>
                  (settings.appearanceMode, settings.diagnosticsExpanded),
            )
            .toList(),
        [(GuiAppearanceMode.dark, true)],
      );
      appearance.dispose();
    },
  );

  for (final field in ['theme', 'fold', 'close', 'hidden']) {
    test(
      'late load preserves edited $field and hydrates other three',
      () async {
        final store = ControlledStore();
        final owner = Appearance(store: store);
        switch (field) {
          case 'theme':
            owner.value = ThemeMode.light;
          case 'fold':
            owner.diagnosticsExpanded = false;
          case 'close':
            owner.closeBehavior = GuiCloseBehavior.hideToTray;
          case 'hidden':
            owner.startHidden = false;
        }
        store.readResult.complete(
          const GuiSettings(
            appearanceMode: GuiAppearanceMode.dark,
            diagnosticsExpanded: true,
            closeBehavior: GuiCloseBehavior.exit,
            startHidden: true,
            announcementFrequency: GuiAnnouncementFrequency.manualOnly,
            hiddenAnnouncementBehavior: GuiHiddenAnnouncementBehavior.onRestore,
            lastAutomaticAnnouncementDate: '2026-10-09',
          ),
        );
        await owner.settled;
        expect(
          owner.value,
          field == 'theme' ? ThemeMode.light : ThemeMode.dark,
        );
        expect(owner.diagnosticsExpanded, field != 'fold');
        expect(
          owner.closeBehavior,
          field == 'close'
              ? GuiCloseBehavior.hideToTray
              : GuiCloseBehavior.exit,
        );
        expect(owner.startHidden, field != 'hidden');
        final saved = store.saved.single;
        expect(saved.appearanceMode.name, owner.value.name);
        expect(saved.diagnosticsExpanded, owner.diagnosticsExpanded);
        expect(saved.closeBehavior, owner.closeBehavior);
        expect(saved.startHidden, owner.startHidden);
        owner.value = ThemeMode.system;
        await owner.settled;
        expect(store.saved.last.closeBehavior, saved.closeBehavior);
        expect(store.saved.last.startHidden, saved.startHidden);
        owner.diagnosticsExpanded = !owner.diagnosticsExpanded;
        await owner.settled;
        expect(store.saved.last.appearanceMode, GuiAppearanceMode.system);
        expect(store.saved.last.closeBehavior, saved.closeBehavior);
        expect(store.saved.last.startHidden, saved.startHidden);
        owner.closeBehavior = GuiCloseBehavior.exit;
        await owner.settled;
        expect(store.saved.last.startHidden, saved.startHidden);
        expect(store.saved.last.diagnosticsExpanded, owner.diagnosticsExpanded);
        owner.startHidden = true;
        await owner.settled;
        expect(store.saved.last.closeBehavior, GuiCloseBehavior.exit);
        expect(store.saved.last.appearanceMode, GuiAppearanceMode.system);
        expect(store.saved.last.diagnosticsExpanded, owner.diagnosticsExpanded);
        expect(store.maximumActive, 1);
        expect(
          owner.announcementFrequency,
          GuiAnnouncementFrequency.manualOnly,
        );
        expect(
          owner.hiddenAnnouncementBehavior,
          GuiHiddenAnnouncementBehavior.onRestore,
        );
        expect(owner.lastAutomaticAnnouncementDate, '2026-10-09');
        expect(
          store.saved.last.announcementFrequency,
          GuiAnnouncementFrequency.manualOnly,
        );
        expect(
          store.saved.last.hiddenAnnouncementBehavior,
          GuiHiddenAnnouncementBehavior.onRestore,
        );
        expect(store.saved.last.lastAutomaticAnnouncementDate, '2026-10-09');
        owner.dispose();
      },
    );
  }

  test('each preference mutation preserves the other field', () async {
    final store = ControlledStore()
      ..readResult.complete(
        const GuiSettings(
          appearanceMode: GuiAppearanceMode.dark,
          diagnosticsExpanded: true,
        ),
      );
    final appearance = Appearance(store: store);
    await appearance.initialize();
    appearance.value = ThemeMode.light;
    await appearance.settled;
    expect(store.saved.last.appearanceMode, GuiAppearanceMode.light);
    expect(store.saved.last.diagnosticsExpanded, true);
    appearance.diagnosticsExpanded = false;
    await appearance.settled;
    expect(store.saved.last.appearanceMode, GuiAppearanceMode.light);
    expect(store.saved.last.diagnosticsExpanded, false);
    appearance.dispose();
  });
  test('failed load blocks overwrite but user theme applies', () async {
    final store = ControlledStore();
    final appearance = Appearance(store: store);
    final loading = appearance.initialize();
    store.readResult.completeError(const FormatException());
    await loading;
    appearance.value = ThemeMode.dark;
    appearance.diagnosticsExpanded = true;
    await appearance.settled;
    expect(appearance.value, ThemeMode.dark);
    expect(appearance.diagnosticsExpanded, true);
    appearance.closeBehavior = GuiCloseBehavior.exit;
    appearance.startHidden = true;
    await appearance.settled;
    expect(appearance.closeBehavior, GuiCloseBehavior.exit);
    expect(appearance.startHidden, true);
    expect(store.saved, isEmpty);
    expect(appearance.feedback, contains('原文件保留'));
    appearance.dispose();
  });
  test(
    'save failure does not report success or revert current theme',
    () async {
      final store = ControlledStore()..failSave = true;
      store.readResult.complete(null);
      final appearance = Appearance(store: store);
      await appearance.initialize();
      appearance.value = ThemeMode.dark;
      await appearance.settled;
      expect(appearance.value, ThemeMode.dark);
      appearance.diagnosticsExpanded = true;
      await appearance.settled;
      expect(appearance.diagnosticsExpanded, true);
      appearance.closeBehavior = GuiCloseBehavior.exit;
      appearance.startHidden = true;
      await appearance.settled;
      expect(appearance.closeBehavior, GuiCloseBehavior.exit);
      expect(appearance.startHidden, true);
      expect(appearance.feedback, contains('未能保存'));
      appearance.dispose();
    },
  );

  for (final platform in [
    TargetPlatform.windows,
    TargetPlatform.android,
    TargetPlatform.iOS,
  ]) {
    for (final scale in [1.0, 2.0]) {
      for (final dark in [false, true]) {
        testWidgets('appearance centered $platform scale=$scale dark=$dark', (
          tester,
        ) async {
          await tester.runAsync(fonts.loadCaptureFonts);
          await tester.binding.setSurfaceSize(const Size(360, 640));
          addTearDown(() => tester.binding.setSurfaceSize(null));
          final mode = ValueNotifier(ThemeMode.system);
          addTearDown(mode.dispose);
          await tester.pumpWidget(
            MaterialApp(
              theme: (dark ? AppTheme.dark() : AppTheme.light()).copyWith(
                platform: platform,
              ),
              home: MediaQuery(
                data: MediaQueryData(
                  size: const Size(360, 640),
                  textScaler: TextScaler.linear(scale),
                ),
                child: Scaffold(
                  body: Center(
                    child: ValueListenableBuilder<ThemeMode>(
                      valueListenable: mode,
                      builder: (_, value, _) => AppearanceSelector(
                        value: value,
                        onChanged: (v) => mode.value = v,
                      ),
                    ),
                  ),
                ),
              ),
            ),
          );
          await tester.pumpAndSettle();
          final start = tester.getSize(
            find.byKey(const ValueKey('appearance-selector')),
          );
          for (final value in ThemeMode.values) {
            mode.value = value;
            await tester.pumpAndSettle();
            final region = find.byKey(
              ValueKey('appearance-text-region-${value.name}'),
            );
            final frame = tester.getRect(
              find.byKey(const ValueKey('appearance-selector')),
            );
            expect(
              tester.getRect(region).center.dy,
              closeTo(frame.center.dy, .01),
              reason: 'selected region must center within the full outer frame',
            );
            final arrow = find.descendant(
              of: find.byKey(const ValueKey('appearance-selector')),
              matching: find.byIcon(Icons.arrow_drop_down),
            );
            expect(
              tester.getRect(arrow).center.dy,
              closeTo(frame.center.dy, .01),
            );
            final text = find.descendant(
              of: region,
              matching: find.byType(Text),
            );
            expect(
              tester.getRect(text).center.dx,
              closeTo(tester.getRect(region).center.dx, .01),
            );
            expect(
              tester.getRect(text).center.dy,
              closeTo(tester.getRect(region).center.dy, .01),
            );
            expect(
              tester.getSize(find.byKey(const ValueKey('appearance-selector'))),
              start,
            );
            expect(tester.takeException(), isNull);
          }
          expect(
            start.height,
            greaterThanOrEqualTo(platform == TargetPlatform.windows ? 30 : 48),
          );
          await tester.tap(find.byType(DropdownButton<ThemeMode>));
          await tester.pumpAndSettle();
          await tester.tap(find.text('浅色').last);
          await tester.pumpAndSettle();
          expect(mode.value, ThemeMode.light);
        });
      }
    }
  }
}

class ControlledStore implements GuiSettingsStore {
  final readResult = Completer<GuiSettings?>();
  final saved = <GuiSettings>[];
  int active = 0, maximumActive = 0;
  bool failSave = false;
  @override
  bool get persistent => true;
  @override
  Future<GuiSettings?> read() => readResult.future;
  @override
  Future<void> save(GuiSettings settings) async {
    active++;
    if (active > maximumActive) maximumActive = active;
    await Future<void>.delayed(Duration.zero);
    active--;
    if (failSave) throw StateError('fixture');
    saved.add(settings);
  }
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
