import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';

void main() {
  testWidgets('diagnostics show unavailable facts without inventing values', (
    tester,
  ) async {
    final controller = GuiController(
      bootstrapper: const UnsupportedGuiBootstrapper(),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: AdvancedPage(
          controller: controller,
          detailsOnly: false,
          onBack: () {},
        ),
      ),
    );
    expect(find.text('暂不可用'), findsAtLeastNWidgets(1));
    expect(find.text('复制诊断信息（暂不可用）'), findsOneWidget);
    controller.dispose();
  });
}
