import 'package:sidravia_gui/features/settings/settings_dropdown.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/application/preferences/gui_preferences.dart';
import 'package:sidravia_gui/application/preferences/gui_settings.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

import 'support/offline_gui_controller.dart';

void main() {
  for (final scale in [1.0, 2.0]) {
    testWidgets('expanded menu item rectangles fit trigger at scale $scale', (
      tester,
    ) async {
      String selected = 'one';
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.dark().copyWith(platform: TargetPlatform.windows),
          home: Scaffold(
            body: MediaQuery(
              data: MediaQueryData(
                size: const Size(400, 800),
                textScaler: TextScaler.linear(scale),
              ),
              child: Center(
                child: SettingsDropdown<String>(
                  frameKey: const ValueKey('frame'),
                  dropdownKey: const ValueKey('trigger'),
                  value: selected,
                  labels: const {'one': '隐藏到托盘', 'two': '退出程序'},
                  onChanged: (v) => selected = v,
                ),
              ),
            ),
          ),
        ),
      );
      final frame = tester.getRect(find.byKey(const ValueKey('frame')));
      final label = find.text('隐藏到托盘');
      final arrow = tester.getRect(
        find.descendant(
          of: find.byKey(const ValueKey('trigger')),
          matching: find.byKey(const ValueKey('settings-dropdown-arrow')),
        ),
      );
      expect(
        tester.getRect(label).left - frame.left,
        closeTo(frame.right - arrow.right, .01),
      );
      expect(tester.widget<Text>(label).textAlign, TextAlign.start);
      await tester.tap(find.byKey(const ValueKey('trigger')));
      await tester.pumpAndSettle();
      for (final item in find.byType(MenuItemButton).evaluate()) {
        final rect = tester.getRect(find.byWidget(item.widget));
        expect(rect.width, lessThanOrEqualTo(frame.width + .01));
        expect(rect.height, lessThanOrEqualTo(frame.height + .01));
      }
      await tester.tap(find.text('退出程序'));
      await tester.pumpAndSettle();
      expect(selected, 'two');
      expect(tester.takeException(), isNull);
    });
  }

  for (final width in [360.0, 400.0]) {
    testWidgets(
      'long announcement labels fit common menu and trigger at $width with 2x text',
      (tester) async {
        await tester.binding.setSurfaceSize(Size(width, 800));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        const labels = {'notice': '发送系统通知点击查看', 'daily': '每天首次连接成功后'};
        await tester.pumpWidget(
          MaterialApp(
            theme: AppTheme.light().copyWith(platform: TargetPlatform.windows),
            home: Scaffold(
              body: MediaQuery(
                data: MediaQueryData(
                  size: Size(width, 800),
                  textScaler: const TextScaler.linear(2),
                ),
                child: Center(
                  child: SettingsDropdown<String>(
                    frameKey: const ValueKey('frame'),
                    dropdownKey: const ValueKey('trigger'),
                    value: 'notice',
                    labels: labels,
                    onChanged: (_) {},
                  ),
                ),
              ),
            ),
          ),
        );
        final frame = tester.getRect(find.byKey(const ValueKey('frame')));
        final selected = tester.getRect(find.text(labels['notice']!));
        expect(selected.top, greaterThanOrEqualTo(frame.top));
        expect(selected.bottom, lessThanOrEqualTo(frame.bottom));
        await tester.tap(find.byKey(const ValueKey('trigger')));
        await tester.pumpAndSettle();
        expect(find.byType(MenuItemButton), findsNWidgets(labels.length));
        for (final item in find.byType(MenuItemButton).evaluate()) {
          final finder = find.byWidget(item.widget);
          final rect = tester.getRect(finder);
          expect(rect.width, lessThanOrEqualTo(frame.width + .01));
          expect(rect.height, lessThanOrEqualTo(frame.height + .01));
          final text = tester.getRect(
            find.descendant(of: finder, matching: find.byType(Text)),
          );
          expect(text.top, greaterThanOrEqualTo(rect.top));
          expect(text.bottom, lessThanOrEqualTo(rect.bottom));
          expect(text.left, greaterThanOrEqualTo(rect.left));
          expect(text.right, lessThanOrEqualTo(rect.right));
        }
        expect(tester.takeException(), isNull);
      },
    );
  }

  for (final platform in [TargetPlatform.windows, TargetPlatform.android]) {
    for (final label in ['返回']) {
      testWidgets(
        'back button circular target and centered icon $platform $label',
        (tester) async {
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
          final icon = tester.getRect(
            find.descendant(of: back, matching: find.byType(Icon)),
          );
          expect(button.width, button.height);
          expect(button.width, platform == TargetPlatform.android ? 48 : 42);
          expect(icon.center, button.center);
          final widget = tester.widget<IconButton>(
            find.descendant(of: back, matching: find.byType(IconButton)),
          );
          expect(widget.style!.shape!.resolve({}), isA<CircleBorder>());
          expect(tester.takeException(), isNull);
        },
      );
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
      final controller = offlineGuiController(client);
      await controller.start();
      final appearance = GuiPreferences(
        store: MemoryGuiSettingsStore()
          ..settings = const GuiSettings(logLevel: 'info'),
      );
      await appearance.initialize();
      var backs = 0;
      await tester.pumpWidget(
        GuiPreferencesScope(
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
        lessThan(tester.getTopLeft(find.text('常规')).dy),
      );
      expect(
        tester.getTopLeft(find.text('常规')).dy,
        lessThan(tester.getTopLeft(find.text('启动与窗口行为')).dy),
      );
      expect(
        tester.getTopLeft(find.text('启动与窗口行为')).dy,
        lessThan(tester.getTopLeft(find.text('诊断与更多信息')).dy),
      );
      final back = find.byKey(const ValueKey('design-back-button'));
      final before = tester.getRect(back);
      expect(before.left, (width - width.clamp(0, 720)) / 2 + 24);
      expect(tester.getRect(find.byType(DesignHeader)).center.dx, width / 2);
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
      await tester.drag(find.byType(Scrollable).first, const Offset(0, -900));
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
