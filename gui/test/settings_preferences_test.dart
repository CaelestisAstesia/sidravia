import 'dart:async';

import 'package:flutter/services.dart';
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

void main() {
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
      store.readResult.complete(GuiAppearanceMode.light);
      await appearance.settled;
      expect(appearance.value, ThemeMode.system);
      expect(store.saved, [
        GuiAppearanceMode.dark,
        GuiAppearanceMode.light,
        GuiAppearanceMode.system,
      ]);
      expect(store.maximumActive, 1);
      appearance.dispose();
    },
  );
  test('failed load blocks overwrite but user theme applies', () async {
    final store = ControlledStore();
    final appearance = Appearance(store: store);
    final loading = appearance.initialize();
    store.readResult.completeError(const FormatException());
    await loading;
    appearance.value = ThemeMode.dark;
    await appearance.settled;
    expect(appearance.value, ThemeMode.dark);
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
      expect(appearance.feedback, contains('未能保存'));
      appearance.dispose();
    },
  );

  for (final platform in [TargetPlatform.windows, TargetPlatform.android]) {
    for (final scale in [1.0, 2.0]) {
      for (final dark in [false, true]) {
        testWidgets('appearance centered $platform scale=$scale dark=$dark', (
          tester,
        ) async {
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
            greaterThanOrEqualTo(platform == TargetPlatform.android ? 48 : 30),
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
  final readResult = Completer<GuiAppearanceMode?>();
  final saved = <GuiAppearanceMode>[];
  int active = 0, maximumActive = 0;
  bool failSave = false;
  @override
  bool get persistent => true;
  @override
  Future<GuiAppearanceMode?> read() => readResult.future;
  @override
  Future<void> save(GuiAppearanceMode mode) async {
    active++;
    if (active > maximumActive) maximumActive = active;
    await Future<void>.delayed(Duration.zero);
    active--;
    if (failSave) throw StateError('fixture');
    saved.add(mode);
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
