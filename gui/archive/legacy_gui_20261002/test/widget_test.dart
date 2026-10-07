import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/preview_fixtures.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/window/sidravia_window_frame.dart';

void main() {
  test('compact text scaling uses two semantic limits', () {
    expect(SidraviaLayout.compactChromeMaxTextScale, 1.3);
    expect(SidraviaLayout.compactContentMaxTextScale, 1.6);
  });

  testWidgets('wide shell exposes the four-section navigation', (tester) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = createPreviewController(PreviewScenario.authenticated);
    await controller.start();

    await tester.pumpWidget(_app(controller));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('wide-sidebar')), findsOneWidget);
    expect(find.text('仪表板'), findsWidgets);
    expect(find.text('配置'), findsOneWidget);
    expect(find.text('高级'), findsOneWidget);
    expect(find.text('选项'), findsOneWidget);
    expect(
      find.byKey(const ValueKey('connection-status-surface')),
      findsOneWidget,
    );
    expect(find.text('已连接'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('destination-advanced')));
    await tester.pumpAndSettle();
    expect(find.text('认证协议'), findsOneWidget);
    expect(find.text('本机服务'), findsOneWidget);
    controller.dispose();
  });

  testWidgets('compact shell uses the translucent capsule navigation', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = createPreviewController(PreviewScenario.noConfiguration);
    await controller.start();

    await tester.pumpWidget(_app(controller));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('wide-sidebar')), findsNothing);
    expect(find.byKey(const ValueKey('compact-header')), findsOneWidget);
    expect(find.text('仪表板'), findsWidgets);
    expect(find.text('选项'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('destination-options')));
    await tester.pumpAndSettle();
    expect(find.text('主题模式'), findsOneWidget);
    expect(find.text('跟随系统设置'), findsOneWidget);
    controller.dispose();
  });

  testWidgets('dashboard uses real snapshot state and connection wording', (
    tester,
  ) async {
    final controller = createPreviewController(PreviewScenario.disconnected);
    await controller.start();

    await tester.pumpWidget(
      MaterialApp(
        theme: SidraviaTheme.light(),
        home: HomePage(controller: controller, onOpenConfiguration: () {}),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('未连接'), findsOneWidget);
    expect(find.text('开始连接'), findsOneWidget);
    expect(find.text('退出校园网'), findsNothing);
    controller.dispose();
  });

  testWidgets('Windows frame exposes direct minimize and close controls', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: SidraviaWindowFrame(
          platform: TargetPlatform.windows,
          child: ColoredBox(color: Colors.white),
        ),
      ),
    );

    expect(find.byTooltip('最小化'), findsOneWidget);
    expect(find.byTooltip('关闭'), findsOneWidget);
  });
}

Widget _app(GuiController controller) => MaterialApp(
  title: 'Sidravia',
  theme: SidraviaTheme.light(),
  home: SidraviaShell(controller: controller),
);
