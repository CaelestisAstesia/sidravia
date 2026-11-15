import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/widgets/design_search_field.dart';

void main() {
  for (final platform in [
    TargetPlatform.linux,
    TargetPlatform.android,
    TargetPlatform.iOS,
  ]) {
    for (final scale in [1.0, 2.0]) {
      testWidgets(
        'search and clear retain size and touch area $platform/$scale',
        (tester) async {
          final controller = TextEditingController();
          String? query;
          await tester.binding.setSurfaceSize(const Size(400, 690));
          addTearDown(() => tester.binding.setSurfaceSize(null));
          await tester.pumpWidget(
            MaterialApp(
              theme: AppTheme.dark(),
              home: Scaffold(
                body: MediaQuery(
                  data: MediaQueryData(
                    size: const Size(400, 690),
                    textScaler: TextScaler.linear(scale),
                  ),
                  child: DesignSearchField(
                    controller: controller,
                    hint: '查找组件',
                    onChanged: (value) => query = value,
                  ),
                ),
              ),
            ),
          );
          final field = find.byType(TextField);
          final before = tester.getSize(field);
          final minimum = platform == TargetPlatform.linux ? 42.0 : 48.0;
          expect(before.height, greaterThanOrEqualTo(minimum));
          await tester.enterText(field, 'component');
          await tester.pumpAndSettle();
          expect(query, 'component');
          expect(tester.getSize(field), before);
          final clear = find.widgetWithIcon(IconButton, Icons.close);
          expect(tester.getSize(clear).height, greaterThanOrEqualTo(minimum));
          await tester.tap(clear);
          await tester.pumpAndSettle();
          expect(query, '');
          expect(controller.text, isEmpty);
          expect(tester.getSize(field), before);
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox.shrink());
          controller.dispose();
        },
        variant: TargetPlatformVariant({platform}),
      );
    }
  }
}
