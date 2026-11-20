import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/dev/interactive_demo_main.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  for (final systemLocale in [
    const Locale('en', 'US'),
    const Locale('zh', 'TW'),
  ]) {
    testWidgets(
      'Chinese UI keeps Mainland glyph locale on $systemLocale',
      (tester) async {
        tester.binding.platformDispatcher.localeTestValue = systemLocale;
        addTearDown(tester.binding.platformDispatcher.clearLocaleTestValue);
        await tester.pumpWidget(const OfflineDemoApp());
        await tester.pumpAndSettle();
        await tester.tap(find.byTooltip('设置'));
        await tester.pumpAndSettle();

        for (final label in ['自动重连', '自动显示公告', '外观模式']) {
          final text = find.text(label);
          final context = tester.element(text);
          expect(
            Localizations.localeOf(context),
            const Locale.fromSubtags(
              languageCode: 'zh',
              scriptCode: 'Hans',
              countryCode: 'CN',
            ),
          );
          expect(MaterialLocalizations.of(context).copyButtonLabel, '复制');
          final paragraph = tester.renderObject<RenderParagraph>(
            find.descendant(of: text, matching: find.byType(RichText)),
          );
          expect(paragraph.locale, Localizations.localeOf(context));
          expect(paragraph.text.style!.fontWeight, FontWeight.w500);
          expect(
            paragraph.text.style!.fontFamily,
            AppTheme.light().textTheme.bodyMedium!.fontFamily,
          );
          expect(
            paragraph.text.style!.fontFamilyFallback,
            AppTheme.light().textTheme.bodyMedium!.fontFamilyFallback,
          );
        }
        await tester.pumpWidget(const SizedBox.shrink());
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull);
      },
      variant: const TargetPlatformVariant({
        TargetPlatform.windows,
        TargetPlatform.linux,
      }),
    );
  }
}
