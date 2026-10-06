import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  test(
    'late startup read never overwrites user selection; saves serialize',
    () async {
      final store = ControlledStore();
      final appearance = Appearance(store: store);
      appearance.value = ThemeMode.dark;
      appearance.value = ThemeMode.light;
      appearance.value = ThemeMode.system;
      store.readResult.complete(GuiAppearanceMode.light);
      await appearance.settled;
      expect(appearance.value, ThemeMode.system);
      expect(store.saved, [
        GuiAppearanceMode.dark,
        GuiAppearanceMode.light,
        GuiAppearanceMode.system,
      ]);
      expect(store.maximumActive, 1);
      appearance.dispose();
    },
  );
  test('failed load blocks overwrite but user theme applies', () async {
    final store = ControlledStore();
    final appearance = Appearance(store: store);
    final loading = appearance.initialize();
    store.readResult.completeError(const FormatException());
    await loading;
    appearance.value = ThemeMode.dark;
    await appearance.settled;
    expect(appearance.value, ThemeMode.dark);
    expect(store.saved, isEmpty);
    expect(appearance.feedback, contains('原文件保留'));
    appearance.dispose();
  });
  test(
    'save failure does not report success or revert current theme',
    () async {
      final store = ControlledStore()..failSave = true;
      store.readResult.complete(null);
      final appearance = Appearance(store: store);
      await appearance.initialize();
      appearance.value = ThemeMode.dark;
      await appearance.settled;
      expect(appearance.value, ThemeMode.dark);
      expect(appearance.feedback, contains('未能保存'));
      appearance.dispose();
    },
  );

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

class ControlledStore implements GuiSettingsStore {
  final readResult = Completer<GuiAppearanceMode?>();
  final saved = <GuiAppearanceMode>[];
  int active = 0, maximumActive = 0;
  bool failSave = false;
  @override
  bool get persistent => true;
  @override
  Future<GuiAppearanceMode?> read() => readResult.future;
  @override
  Future<void> save(GuiAppearanceMode mode) async {
    active++;
    if (active > maximumActive) maximumActive = active;
    await Future<void>.delayed(Duration.zero);
    active--;
    if (failSave) throw StateError('fixture');
    saved.add(mode);
  }
}
