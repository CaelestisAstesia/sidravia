import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/application/preferences/gui_preferences.dart';
import 'package:sidravia_gui/application/preferences/gui_settings.dart';

void main() {
  testWidgets(
    'application selector saves mode and explains muted announcements in row',
    (tester) async {
      final client = OfflineDemoClient();
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      final store = MemoryGuiSettingsStore();
      final prefs = GuiPreferences(store: store);
      await prefs.initialize();
      await c.start();
      try {
        await tester.pumpWidget(
          GuiPreferencesScope(
            appearance: prefs,
            child: MaterialApp(
              home: ValueListenableBuilder<ThemeMode>(
                valueListenable: prefs,
                builder: (_, mode, child) => Scaffold(
                  body: SettingsPage(
                    controller: c,
                    onNavigate: (_) {},
                    onBack: () {},
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        expect(find.text('仅异常'), findsOneWidget);
        final dropdown = find.byKey(
          const ValueKey('notification-mode-selector'),
        );
        await tester.ensureVisible(dropdown);
        await tester.tap(dropdown);
        await tester.pumpAndSettle();
        await tester.tap(find.text('不提醒').last);
        await tester.pumpAndSettle();
        await prefs.settled;
        expect(store.settings!.notificationMode, GuiNotificationMode.silent);
        expect(find.text('系统通知已关闭，可在首页查看公告'), findsOneWidget);
        expect(tester.takeException(), isNull);
      } finally {
        await tester.pumpWidget(const SizedBox());
        await c.close();
        c.dispose();
        prefs.dispose();
      }
    },
  );
}
