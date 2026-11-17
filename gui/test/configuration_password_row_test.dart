import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/configuration/configuration_password_row.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  for (final width in [360.0, 960.0]) {
    for (final scale in [1.0, 2.0]) {
      testWidgets(
        'password edit cancel and disabled conceal at $width/$scale',
        (tester) async {
          await tester.binding.setSurfaceSize(Size(width, 800));
          addTearDown(() => tester.binding.setSurfaceSize(null));
          final semantics = tester.ensureSemantics();
          try {
            final controller = TextEditingController();
            addTearDown(controller.dispose);
            var enabled = true;
            var cancelled = 0;
            late StateSetter update;
            await tester.pumpWidget(
              MaterialApp(
                theme: AppTheme.light(),
                home: StatefulBuilder(
                  builder: (context, setState) {
                    update = setState;
                    return MediaQuery(
                      data: MediaQuery.of(context)
                          .copyWith(textScaler: TextScaler.linear(scale)),
                      child: Scaffold(
                        body: SingleChildScrollView(
                          child: ConfigurationPasswordRow(
                            controller: controller,
                            credentialStored: true,
                            enabled: enabled,
                            onCancel: () => cancelled++,
                          ),
                        ),
                      ),
                    );
                  },
                ),
              ),
            );
            expect(find.byType(TextField), findsNothing);
            expect(find.text('已保存密码'), findsOneWidget);
            await tester.tap(find.text('更改'));
            await tester.pumpAndSettle();
            final field = find.byType(TextField);
            expect(tester.widget<TextField>(field).obscureText, isTrue);
            await tester.enterText(field, 'new-fixture-only');
            expect(
              tester
                  .getSemantics(find.byTooltip('显示密码'))
                  .getSemanticsData()
                  .flagsCollection
                  .isButton,
              isTrue,
            );
            await tester.tap(find.byTooltip('显示密码'));
            await tester.pumpAndSettle();
            expect(tester.widget<TextField>(field).obscureText, isFalse);
            update(() => enabled = false);
            await tester.pumpAndSettle();
            expect(tester.widget<TextField>(field).obscureText, isTrue);
            expect(tester.widget<TextField>(field).enabled, isFalse);
            expect(controller.text, 'new-fixture-only');
            update(() => enabled = true);
            await tester.pumpAndSettle();
            expect(tester.widget<TextField>(field).obscureText, isTrue);
            await tester.tap(find.text('取消更改'));
            await tester.pumpAndSettle();
            expect(controller.text, isEmpty);
            expect(cancelled, 1);
            expect(find.byType(TextField), findsNothing);
            await tester.tap(find.text('更改'));
            await tester.pumpAndSettle();
            expect(tester.widget<TextField>(field).obscureText, isTrue);
            expect(controller.text, isEmpty);
            expect(tester.takeException(), isNull);
            await tester.pumpWidget(const SizedBox.shrink());
          } finally {
            semantics.dispose();
          }
        },
      );
    }
  }
}
