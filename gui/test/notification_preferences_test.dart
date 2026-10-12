import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

class DelayedPreferences implements GuiSettingsStore {
  final readResult = Completer<GuiSettings?>();
  final saved = <GuiSettings>[];
  bool fail = false;
  @override
  bool get persistent => true;
  @override
  Future<GuiSettings?> read() => readResult.future;
  @override
  Future<void> save(GuiSettings value) async {
    if (fail) throw StateError('storage unavailable');
    saved.add(value);
  }
}

void main() {
  test('old settings migrate to abnormal without losing fields', () {
    for (var schema = 1; schema <= 5; schema++) {
      final fields = <String, Object?>{
        'schemaVersion': schema,
        'appearanceMode': 'dark',
        if (schema >= 2) 'diagnosticsExpanded': true,
        if (schema >= 3) ...{'closeBehavior': 'exit', 'startHidden': true},
        if (schema >= 4) ...{
          'announcementFrequency': 'everyConnection',
          'hiddenAnnouncementBehavior': 'onRestore',
          'lastAutomaticAnnouncementDate': '2026-10-12',
        },
        if (schema == 5) 'logLevel': 'debug',
      };
      final migrated = decodeGuiSettings(jsonEncode(fields));
      expect(migrated.notificationMode, GuiNotificationMode.abnormal);
      expect(migrated.appearanceMode, GuiAppearanceMode.dark);
      if (schema >= 4) {
        expect(
          migrated.announcementFrequency,
          GuiAnnouncementFrequency.everyConnection,
        );
        expect(migrated.lastAutomaticAnnouncementDate, '2026-10-12');
      }
      if (schema == 5) expect(migrated.logLevel, 'debug');
    }
  });
  test('schema6 roundtrip accepts only exact typed modes', () {
    for (final mode in GuiNotificationMode.values) {
      final encoded = encodeGuiSettings(GuiSettings(notificationMode: mode));
      expect(jsonDecode(encoded)['schemaVersion'], 6);
      expect(decodeGuiSettings(encoded).notificationMode, mode);
    }
    final base = jsonDecode(
      encodeGuiSettings(const GuiSettings()),
    ) as Map<String, dynamic>;
    for (final invalid in [
      null,
      1,
      true,
      'errors',
      '',
      ['all'],
    ]) {
      expect(
        () => decodeGuiSettings(
          jsonEncode({...base, 'notificationMode': invalid}),
        ),
        throwsFormatException,
      );
    }
    expect(
      () => decodeGuiSettings(jsonEncode({...base, 'extra': true})),
      throwsFormatException,
    );
    expect(
      () =>
          decodeGuiSettings(jsonEncode({...base}..remove('notificationMode'))),
      throwsFormatException,
    );
  });
  test(
    'mode changed during load wins and queued writes preserve other prefs',
    () async {
      final store = DelayedPreferences();
      final prefs = Appearance(store: store);
      prefs.notificationMode = GuiNotificationMode.silent;
      store.readResult.complete(
        const GuiSettings(
          appearanceMode: GuiAppearanceMode.dark,
          logLevel: 'debug',
          closeBehavior: GuiCloseBehavior.exit,
          startHidden: true,
          notificationMode: GuiNotificationMode.all,
          announcementFrequency: GuiAnnouncementFrequency.everyConnection,
        ),
      );
      await prefs.settled;
      expect(prefs.notificationMode, GuiNotificationMode.silent);
      prefs.notificationMode = GuiNotificationMode.all;
      prefs.diagnosticsExpanded = true;
      await prefs.settled;
      expect(store.saved.last.notificationMode, GuiNotificationMode.all);
      expect(store.saved.last.logLevel, 'debug');
      expect(store.saved.last.appearanceMode, GuiAppearanceMode.dark);
      expect(store.saved.last.closeBehavior, GuiCloseBehavior.exit);
      expect(store.saved.last.startHidden, isTrue);
      expect(
        store.saved.last.announcementFrequency,
        GuiAnnouncementFrequency.everyConnection,
      );
      expect(store.saved.last.diagnosticsExpanded, isTrue);
      store.fail = true;
      prefs.notificationMode = GuiNotificationMode.abnormal;
      await prefs.settled;
      expect(prefs.notificationMode, GuiNotificationMode.abnormal);
      expect(prefs.feedback, contains('未能保存'));
      prefs.dispose();
    },
  );
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
      final prefs = Appearance(store: store);
      await prefs.initialize();
      await c.start();
      try {
        await tester.pumpWidget(
          AppearanceScope(
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
