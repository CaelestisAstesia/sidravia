import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/dev/preview_main.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

const output = String.fromEnvironment('HOME_RESPONSIVE_OUTPUT');
const sizes = {
  'reference': Size(398, 642),
  'compact': Size(398, 572),
  'tall': Size(398, 732),
  'wide': Size(758, 542),
  'intermediate': Size(518, 602),
  'before-wide': Size(757, 642),
  'after-wide': Size(759, 642),
  'large': Size(918, 732),
  'short': Size(398, 280),
  'narrow': Size(298, 542),
};

Future<void> loadCaptureFonts() async {
  await (FontLoader('HarmonyOS Sans')
        ..addFont(rootBundle.load('assets/fonts/HarmonyOS_Sans_SC_Regular.ttf'))
        ..addFont(rootBundle.load('assets/fonts/HarmonyOS_Sans_SC_Medium.ttf'))
        ..addFont(rootBundle.load('assets/fonts/HarmonyOS_Sans_SC_Bold.ttf')))
      .load();
  await (FontLoader(
    'MaterialIcons',
  )..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'))).load();
  const path = String.fromEnvironment(
    'HOME_SYMBOL_FONT',
    defaultValue: '/mnt/c/Windows/Fonts/seguisym.ttf',
  );
  if (await File(path).exists()) {
    await (FontLoader(
      'Segoe UI Symbol',
    )..addFont(File(path).readAsBytes().then(ByteData.sublistView))).load();
  } else if (output.isNotEmpty) {
    throw StateError('HOME_SYMBOL_FONT is required for candidate capture');
  }
}

Widget surface(Widget child) => MaterialApp(
  debugShowCheckedModeBanner: false,
  theme: AppTheme.light(),
  home: Scaffold(
    body: SizedBox.expand(
      child: RepaintBoundary(
        key: const ValueKey('responsive-capture'),
        child: ColoredBox(color: AppColors.bgLight, child: child),
      ),
    ),
  ),
);

Map<String, double> rectMap(Rect r) => {
  'x': r.left,
  'y': r.top,
  'width': r.width,
  'height': r.height,
};

Future<void> capture(WidgetTester tester, String name) async {
  if (output.isEmpty) return;
  final elements = <String, Object>{};
  for (final key in [
    'home-page',
    'home-header',
    'home-status',
    'home-mark',
    'home-context',
    'home-actions',
    'home-divider',
    'home-notice',
  ]) {
    elements[key] = rectMap(tester.getRect(find.byKey(ValueKey(key))));
  }
  final position = tester
      .state<ScrollableState>(find.byType(Scrollable).first)
      .position;
  elements['scrollExtent'] = position.maxScrollExtent;
  final boundary = tester.renderObject<RenderRepaintBoundary>(
    find.byKey(const ValueKey('responsive-capture')),
  );
  await tester.runAsync(() async {
    final dir = Directory('$output/$name');
    await dir.create(recursive: true);
    final image = await boundary.toImage(pixelRatio: 1);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    await File('${dir.path}/flutter-content.png')
        .writeAsBytes(bytes!.buffer.asUint8List());
    await File('${dir.path}/flutter-measurements.json')
        .writeAsString(jsonEncode(elements));
    image.dispose();
  });
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  for (final entry in sizes.entries) {
    testWidgets('home follows HTML width/padding and flow at ${entry.key}', (
      tester,
    ) async {
      tester.view.devicePixelRatio = 1;
      tester.platformDispatcher.textScaleFactorTestValue = 1;
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
      await tester.binding.setSurfaceSize(entry.value);
      addTearDown(() => tester.binding.setSurfaceSize(null));
      await tester.runAsync(loadCaptureFonts);
      await tester.pumpWidget(surface(const HomeFixture()));
      await tester.pumpAndSettle();
      final width = entry.value.width;
      final pageWidth = width.clamp(0, 620).toDouble();
      final wide = width >= 758;
      final pad = wide ? 36.0 : 24.0;
      final header = tester.getRect(find.byKey(const ValueKey('home-header')));
      expect(header.left, (width - pageWidth) / 2 + pad);
      expect(header.top, wide ? 16 : 12);
      expect(header.width, pageWidth - 2 * pad);
      expect(
        tester.getSize(find.byKey(const ValueKey('home-mark'))),
        const Size(44, 44),
      );
      expect(
        tester.getSize(find.byKey(const ValueKey('home-actions'))).width,
        (pageWidth - 2 * pad).clamp(0, 320),
      );
      final notice = tester.getRect(find.byKey(const ValueKey('home-notice')));
      expect(notice.left, header.left);
      // Announcement stays in content flow even when the window is tall.
      expect(notice.top, wide ? 451 : 447);
      final position = tester
          .state<ScrollableState>(find.byType(Scrollable).first)
          .position;
      if (entry.key == 'short') {
        expect(position.maxScrollExtent, greaterThan(0));
        await capture(tester, entry.key);
        await tester.ensureVisible(find.byType(HomeNoticeView));
        await tester.pumpAndSettle();
        expect(
          tester.getRect(find.byType(HomeNoticeView)).bottom,
          lessThanOrEqualTo(entry.value.height),
        );
      } else {
        await capture(tester, entry.key);
      }
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets(
    'continuous widths preserve element scale and constrain the page',
    (tester) async {
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(() => tester.binding.setSurfaceSize(null));
      await tester.pumpWidget(surface(const HomeFixture()));
      for (double width = 320; width <= 1000; width += 10) {
        await tester.binding.setSurfaceSize(
          Size(width, 420 + (width - 320) / 2),
        );
        await tester.pump();
        expect(
          tester.getSize(find.byKey(const ValueKey('home-page'))).width,
          width.clamp(0, 620),
        );
        expect(
          tester.getSize(find.byKey(const ValueKey('home-mark'))),
          const Size(44, 44),
        );
        expect(tester.takeException(), isNull);
      }
    },
  );

  testWidgets(
    'long content grows status and context; short viewport scrolls to the announcement',
    (tester) async {
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.binding.setSurfaceSize(const Size(398, 280));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      var opened = false;
      final data = HomeViewData(
        institution: '吉林大学某个很长的校区名称与网络接入机构名称',
        username: 'student-with-a-very-long-account-name',
        state: '已连接',
        detail: List.filled(12, '认证成功，网络接入信息较长').join('；'),
        context: List.filled(16, '以太网适配器及地址的详细说明').join('；'),
        glyph: '✓',
        tone: HomeTone.success,
        secondaryLabel: '连接详情',
        primaryLabel: '断开连接',
        primaryKind: HomeButtonKind.secondary,
        primaryEnabled: true,
        onHeader: () => opened = true,
        onSettings: () => opened = true,
        onSecondary: () => opened = true,
        onPrimary: () => opened = true,
        notice: HomeNotice(
          title: List.filled(8, '校园网维护安排与相关说明').join('，'),
          onOpen: () => opened = true,
        ),
      );
      await tester.runAsync(loadCaptureFonts);
      await tester.pumpWidget(surface(HomeView(data: data)));
      await tester.pumpAndSettle();
      expect(
        tester.getSize(find.byKey(const ValueKey('home-context'))).height,
        greaterThan(92),
      );
      expect(
        tester.getSize(find.byKey(const ValueKey('home-status'))).height,
        greaterThan(258),
      );
      expect(tester.takeException(), isNull);
      await capture(tester, 'long');
      await tester.ensureVisible(find.byType(HomeNoticeView));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(HomeNoticeView));
      expect(opened, isTrue);
      expect(tester.takeException(), isNull);
    },
  );
}
