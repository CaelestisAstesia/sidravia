import 'school_announcement_controller_test.dart' as school_dispatch;

import 'dart:convert';
import 'dart:io';

import 'package:flutter/gestures.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';

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
    try {
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
        if (previous != height ||
            w >= 748 && w <= 768 ||
            w >= 950 && w <= 970) {
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
        'platform':
            'Windows widget simulation; native measurements unavailable',
      };
      if (output.isNotEmpty) {
        await tester.runAsync(() async {
          await Directory(output).create(recursive: true);
          await File(
            '$output/${baseline ? 'baseline' : 'updated'}-geometry.json',
          ).writeAsString(const JsonEncoder.withIndent('  ').convert(records));
        });
      }
    } finally {
      await school_dispatch.closeShellOwners(tester);
      await tester.pumpWidget(const SizedBox.shrink());
    }
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
            expect((r.width - previous.width).abs(), lessThan(1.001));
            expect((r.left - previous.left).abs(), lessThan(.60));
          }
          if (page == 'home') {
            expect(
              tester.getSize(find.byKey(const ValueKey('home-page'))).width,
              w.toDouble(),
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
    testWidgets(
      'shared school entry remains reachable at narrow width $platform',
      (tester) async {
        await tester.binding.setSurfaceSize(const Size(360, 640));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        try {
          await tester.pumpWidget(
            MaterialApp(
              theme: AppTheme.light().copyWith(platform: platform),
              home: const OfflineDemoApp(),
            ),
          );
          await tester.pumpAndSettle();
          await tester.ensureVisible(find.text('吉林大学公告'));
          await tester.tap(find.text('吉林大学公告'));
          await tester.pumpAndSettle();
          expect(find.text('离线公告占位预览'), findsOneWidget);
          expect(tester.takeException(), isNull);
          await tester.tap(find.text('关闭'));
          await tester.pumpAndSettle();
          expect(find.text('离线公告占位预览'), findsNothing);
        } finally {
          await school_dispatch.closeShellOwners(tester);
          await tester.pumpWidget(const SizedBox.shrink());
        }
      },
    );
  }
}
