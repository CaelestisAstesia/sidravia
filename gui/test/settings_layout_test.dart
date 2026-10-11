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
      expect(closeBox.right, appearanceBox.right);
      expect(find.text('每天只显示一次。'), findsNothing);
      for (final label in ['每天首次连接成功后', '发送系统通知点击查看']) {
        expect(
          tester.getRect(find.text(label).first).right,
          closeTo(width - 24 - 1 - 12 - 18, .01),
        );
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
