import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

void main() {
  for (final platform in [TargetPlatform.windows, TargetPlatform.android]) {
    for (final label in ['返回']) {
      testWidgets('back button equal horizontal gutters $platform $label', (
        tester,
      ) async {
        await tester.pumpWidget(
          MaterialApp(
            theme: AppTheme.light().copyWith(platform: platform),
            home: Scaffold(
              body: DesignHeader(title: '设置', onBack: () {}),
            ),
          ),
        );
        final back = find.byKey(const ValueKey('design-back-button'));
        final button = tester.getRect(back);
        final row = tester.getRect(
          find.descendant(of: back, matching: find.byType(Row)),
        );
        expect(row.left - button.left, 12);
        expect(button.right - row.right, 12);
        expect(tester.takeException(), isNull);
      });
    }
  }

  testWidgets(
    'choice dialog centered title, left back and rounded choice group',
    (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                onPressed: () => showDesignChoice<String>(
                  context,
                  title: '机构',
                  value: 'jlu',
                  options: [('jlu', '吉林大学')],
                ),
                child: const Text('选择机构'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('选择机构'));
      await tester.pumpAndSettle();
      final dialog = tester.getRect(
        find
            .descendant(
              of: find.byType(Dialog),
              matching: find.byType(Material),
            )
            .first,
      );
      final back = tester.getRect(
        find.byKey(const ValueKey('design-back-button')),
      );
      final title = tester.getRect(find.text('机构'));
      final option = tester.getRect(find.text('吉林大学'));
      expect(back.left, dialog.left + 16);
      expect(title.center.dx, dialog.center.dx);
      expect(option.left, dialog.left + 29);
      expect(
        find.ancestor(
          of: find.text('吉林大学'),
          matching: find.byType(DesignGroup),
        ),
        findsOneWidget,
      );
      expect(back.center.dy, closeTo(title.center.dy, 1));
      await tester.tap(find.byKey(const ValueKey('design-back-button')));
      await tester.pumpAndSettle();
      expect(find.byType(Dialog), findsNothing);
    },
  );
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
        tester.getSize(find.byKey(const ValueKey('auto-login-row'))).height,
      );
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('auto-reconnect-row')),
          matching: find.text('连接中断后自动尝试重新认证'),
        ),
        findsOneWidget,
      );
      expect(find.text('修改后从下次连接生效。'), findsNothing);
      expect(find.text('重启核心后生效'), findsOneWidget);
      expect(find.text('下次启动生效；不改变自动登录或当前窗口。'), findsNothing);
      expect(find.text('INFO'), findsOneWidget);
      expect(find.text('普通（info）'), findsNothing);
      expect(closeBox.right, appearanceBox.right);
      expect(find.text('每天只显示一次。'), findsNothing);
      expect(find.text('通知可点击查看公告。'), findsNothing);
      expect(find.text('桌面托盘不可用；关闭与启动隐藏设置暂不可操作。'), findsNothing);
      expect(find.text('离线演示：外观只保存在演示内存中。'), findsNothing);
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
