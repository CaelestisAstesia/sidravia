import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';

void main() {
  testWidgets('advanced page explains unavailable details before bootstrap', (
    tester,
  ) async {
    final controller = GuiController(
      bootstrapper: const UnsupportedGuiBootstrapper(),
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        theme: SidraviaTheme.light(),
        home: AdvancedPage(controller: controller),
      ),
    );

    expect(find.text('高级'), findsOneWidget);
    expect(find.text('正在连接本机服务…'), findsOneWidget);
  });
}
