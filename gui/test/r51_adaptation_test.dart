import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/gestures.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_feed.dart';
import 'package:sidravia_gui/features/announcements/announcement_model.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/dev/preview_main.dart';
import 'package:sidravia_gui/dev/interactive_demo_main.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

import 'home_responsive_test.dart' as fonts;

const output = String.fromEnvironment('R51_OUTPUT');
const baseline = bool.fromEnvironment('R51_BASELINE');
Map<String, double> rect(Rect r) => {
  'x': r.left,
  'y': r.top,
  'w': r.width,
  'h': r.height,
};
void main() {
  testWidgets('measure continuity and developer tool height', (tester) async {
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.runAsync(fonts.loadCaptureFonts);
    final records = <String, Object?>{};
    for (final kind in ['home', 'design']) {
      final rows = <Object?>[];
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light(),
          home: kind == 'home'
              ? const HomeFixture()
              : const DesignPage(
                  children: [
                    SizedBox(key: ValueKey('design-marker'), height: 42),
                  ],
                ),
        ),
      );
      for (final w in [
        ...List.generate(21, (i) => 748 + i),
        ...List.generate(21, (i) => 950 + i),
      ]) {
        await tester.binding.setSurfaceSize(Size(w.toDouble(), 690));
        await tester.pump();
        rows.add({
          'width': w,
          'marker': rect(
            tester.getRect(
              find.byKey(
                ValueKey(kind == 'home' ? 'home-header' : 'design-marker'),
              ),
            ),
          ),
          if (kind == 'home')
            'actions': rect(
              tester.getRect(find.byKey(const ValueKey('home-actions'))),
            ),
        });
      }
      records[kind] = rows;
    }
    await tester.pumpWidget(const OfflineDemoApp());
    await tester.pumpAndSettle();
    final rows = <Object?>[];
    double? previous;
    for (int w = 320; w <= 1100; w++) {
      await tester.binding.setSurfaceSize(Size(w.toDouble(), 690));
      await tester.pump();
      final label = find.text('离线演示 · 勿输入真实账号或密码');
      final material = find
          .ancestor(of: label, matching: find.byType(Material))
          .first;
      final height = tester.getSize(material).height;
      if (previous != height || w >= 748 && w <= 768 || w >= 950 && w <= 970) {
        rows.add({
          'width': w,
          'toolHeight': height,
          'shell': rect(tester.getRect(find.byType(HomePage))),
          'header': rect(
            tester.getRect(find.byKey(const ValueKey('home-header'))),
          ),
        });
      }
      if (!baseline && previous != null) {
        expect(height, closeTo(previous, .00001));
      }
      previous = height;
    }
    records['demo'] = rows;
    records['environment'] = {
      'nativePhysicalWidth': null,
      'nativeDpi': null,
      'widgetDpr': 1,
      'platform': 'Windows widget simulation; native measurements unavailable',
    };
    if (output.isNotEmpty) {
      await tester.runAsync(() async {
        await Directory(output).create(recursive: true);
        await File('$output/${baseline ? 'baseline' : 'updated'}-geometry.json')
            .writeAsString(const JsonEncoder.withIndent('  ').convert(records));
      });
    }
    await tester.pumpWidget(const SizedBox.shrink());
  }, variant: TargetPlatformVariant({TargetPlatform.windows}));
  testWidgets(
    'formal home and other pages are continuous with no scrollbar and keep targets',
    (tester) async {
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light().copyWith(platform: TargetPlatform.windows),
          home: SidraviaShell(controller: controller),
        ),
      );
      for (final page in ['home', 'settings']) {
        if (page == 'settings') {
          await tester.tap(find.byKey(const ValueKey('home-settings-button')));
          await tester.pumpAndSettle();
        }
        Rect? previous;
        int? previousWidth;
        for (final w in [
          ...List.generate(160, (i) => 610 + i),
          ...List.generate(21, (i) => 950 + i),
        ]) {
          await tester.binding.setSurfaceSize(Size(w.toDouble(), 500));
          await tester.pump();
          final r = tester.getRect(
            page == 'home'
                ? find.byKey(const ValueKey('home-header'))
                : find.byType(DesignHeader),
          );
          if (previous != null &&
              previousWidth != null &&
              w == previousWidth + 1) {
            expect((r.top - previous.top).abs(), lessThan(.04));
            expect(
              (r.width - previous.width).abs(),
              lessThan(w <= 620 ? 1.001 : .18),
            );
            expect((r.left - previous.left).abs(), lessThan(.60));
          }
          if (page == 'home') {
            expect(
              tester.getSize(find.byKey(const ValueKey('home-page'))).width,
              lessThanOrEqualTo(620),
            );
            expect(
              tester.getSize(find.byKey(const ValueKey('home-actions'))).width,
              320,
            );
            expect(
              tester
                  .getSize(find.byKey(const ValueKey('home-settings-button')))
                  .height,
              42,
            );
          }
          previous = r;
          previousWidth = w;
        }
      }
      await tester.binding.setSurfaceSize(const Size(360, 220));
      await tester.pumpAndSettle();
      final scroller = find.byType(Scrollable).first;
      final position = tester.state<ScrollableState>(scroller).position;
      expect(position.maxScrollExtent, greaterThan(0));
      await tester.sendEventToBinding(
        PointerScrollEvent(
          position: tester.getRect(scroller).center,
          scrollDelta: const Offset(0, 100),
        ),
      );
      await tester.pumpAndSettle();
      expect(position.pixels, greaterThan(0));
      expect(find.byType(Scrollbar), findsNothing);
      expect(find.byType(RawScrollbar), findsNothing);
      await tester.ensureVisible(find.text('关于 Sidravia'));
      await tester.pumpAndSettle();
      expect(
        tester.getRect(find.text('关于 Sidravia')).bottom,
        lessThanOrEqualTo(220),
      );
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );
  for (final platform in [
    TargetPlatform.windows,
    TargetPlatform.android,
    TargetPlatform.iOS,
  ]) {
    testWidgets('notice long scrolling shell and dismissal $platform', (
      tester,
    ) async {
      final fixture = await noticeFixture(tester, platform, long: true);
      expect(
        find.byType(Dialog),
        platform == TargetPlatform.windows ? findsOneWidget : findsNothing,
      );
      expect(
        find.byType(BottomSheet),
        platform == TargetPlatform.windows ? findsNothing : findsOneWidget,
      );
      expect(fixture.unreadCount, 0);
      final scroll = find.descendant(
        of: find.byKey(const ValueKey('announcement-scroll')),
        matching: find.byType(Scrollable),
      );
      final position = tester.state<ScrollableState>(scroll).position;
      expect(position.maxScrollExtent, greaterThan(0));
      if (platform == TargetPlatform.windows) {
        await tester.sendEventToBinding(
          PointerScrollEvent(
            position: tester.getRect(scroll).center,
            scrollDelta: const Offset(0, 200),
          ),
        );
        await tester.pumpAndSettle();
        expect(position.pixels, greaterThan(0));
        final before = position.pixels;
        await tester.sendKeyEvent(LogicalKeyboardKey.pageDown);
        await tester.pumpAndSettle();
        expect(position.pixels, greaterThan(before));
        await tester.sendKeyEvent(LogicalKeyboardKey.escape);
        await tester.pumpAndSettle();
      } else {
        await tester.drag(scroll, const Offset(0, -180));
        await tester.pumpAndSettle();
        expect(position.pixels, greaterThan(0));
        await tester.drag(scroll, const Offset(0, 40));
        await tester.pumpAndSettle();
        expect(find.byType(BottomSheet), findsOneWidget);
        position.jumpTo(0);
        await tester.pump();
        await tester.drag(scroll, const Offset(0, 350));
        await tester.pumpAndSettle();
        expect(find.byType(BottomSheet), findsOneWidget);
        final sheet = tester.getRect(find.byType(BottomSheet));
        await tester.flingFrom(
          Offset(sheet.center.dx, sheet.top + 24),
          const Offset(0, 300),
          1000,
        );
        await tester.pumpAndSettle();
      }
      expect(find.byType(AnnouncementContent), findsNothing);
      expect(focusNode.hasFocus, isTrue);
      await openNotice(tester, fixture);
      expect(find.byType(RawScrollbar), findsNothing);
      expect(find.byType(Scrollbar), findsNothing);
      await tester.tap(find.byTooltip('关闭公告'));
      await tester.pumpAndSettle();
      expect(find.byType(AnnouncementContent), findsNothing);
      await openNotice(tester, fixture);
      await tester.tapAt(const Offset(5, 5));
      await tester.pumpAndSettle();
      expect(find.byType(AnnouncementContent), findsNothing);
      await openNotice(tester, fixture);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.byType(AnnouncementContent), findsNothing);
      await tester.pumpWidget(const SizedBox.shrink());
      fixture.dispose();
      focusNode.dispose();
    });
  }
  for (final spec in [
    (TargetPlatform.windows, const Size(360, 640), 1.0, 0.0, 'pc'),
    (TargetPlatform.android, const Size(360, 640), 1.0, 0.0, 'android'),
    (TargetPlatform.iOS, const Size(360, 640), 1.0, 0.0, 'ios'),
    (TargetPlatform.android, const Size(780, 320), 1.8, 120.0, 'landscape'),
    (TargetPlatform.iOS, const Size(780, 320), 1.8, 180.0, 'keyboard'),
  ]) {
    testWidgets('notice safe scaled candidate ${spec.$5}', (tester) async {
      final fixture = await noticeFixture(
        tester,
        spec.$1,
        size: spec.$2,
        scale: spec.$3,
        inset: spec.$4,
        long: spec.$5 == 'landscape' || spec.$5 == 'keyboard',
      );
      expect(tester.takeException(), isNull);
      final panel = tester.getRect(find.byType(AnnouncementContent));
      expect(
        panel.bottom,
        lessThanOrEqualTo(
          spec.$2.height - spec.$4 - (spec.$4 > 0 ? 0 : 20) + .01,
        ),
      );
      if (output.isNotEmpty) await captureNotice(tester, spec.$5);
      await tester.pumpWidget(const SizedBox.shrink());
      fixture.dispose();
      focusNode.dispose();
    });
  }
  testWidgets(
    'dev policy previews mobile with unchanged formal platform and binding',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(400, 690));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      await tester.pumpWidget(const OfflineDemoApp());
      await tester.pumpAndSettle();
      await tester.tap(find.text('公告预览：PC 对话框'));
      await tester.pumpAndSettle();
      expect(find.text('公告预览：手机面板'), findsOneWidget);
      await tester.ensureVisible(find.text('校园网维护安排'));
      await tester.tap(find.text('校园网维护安排'));
      await tester.pumpAndSettle();
      expect(find.byType(BottomSheet), findsOneWidget);
      await tester.tap(find.byTooltip('关闭公告'));
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox.shrink());
    },
    variant: TargetPlatformVariant({TargetPlatform.windows}),
  );
}

late FocusNode focusNode;

class NoticeFetcher implements AnnouncementFetcher {
  final bool long;
  NoticeFetcher(this.long);
  @override
  Future<AnnouncementFetchResult> fetch({
    String? etag,
    String? lastModified,
  }) async {
    final now = DateTime.utc(2026, 10, 5);
    final raw = jsonEncode({
      'schema': 1,
      'generatedAt': now.toIso8601String(),
      'items': [
        {
          'id': 'notice-test',
          'revision': 1,
          'level': 'maintenance',
          'title': '校园网维护安排',
          'body': long
              ? List.filled(45, '离线长正文：阅读后再关闭。').join('\n')
              : '这是离线公告，正文与业务规则保持一致。',
          'publishedAt': now.toIso8601String(),
          'startsAt': now.toIso8601String(),
          'expiresAt': now.add(const Duration(days: 365)).toIso8601String(),
        },
      ],
    });
    return AnnouncementFetchResult.updated(
      feed: AnnouncementDocument.decode(
        raw,
        endpoint: OfflineAnnouncementFetcher.endpoint,
      ),
      rawBody: raw,
    );
  }

  @override
  void close() {}
}

Future<AnnouncementController> noticeFixture(
  WidgetTester tester,
  TargetPlatform platform, {
  Size size = const Size(360, 640),
  double scale = 1,
  double inset = 0,
  bool long = false,
}) async {
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.binding.setSurfaceSize(size);
  addTearDown(() => tester.binding.setSurfaceSize(null));
  await tester.runAsync(fonts.loadCaptureFonts);
  final now = DateTime.utc(2026, 10, 5);
  final controller = AnnouncementController(
    endpoint: OfflineAnnouncementFetcher.endpoint,
    store: MemoryAnnouncementStore(),
    fetcher: NoticeFetcher(long),
    nowUtc: () => now,
  );
  await controller.start();
  focusNode = FocusNode();
  await tester.pumpWidget(
    MaterialApp(
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light().copyWith(platform: platform),
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(context).copyWith(
          textScaler: TextScaler.linear(scale),
          viewInsets: EdgeInsets.only(bottom: inset),
          padding: EdgeInsets.only(top: 24, bottom: inset > 0 ? 0 : 20),
          viewPadding: const EdgeInsets.only(top: 24, bottom: 20),
        ),
        child: RepaintBoundary(
          key: const ValueKey('notice-capture'),
          child: child!,
        ),
      ),
      home: Scaffold(
        body: Center(
          child: TextButton(
            focusNode: focusNode,
            onPressed: () => showAnnouncementSheet(
              tester.element(find.text('打开公告')),
              controller: controller,
            ),
            child: const Text('打开公告'),
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
  focusNode.requestFocus();
  await tester.pump();
  await openNotice(tester, controller);
  return controller;
}

Future<void> openNotice(
  WidgetTester tester,
  AnnouncementController controller,
) async {
  await tester.tap(find.text('打开公告'));
  await tester.pumpAndSettle();
}

Future<void> captureNotice(WidgetTester tester, String name) async {
  final boundary = tester.renderObject<RenderRepaintBoundary>(
    find.byKey(const ValueKey('notice-capture')),
  );
  await tester.runAsync(() async {
    await Directory(output).create(recursive: true);
    final image = await boundary.toImage(pixelRatio: 1);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    await File('$output/$name.png').writeAsBytes(bytes!.buffer.asUint8List());
    image.dispose();
  });
}
