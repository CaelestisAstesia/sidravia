import 'dart:io';

import 'package:sidravia_gui/features/diagnostics/website_latency.dart';

import 'website_latency_test.dart' show PendingProbe;

import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/diagnostics/website_latency_section.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

import 'support/capture_fonts.dart' as fonts;

const output = String.fromEnvironment('WEBSITE_LATENCY_CAPTURE');

void main() {
  for (final width in [320.0, 960.0]) {
    for (final dark in [false, true]) {
      for (final scale in [1.0, 2.0]) {
        testWidgets('website latency $width dark=$dark scale=$scale', (
          tester,
        ) async {
          final controller = WebsiteLatencyController(
            createProbe: (_) => PendingProbe(),
          );
          addTearDown(controller.dispose);
          tester.view.devicePixelRatio = 1;
          tester.view.physicalSize = Size(width, 900);
          addTearDown(tester.view.resetPhysicalSize);
          addTearDown(tester.view.resetDevicePixelRatio);
          if (output.isNotEmpty) {
            await tester.runAsync(
              () => fonts.loadCaptureFonts(forCapture: true),
            );
          }
          await tester.pumpWidget(
            MaterialApp(
              debugShowCheckedModeBanner: false,
              theme: dark ? AppTheme.dark() : AppTheme.light(),
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(context)
                    .copyWith(textScaler: TextScaler.linear(scale)),
                child: child!,
              ),
              home: RepaintBoundary(
                key: const ValueKey('website-capture'),
                child: Scaffold(
                  body: SingleChildScrollView(
                    padding: const EdgeInsets.all(24),
                    child: WebsiteLatencySection(controller: controller),
                  ),
                ),
              ),
            ),
          );
          await tester.pumpAndSettle();
          expect(find.text('未测试'), findsNWidgets(5));
          expect(find.text('百度'), findsOneWidget);
          expect(find.text('淘宝'), findsOneWidget);
          expect(
            tester
                .widget<OutlinedButton>(
                  find.widgetWithText(OutlinedButton, '测试'),
                )
                .onPressed,
            isNotNull,
          );
          expect(tester.takeException(), isNull);
          if (output.isNotEmpty) {
            final boundary = tester.renderObject<RenderRepaintBoundary>(
              find.byKey(const ValueKey('website-capture')),
            );
            await tester.runAsync(() async {
              await Directory(output).create(recursive: true);
              final image = await boundary.toImage(pixelRatio: 1);
              final bytes = await image.toByteData(
                format: ui.ImageByteFormat.png,
              );
              await File(
                '$output/${width.toInt()}-${dark ? "dark" : "light"}-$scale.png',
              ).writeAsBytes(bytes!.buffer.asUint8List());
              image.dispose();
            });
          }
          await tester.pumpWidget(const SizedBox.shrink());
        }, variant: const TargetPlatformVariant({TargetPlatform.linux}));
      }
    }
  }
  testWidgets('test, progressive results, cancel, retry, and page disposal', (
    tester,
  ) async {
    final probes = <PendingProbe>[];
    final controller = WebsiteLatencyController(
      createProbe: (_) {
        final probe = PendingProbe();
        probes.add(probe);
        return probe;
      },
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: WebsiteLatencySection(controller: controller)),
      ),
    );
    await tester.tap(find.text('测试'));
    await tester.pump();
    expect(find.text('取消'), findsOneWidget);
    expect(find.text('测试中…'), findsNWidgets(2));
    expect(find.text('等待中'), findsNWidgets(3));
    probes[0].done.complete(
      const WebsiteLatencyResult(WebsiteLatencyStatus.success, 88),
    );
    await tester.pump();
    expect(find.text('88 ms'), findsOneWidget);
    await tester.tap(find.text('取消'));
    await tester.pump();
    expect(find.text('已取消'), findsNWidgets(4));
    expect(find.text('测试'), findsOneWidget);
    await tester.tap(find.text('测试'));
    await tester.pump();
    expect(find.text('88 ms'), findsNothing);
    // Cover each terminal presentation through the public controller contract.
    probes[3].done.complete(
      const WebsiteLatencyResult(WebsiteLatencyStatus.timeout),
    );
    probes[4].done.complete(
      const WebsiteLatencyResult(WebsiteLatencyStatus.restricted),
    );
    await tester.pump();
    probes[5].done.complete(
      const WebsiteLatencyResult(WebsiteLatencyStatus.unexpected),
    );
    probes[6].done.complete(
      const WebsiteLatencyResult(WebsiteLatencyStatus.failed),
    );
    await tester.pump();
    probes[7].done.complete(
      const WebsiteLatencyResult(WebsiteLatencyStatus.success, 42),
    );
    await tester.pump();
    for (final value in ['超时', '访问受限', '响应异常', '失败', '42 ms']) {
      expect(find.text(value), findsOneWidget);
    }
    await tester.tap(find.text('测试'));
    await tester.pump();
    await tester.pumpWidget(const SizedBox.shrink());
    controller.dispose();
    await tester.pump();
    expect(tester.takeException(), isNull);
  });
  testWidgets('offline environment disables manual test with a reason', (
    tester,
  ) async {
    final controller = WebsiteLatencyController(createProbe: null);
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: WebsiteLatencySection(controller: controller)),
      ),
    );
    expect(find.text('当前环境不支持直连测试'), findsOneWidget);
    expect(
      tester
          .widget<OutlinedButton>(find.widgetWithText(OutlinedButton, '测试'))
          .onPressed,
      isNull,
    );
  });
}
