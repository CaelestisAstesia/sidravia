import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  for (final platform in [TargetPlatform.windows, TargetPlatform.android]) {
    for (final scale in [1.0, 2.0]) {
      for (final dark in [false, true]) {
        testWidgets('appearance centered $platform scale=$scale dark=$dark', (
          tester,
        ) async {
          await tester.binding.setSurfaceSize(const Size(360, 640));
          addTearDown(() => tester.binding.setSurfaceSize(null));
          final mode = ValueNotifier(ThemeMode.system);
          addTearDown(mode.dispose);
          await tester.pumpWidget(
            MaterialApp(
              theme: (dark ? AppTheme.dark() : AppTheme.light()).copyWith(
                platform: platform,
              ),
              home: MediaQuery(
                data: MediaQueryData(
                  size: const Size(360, 640),
                  textScaler: TextScaler.linear(scale),
                ),
                child: Scaffold(
                  body: Center(
                    child: ValueListenableBuilder<ThemeMode>(
                      valueListenable: mode,
                      builder: (_, value, _) => AppearanceSelector(
                        value: value,
                        onChanged: (v) => mode.value = v,
                      ),
                    ),
                  ),
                ),
              ),
            ),
          );
          await tester.pumpAndSettle();
          final start = tester.getSize(
            find.byKey(const ValueKey('appearance-selector')),
          );
          for (final value in ThemeMode.values) {
            mode.value = value;
            await tester.pumpAndSettle();
            final region = find.byKey(
              ValueKey('appearance-text-region-${value.name}'),
            );
            final text = find.descendant(
              of: region,
              matching: find.byType(Text),
            );
            expect(
              tester.getRect(text).center.dx,
              closeTo(tester.getRect(region).center.dx, .01),
            );
            expect(
              tester.getRect(text).center.dy,
              closeTo(tester.getRect(region).center.dy, .01),
            );
            expect(
              tester.getSize(find.byKey(const ValueKey('appearance-selector'))),
              start,
            );
            expect(tester.takeException(), isNull);
          }
          expect(
            start.height,
            greaterThanOrEqualTo(platform == TargetPlatform.android ? 48 : 30),
          );
          await tester.tap(find.byType(DropdownButton<ThemeMode>));
          await tester.pumpAndSettle();
          await tester.tap(find.text('浅色').last);
          await tester.pumpAndSettle();
          expect(mode.value, ThemeMode.light);
        });
      }
    }
  }
}
