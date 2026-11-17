import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  for (final platform in [TargetPlatform.linux, TargetPlatform.windows]) {
    testWidgets('ordinary text inherits platform typography on $platform', (
      tester,
    ) async {
      final expected = ThemeData().textTheme.bodyMedium!;
      for (final theme in [AppTheme.light(), AppTheme.dark()]) {
        expect(theme.textTheme.bodyMedium!.fontFamily, expected.fontFamily);
        expect(
          theme.textTheme.bodyMedium!.fontFamilyFallback,
          expected.fontFamilyFallback,
        );
        await tester.pumpWidget(
          MaterialApp(
            theme: theme,
            home: Scaffold(
              body: Column(
                children: [
                  FilledButton(onPressed: () {}, child: const Text('保存')),
                  OutlinedButton(onPressed: () {}, child: const Text('取消')),
                  TextButton(onPressed: () {}, child: const Text('返回')),
                ],
              ),
            ),
          ),
        );
        for (final label in ['保存', '取消', '返回']) {
          final text = tester.widget<Text>(find.text(label));
          final style = DefaultTextStyle.of(tester.element(find.text(label)))
              .style
              .merge(text.style);
          expect(style.fontFamily, expected.fontFamily);
          expect(
            style.fontWeight,
            label == '返回' ? FontWeight.w500 : FontWeight.w600,
          );
        }
      }
    }, variant: TargetPlatformVariant({platform}));
  }
}
