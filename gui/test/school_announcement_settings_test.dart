import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

void main() {
  testWidgets(
    'announcement choices save immediately and retain hidden choice',
    (tester) async {
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.binding.setSurfaceSize(const Size(360, 640));
      addTearDown(() => tester.binding.setSurfaceSize(null));

      final store = MemoryGuiSettingsStore()
        ..settings = const GuiSettings(
          appearanceMode: GuiAppearanceMode.dark,
          diagnosticsExpanded: true,
          closeBehavior: GuiCloseBehavior.exit,
          startHidden: true,
          announcementFrequency: GuiAnnouncementFrequency.everyConnection,
          hiddenAnnouncementBehavior: GuiHiddenAnnouncementBehavior.onRestore,
          lastAutomaticAnnouncementDate: '2026-10-09',
        );
      final appearance = Appearance(store: store);
      await appearance.initialize();
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await controller.start();
      final initialConfiguration = controller.capabilities.configuration!;
      final initialSession = controller.snapshot!.sessions.single;
      final initialOperations = client.operations;

      await tester.pumpWidget(
        AppearanceScope(
          appearance: appearance,
          child: MaterialApp(
            theme: AppTheme.light().copyWith(platform: TargetPlatform.windows),
            darkTheme: AppTheme.dark().copyWith(
              platform: TargetPlatform.windows,
            ),
            themeMode: appearance.value,
            home: MediaQuery(
              data: const MediaQueryData(textScaler: TextScaler.linear(1)),
              child: SettingsPage(
                controller: controller,
                onNavigate: (_) {},
                onBack: () {},
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      final frequencyKey = const ValueKey('announcement-frequency-selector');
      final hiddenKey = const ValueKey('hidden-announcement-behavior-selector');
      expect(find.text('自动显示公告'), findsOneWidget);
      expect(find.text('连接成功后显示公告。'), findsOneWidget);
      expect(find.text('恢复主窗口时显示公告。'), findsOneWidget);
      expect(
        tester
            .widget<DropdownButton<GuiAnnouncementFrequency>>(
              find.byKey(frequencyKey),
            )
            .value,
        GuiAnnouncementFrequency.everyConnection,
      );
      expect(
        tester
            .widget<DropdownButton<GuiHiddenAnnouncementBehavior>>(
              find.byKey(hiddenKey),
            )
            .value,
        GuiHiddenAnnouncementBehavior.onRestore,
      );

      await tester.ensureVisible(find.byKey(frequencyKey));
      await tester.tap(find.byKey(frequencyKey));
      await tester.pumpAndSettle();
      await tester.tap(find.text('仅手动查看').last);
      await tester.pumpAndSettle();
      await appearance.settled;
      expect(
        appearance.announcementFrequency,
        GuiAnnouncementFrequency.manualOnly,
      );
      expect(find.byKey(hiddenKey), findsNothing);
      expect(
        store.settings!.announcementFrequency,
        GuiAnnouncementFrequency.manualOnly,
      );
      expect(
        store.settings!.hiddenAnnouncementBehavior,
        GuiHiddenAnnouncementBehavior.onRestore,
      );
      expect(store.settings!.lastAutomaticAnnouncementDate, '2026-10-09');

      await tester.ensureVisible(find.byKey(frequencyKey));
      await tester.tap(find.byKey(frequencyKey));
      await tester.pumpAndSettle();
      await tester.tap(find.text('每天首次连接成功后').last);
      await tester.pumpAndSettle();
      await appearance.settled;
      expect(
        appearance.announcementFrequency,
        GuiAnnouncementFrequency.dailyFirstConnection,
      );
      expect(find.byKey(hiddenKey), findsOneWidget);
      expect(
        tester
            .widget<DropdownButton<GuiHiddenAnnouncementBehavior>>(
              find.byKey(hiddenKey),
            )
            .value,
        GuiHiddenAnnouncementBehavior.onRestore,
      );

      await tester.ensureVisible(find.byKey(hiddenKey));
      await tester.tap(find.byKey(hiddenKey));
      await tester.pumpAndSettle();
      await tester.tap(find.text('直接显示公告').last);
      await tester.pumpAndSettle();
      await appearance.settled;
      expect(
        store.settings!.hiddenAnnouncementBehavior,
        GuiHiddenAnnouncementBehavior.showImmediately,
      );
      expect(
        store.settings!.announcementFrequency,
        GuiAnnouncementFrequency.dailyFirstConnection,
      );
      expect(store.settings!.lastAutomaticAnnouncementDate, '2026-10-09');
      expect(store.settings!.appearanceMode, GuiAppearanceMode.dark);
      expect(store.settings!.diagnosticsExpanded, true);
      expect(store.settings!.closeBehavior, GuiCloseBehavior.exit);
      expect(store.settings!.startHidden, true);
      expect(client.operations, initialOperations);
      final currentConfiguration = controller.capabilities.configuration!;
      expect(currentConfiguration.id, initialConfiguration.id);
      expect(currentConfiguration.username, initialConfiguration.username);
      expect(currentConfiguration.autoLogin, initialConfiguration.autoLogin);
      expect(
        currentConfiguration.autoReconnect,
        initialConfiguration.autoReconnect,
      );
      expect(controller.snapshot!.sessions.single.id, initialSession.id);
      expect(tester.takeException(), isNull);

      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
      client.dispose();
      appearance.dispose();
    },
  );

  for (final brightness in [Brightness.light, Brightness.dark]) {
    for (final scale in [1.0, 2.0]) {
      testWidgets(
        'announcement controls fit 360x640 ${brightness.name} scale $scale',
        (tester) async {
          tester.view.devicePixelRatio = 1;
          addTearDown(tester.view.resetDevicePixelRatio);
          await tester.binding.setSurfaceSize(const Size(360, 640));
          addTearDown(() => tester.binding.setSurfaceSize(null));

          final store = MemoryGuiSettingsStore();
          final appearance = Appearance(store: store);
          await appearance.initialize();
          final client = OfflineDemoClient();
          final controller = GuiController(
            bootstrapper: OfflineDemoBootstrap(),
            connector: (_) async => client,
            pollDelay: const Duration(days: 1),
          );
          await controller.start();
          await tester.pumpWidget(
            AppearanceScope(
              appearance: appearance,
              child: MaterialApp(
                theme: AppTheme.light().copyWith(
                  platform: TargetPlatform.windows,
                ),
                darkTheme: AppTheme.dark().copyWith(
                  platform: TargetPlatform.windows,
                ),
                themeMode: brightness == Brightness.dark
                    ? ThemeMode.dark
                    : ThemeMode.light,
                home: MediaQuery(
                  data: MediaQueryData(textScaler: TextScaler.linear(scale)),
                  child: SettingsPage(
                    controller: controller,
                    onNavigate: (_) {},
                    onBack: () {},
                  ),
                ),
              ),
            ),
          );
          await tester.pumpAndSettle();

          final frequency = find.byKey(
            const ValueKey('announcement-frequency-selector'),
          );
          final hidden = find.byKey(
            const ValueKey('hidden-announcement-behavior-selector'),
          );
          await tester.ensureVisible(frequency);
          await tester.tap(frequency);
          await tester.pumpAndSettle();
          expect(find.text('每次连接成功后'), findsWidgets);
          expect(find.text('每天首次连接成功后'), findsWidgets);
          expect(find.text('仅手动查看'), findsWidgets);
          expect(tester.takeException(), isNull);
          await tester.tapAt(const Offset(10, 10));
          await tester.pumpAndSettle();

          await tester.ensureVisible(hidden);
          await tester.tap(hidden);
          await tester.pumpAndSettle();
          expect(find.text('直接显示公告'), findsWidgets);
          expect(find.text('发送系统通知点击查看'), findsWidgets);
          expect(find.text('恢复主窗口时显示'), findsWidgets);
          expect(tester.takeException(), isNull);
          await tester.tapAt(const Offset(10, 10));
          await tester.pumpAndSettle();
          expect(tester.takeException(), isNull);

          await tester.pumpWidget(const SizedBox.shrink());
          await controller.close();
          controller.dispose();
          client.dispose();
          appearance.dispose();
        },
      );
    }
  }

  testWidgets('announcement controls are disabled without AppearanceScope', (
    tester,
  ) async {
    final client = OfflineDemoClient();
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
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

    final frequency = tester.widget<DropdownButton<GuiAnnouncementFrequency>>(
      find.byKey(const ValueKey('announcement-frequency-selector')),
    );
    final hidden = tester.widget<DropdownButton<GuiHiddenAnnouncementBehavior>>(
      find.byKey(const ValueKey('hidden-announcement-behavior-selector')),
    );
    expect(frequency.onChanged, isNull);
    expect(hidden.onChanged, isNull);
    final frequencyFinder = find.byKey(
      const ValueKey('announcement-frequency-selector'),
    );
    await tester.ensureVisible(frequencyFinder);
    await tester.tap(frequencyFinder);
    await tester.pumpAndSettle();
    expect(find.text('每次连接成功后'), findsNothing);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
    await controller.close();
    controller.dispose();
    client.dispose();
  });
}
