import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';

void main() {
  testWidgets('diagnostics show unavailable facts without inventing values', (
    tester,
  ) async {
    final controller = GuiController(
      bootstrapper: const UnsupportedGuiBootstrapper(),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: AdvancedPage(controller: controller, onBack: () {}),
      ),
    );
    await tester.ensureVisible(find.text('技术信息'));
    await tester.tap(find.text('技术信息'));
    await tester.pump();
    expect(find.text('暂无数据'), findsAtLeastNWidgets(1));
    expect(find.text('复制诊断信息（暂不可用）'), findsOneWidget);
    controller.dispose();
  });

  Future<GuiController> setup(
    WidgetTester tester,
    Future<bool> Function()? open,
  ) async {
    final controller = GuiController(
      bootstrapper: const UnsupportedGuiBootstrapper(),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AdvancedPage(
            controller: controller,
            onBack: () {},
            onOpenLogDirectory: open,
          ),
        ),
      ),
    );
    await tester.tap(find.text('技术信息'));
    await tester.pumpAndSettle();
    return controller;
  }

  testWidgets('absent platform callback is honestly disabled', (tester) async {
    final controller = await setup(tester, null);
    try {
      final button = tester.widget<OutlinedButton>(
        find.widgetWithText(OutlinedButton, '打开日志目录（暂不可用）'),
      );
      expect(button.onPressed, isNull);
      expect(find.text('查看本实例的运行日志，便于排查连接问题。'), findsNothing);
      expect(find.text('配置数量'), findsOneWidget);
      expect(find.text('configuration_count'), findsNothing);
      expect(find.textContaining('探测仅检查指定网络路径'), findsNothing);
    } finally {
      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
    }
  });
  testWidgets('busy disables duplicate action; safe failure permits retry', (
    tester,
  ) async {
    final result = Completer<bool>();
    var calls = 0;
    final controller = await setup(tester, () {
      calls++;
      return result.future;
    });
    try {
      await tester.ensureVisible(find.text('打开日志目录'));
      await tester.tap(find.text('打开日志目录'));
      await tester.pump();
      expect(calls, 1);
      expect(
        tester
            .widget<OutlinedButton>(
              find.widgetWithText(OutlinedButton, '正在打开日志目录…'),
            )
            .onPressed,
        isNull,
      );
      result.complete(false);
      await tester.pumpAndSettle();
      expect(find.text('打开日志目录失败，请确认日志目录存在后重试'), findsOneWidget);
      expect(
        tester
            .widget<OutlinedButton>(
              find.widgetWithText(OutlinedButton, '打开日志目录'),
            )
            .onPressed,
        isNotNull,
      );
    } finally {
      if (!result.isCompleted) result.complete(false);
      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
    }
  });
  for (final throws in [false, true]) {
    testWidgets(
      'success gives no fake file list; errors use fixed message $throws',
      (tester) async {
        final controller = await setup(tester, () async {
          if (throws) throw StateError('secret path');
          return true;
        });
        try {
          await tester.ensureVisible(find.text('打开日志目录'));
          await tester.tap(find.text('打开日志目录'));
          await tester.pumpAndSettle();
          expect(
            find.text('打开日志目录失败，请确认日志目录存在后重试'),
            throws ? findsOneWidget : findsNothing,
          );
          expect(find.textContaining('secret path'), findsNothing);
          expect(tester.takeException(), isNull);
        } finally {
          await tester.pumpWidget(const SizedBox.shrink());
          await controller.close();
          controller.dispose();
        }
      },
    );
  }
  testWidgets('late completion after unmount does not show feedback', (
    tester,
  ) async {
    final result = Completer<bool>();
    final controller = await setup(tester, () => result.future);
    try {
      await tester.ensureVisible(find.text('打开日志目录'));
      await tester.tap(find.text('打开日志目录'));
      await tester.pump();
      await tester.pumpWidget(const SizedBox.shrink());
      result.complete(false);
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
    } finally {
      if (!result.isCompleted) result.complete(false);
      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
    }
  });
}
