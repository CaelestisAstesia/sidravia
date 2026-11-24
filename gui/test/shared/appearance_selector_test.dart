import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

import '../support/capture_fonts.dart' as fonts;

void main() {
  testWidgets('appearance dropdown Tab keyboard activation and selection', (
    tester,
  ) async {
    final mode = ValueNotifier(ThemeMode.system);
    addTearDown(mode.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ValueListenableBuilder<ThemeMode>(
            valueListenable: mode,
            builder: (_, value, _) => Center(
              child: AppearanceSelector(
                value: value,
                onChanged: (value) => mode.value = value,
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.sendKeyEvent(LogicalKeyboardKey.tab);
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pumpAndSettle();
    expect(find.byType(MenuItemButton), findsWidgets);
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pumpAndSettle();
    expect(mode.value, ThemeMode.light);
  });

  for (final platform in [
    TargetPlatform.windows,
    TargetPlatform.android,
    TargetPlatform.iOS,
  ]) {
    for (final scale in [1.0, 2.0]) {
      for (final dark in [false, true]) {
        testWidgets(
          'appearance left aligned $platform scale=$scale dark=$dark',
          (tester) async {
            await tester.runAsync(fonts.loadCaptureFonts);
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
              final frame = tester.getRect(
                find.byKey(const ValueKey('appearance-selector')),
              );
              expect(
                tester.getRect(region).center.dy,
                closeTo(frame.center.dy, .01),
                reason:
                    'selected region must center within the full outer frame',
              );
              final arrow = find.descendant(
                of: find.byKey(const ValueKey('appearance-selector')),
                matching: find.byIcon(Icons.arrow_drop_down),
              );
              expect(
                tester.getRect(arrow).center.dy,
                closeTo(frame.center.dy, .01),
              );
              expect(tester.widget<Text>(region).textAlign, TextAlign.start);
              final arrowSlot = tester.getRect(
                find.descendant(
                  of: find.byKey(const ValueKey('appearance-selector')),
                  matching: find.byKey(
                    const ValueKey('settings-dropdown-arrow'),
                  ),
                ),
              );
              final labelInset = tester.getRect(region).left - frame.left;
              expect(labelInset, closeTo(12, .01));
              expect(labelInset, closeTo(frame.right - arrowSlot.right, .01));
              expect(
                tester.getSize(
                  find.byKey(const ValueKey('appearance-selector')),
                ),
                start,
              );
              expect(tester.takeException(), isNull);
            }
            expect(
              start.height,
              greaterThanOrEqualTo(
                platform == TargetPlatform.windows ? 30 : 48,
              ),
            );
            await tester.tap(find.byKey(const ValueKey('appearance-selector')));
            await tester.pumpAndSettle();
            await tester.tap(find.text('浅色').last);
            await tester.pumpAndSettle();
            expect(mode.value, ThemeMode.light);
          },
        );
      }
    }
  }
}
