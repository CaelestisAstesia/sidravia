import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';

void main() {
  testWidgets('HTML MVP uses a single surface without the legacy sidebar', (
    tester,
  ) async {
    final controller = GuiController(
      bootstrapper: const UnsupportedGuiBootstrapper(),
    );
    await tester.pumpWidget(
      MaterialApp(home: SidraviaShell(controller: controller)),
    );
    expect(find.text('正在连接服务'), findsOneWidget);
    expect(find.text('尚未配置'), findsNothing);
    expect(find.byTooltip('设置'), findsOneWidget);
    expect(find.byKey(const ValueKey<String>('wide-sidebar')), findsNothing);
    controller.dispose();
  });
  testWidgets('wide shell preserves navigation and bounded announcements', (
    tester,
  ) async {
    final controller = GuiController(
      bootstrapper: const UnsupportedGuiBootstrapper(),
    );
    final announcements = AnnouncementController(
      endpoint: Uri.parse('https://example.test/feed'),
      store: MemoryAnnouncementStore(),
    );
    addTearDown(controller.dispose);
    addTearDown(announcements.dispose);
    addTearDown(() => tester.binding.setSurfaceSize(null));
    for (final width in [900.0, 1440.0]) {
      await tester.binding.setSurfaceSize(Size(width, 900));
      await tester.pumpWidget(
        MaterialApp(
          home: SidraviaShell(
            controller: controller,
            announcements: announcements,
          ),
        ),
      );
      await tester.pump();
      expect(find.byKey(const ValueKey('desktop-header')), findsOneWidget);
      expect(
        tester
            .getSize(find.byKey(const ValueKey('desktop-announcement-entry')))
            .width,
        180,
      );
      expect(
        tester.getSize(find.byKey(const ValueKey('desktop-header'))).height,
        72,
      );
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('desktop-announcement-entry')),
          matching: find.widgetWithText(TextButton, '公告'),
        ),
        findsOneWidget,
      );
      final header = find.byKey(const ValueKey('desktop-header'));
      await tester.tap(find.descendant(of: header, matching: find.text('设置')));
      await tester.pump();
      expect(find.byType(SettingsPage), findsOneWidget);
      await tester.tap(find.descendant(of: header, matching: find.text('连接')));
      await tester.pump();
      expect(find.text('正在连接服务'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    }
    await tester.binding.setSurfaceSize(const Size(400, 688));
    await tester.pumpWidget(
      MaterialApp(home: SidraviaShell(controller: controller)),
    );
    expect(find.byKey(const ValueKey('desktop-header')), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
