import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
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
}
