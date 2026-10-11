import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  for (final width in [360.0, 960.0]) {
    testWidgets('settings pinned back and aligned controls at $width', (
      tester,
    ) async {
      await tester.binding.setSurfaceSize(Size(width, 540));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      final appearance = Appearance();
      await appearance.initialize();
      var backs = 0;
      await tester.pumpWidget(
        AppearanceScope(
          appearance: appearance,
          child: MaterialApp(
            theme: AppTheme.light().copyWith(platform: TargetPlatform.windows),
            home: Scaffold(
              body: SettingsPage(
                controller: controller,
                onNavigate: (_) {},
                onBack: () => backs++,
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(
        tester.getTopLeft(find.text('连接')).dy,
        lessThan(tester.getTopLeft(find.text('公告')).dy),
      );
      expect(
        tester.getTopLeft(find.text('公告')).dy,
        lessThan(tester.getTopLeft(find.text('应用')).dy),
      );
      final back = find.byKey(const ValueKey('design-back-button'));
      final before = tester.getRect(back);
      expect(before.left, 24);
      final appearanceBox = tester.getRect(
        find.byKey(const ValueKey('appearance-selector')),
      );
      final closeBox = tester.getRect(
        find.byKey(const ValueKey('close-behavior-frame')),
      );
      expect(closeBox.height, appearanceBox.height);
      expect(
        tester.getSize(find.byKey(const ValueKey('log-level-row'))).height,
        tester.getSize(find.byKey(const ValueKey('appearance-row'))).height,
      );
      expect(find.text('INFO'), findsOneWidget);
      expect(find.text('普通（info）'), findsNothing);
      expect(closeBox.right, appearanceBox.right);
      expect(find.text('每天只显示一次。'), findsNothing);
      expect(find.text('通知可点击查看公告。'), findsNothing);
      for (final key in [
        'close-behavior-frame',
        'log-level-frame',
        'announcement-自动显示公告-frame',
        'announcement-主窗口隐藏时-frame',
      ]) {
        final frame = tester.getRect(find.byKey(ValueKey(key)));
        expect(frame.width, appearanceBox.width);
        expect(frame.height, appearanceBox.height);
        expect(frame.right, appearanceBox.right);
      }
      await tester.drag(
        find.byType(SingleChildScrollView).first,
        const Offset(0, -900),
      );
      await tester.pumpAndSettle();
      expect(tester.getRect(back), before);
      await tester.tap(back);
      expect(backs, 1);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      await controller.close();
      controller.dispose();
      client.dispose();
      appearance.dispose();
    });
  }
}
