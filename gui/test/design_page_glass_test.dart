import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

void main() {
  Future<void> mount(
    WidgetTester tester, {
    bool dark = false,
    double scale = 1,
    bool highContrast = false,
    bool disableAnimations = false,
    TargetPlatform platform = TargetPlatform.windows,
    bool header = true,
    VoidCallback? onBack,
  }) async {
    tester.view.physicalSize = const Size(400, 600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        theme: (dark ? AppTheme.dark() : AppTheme.light()).copyWith(
          platform: platform,
        ),
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(context).copyWith(
            textScaler: TextScaler.linear(scale),
            highContrast: highContrast,
            disableAnimations: disableAnimations,
          ),
          child: child!,
        ),
        home: Scaffold(
          body: DesignPage(
            children: [
              if (header) DesignHeader(title: '连接与诊断', onBack: onBack ?? () {}),
              for (var i = 0; i < 30; i++)
                SizedBox(height: 56, child: Text('内容 $i')),
            ],
          ),
        ),
      ),
    );
  }

  for (final dark in [false, true]) {
    for (final scale in [1.0, 2.0]) {
      testWidgets('Windows pinned glass dark=$dark scale=$scale', (
        tester,
      ) async {
        var returned = false;
        await mount(
          tester,
          dark: dark,
          scale: scale,
          onBack: () => returned = true,
        );
        final back = find.byKey(const ValueKey('design-back-button'));
        final before = tester.getRect(back);
        expect(
          tester.getTopLeft(find.text('内容 0')).dy,
          greaterThan(before.bottom),
        );
        expect(
          tester.widget<BackdropFilter>(find.byType(BackdropFilter)).enabled,
          false,
        );
        final scroll = tester.state<ScrollableState>(find.byType(Scrollable));
        scroll.position.jumpTo(16);
        await tester.pump();
        final halfway = tester
            .widget<BackdropFilter>(find.byType(BackdropFilter))
            .filter;
        expect(
          tester.widget<BackdropFilter>(find.byType(BackdropFilter)).enabled,
          true,
        );
        scroll.position.jumpTo(300);
        await tester.pump();
        expect(
          tester.widget<BackdropFilter>(find.byType(BackdropFilter)).filter,
          isNot(halfway),
        );
        expect(tester.getRect(back), before);
        await tester.tap(back);
        expect(returned, true);
        scroll.position.jumpTo(0);
        await tester.pump();
        expect(
          tester.widget<BackdropFilter>(find.byType(BackdropFilter)).enabled,
          false,
        );
        expect(tester.takeException(), isNull);
      });
    }
  }
  for (final contrast in [false, true]) {
    testWidgets('solid accessible header contrast=$contrast', (tester) async {
      await mount(tester, highContrast: contrast, disableAnimations: !contrast);
      await tester.drag(find.byType(CustomScrollView), const Offset(0, -300));
      await tester.pumpAndSettle();
      expect(find.byType(BackdropFilter), findsNothing);
      expect(find.text('返回'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  }
  testWidgets('Android and headerless pages retain ordinary layout', (
    tester,
  ) async {
    await mount(tester, platform: TargetPlatform.android);
    expect(find.byType(CustomScrollView), findsNothing);
    expect(find.byType(SingleChildScrollView), findsOneWidget);
    await mount(tester, header: false);
    expect(find.byType(CustomScrollView), findsNothing);
    expect(find.byType(BackdropFilter), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
