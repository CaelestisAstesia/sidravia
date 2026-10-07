import 'package:flutter/material.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  for (final platform in [
    TargetPlatform.windows,
    TargetPlatform.android,
    TargetPlatform.iOS,
  ]) {
    for (final scale in [1.0, 2.0]) {
      testWidgets('header full target $platform text scale $scale', (
        tester,
      ) async {
        await tester.binding.setSurfaceSize(const Size(398, 642));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        var configurationClicks = 0;
        var settingsClicks = 0;
        final fixture = HomeViewData(
          institution: '吉林大学',
          username: 'student01',
          state: '已连接',
          detail: '认证成功',
          context: '',
          glyph: '✓',
          tone: HomeTone.success,
          secondaryLabel: '连接详情',
          primaryLabel: '断开连接',
          primaryKind: HomeButtonKind.secondary,
          primaryEnabled: true,
          onHeader: () {},
          onSettings: () {},
          onSecondary: () {},
          onPrimary: () {},
        );
        await tester.pumpWidget(
          MaterialApp(
            theme: AppTheme.light().copyWith(platform: platform),
            home: MediaQuery(
              data: MediaQueryData(textScaler: TextScaler.linear(scale)),
              child: Scaffold(
                body: HomeView(
                  data: HomeViewData(
                    institution: fixture.institution,
                    username: fixture.username,
                    state: fixture.state,
                    detail: fixture.detail,
                    context: fixture.context,
                    glyph: fixture.glyph,
                    tone: fixture.tone,
                    secondaryLabel: fixture.secondaryLabel,
                    primaryLabel: fixture.primaryLabel,
                    primaryKind: fixture.primaryKind,
                    primaryEnabled: true,
                    onHeader: () => configurationClicks++,
                    onSettings: () => settingsClicks++,
                    onSecondary: () {},
                    onPrimary: () {},
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        final configuration = find.byKey(
          const ValueKey('home-configuration-button'),
        );
        final settings = find.byKey(const ValueKey('home-settings-button'));
        final left = tester.getRect(configuration);
        final right = tester.getRect(settings);
        expect(left.top, right.top);
        expect(left.bottom, right.bottom);
        final minimum = platform == TargetPlatform.windows ? 42.0 : 48.0;
        expect(left.height, greaterThanOrEqualTo(minimum));
        if (scale == 1) expect(left.height, minimum);
        expect(right.width, greaterThanOrEqualTo(minimum));
        await tester.tapAt(Offset(left.center.dx, left.top + 1));
        await tester.tapAt(Offset(left.center.dx, left.bottom - 1));
        await tester.tapAt(Offset(right.center.dx, right.top + 1));
        expect(configurationClicks, 2);
        expect(settingsClicks, 1);
        final mouse = await tester.createGesture(kind: PointerDeviceKind.mouse);
        await mouse.addPointer(location: Offset(left.center.dx, left.top + 1));
        await tester.pumpAndSettle();
        final inkFinder = find.descendant(
          of: configuration,
          matching: find.byType(InkWell),
        );
        final ink = tester.widget<InkWell>(inkFinder);
        expect(ink.statesController!.value, contains(WidgetState.hovered));
        expect(tester.getRect(inkFinder), left);
        await mouse.removePointer();
        expect(tester.takeException(), isNull);
      });
    }
  }
}
