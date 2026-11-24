import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

void main() {
  for (final scale in [1.0, 2.0]) {
    testWidgets('long selection keeps return reachable at text scale $scale', (
      tester,
    ) async {
      tester.view.physicalSize = const Size(400, 600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      String? selected = 'unchanged';
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light().copyWith(platform: TargetPlatform.windows),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context)
                .copyWith(textScaler: TextScaler.linear(scale)),
            child: child!,
          ),
          home: Scaffold(
            body: Builder(
              builder: (context) => TextButton(
                onPressed: () async {
                  selected = await showDesignChoice<String>(
                    context,
                    title: '机构',
                    value: '0',
                    options: [for (var n = 0; n < 50; n++) ('$n', '选项 $n')],
                  );
                },
                child: const Text('打开'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('打开'));
      await tester.pumpAndSettle();
      final back = find.byKey(const ValueKey('design-back-button'));
      final before = tester.getRect(back);
      await tester.scrollUntilVisible(
        find.text('选项 49'),
        250,
        scrollable: find
            .descendant(
              of: find.byType(Dialog),
              matching: find.byType(Scrollable),
            )
            .last,
      );
      expect(tester.getRect(back), before);
      await tester.tap(back);
      await tester.pumpAndSettle();
      expect(selected, isNull);
      expect(find.byType(Dialog), findsNothing);
      expect(tester.takeException(), isNull);
    });
  }
  for (final accepted in [false, true]) {
    testWidgets('confirmation preserves caller outcome $accepted', (
      tester,
    ) async {
      bool? result;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) => TextButton(
                onPressed: () async {
                  result = await showDialog<bool>(
                    context: context,
                    builder: (context) => DesignConfirmationDialog(
                      title: const Text('确认操作'),
                      content: const Text('调用方提供的说明'),
                      actions: [
                        TextButton(
                          onPressed: () => Navigator.pop(context, false),
                          child: const Text('取消'),
                        ),
                        TextButton(
                          onPressed: () => Navigator.pop(context, true),
                          child: const Text('确认'),
                        ),
                      ],
                    ),
                  );
                },
                child: const Text('打开'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('打开'));
      await tester.pumpAndSettle();
      await tester.tap(find.text(accepted ? '确认' : '取消'));
      await tester.pumpAndSettle();
      expect(result, accepted);
      expect(find.byType(Dialog), findsNothing);
    });
  }
}
