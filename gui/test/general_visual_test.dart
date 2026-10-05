import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

import 'home_responsive_test.dart' as fonts;

const output = String.fromEnvironment('GEN_FRONTEND_OUTPUT');
const scenarios = [
  'authenticated',
  'suspended',
  'authenticating',
  'waiting_for_network',
  'waiting_before_retry',
  'blocked_by_error',
  'empty',
];
Map<String, double> rect(Rect r) => {
  'x': r.left,
  'y': r.top,
  'width': r.width,
  'height': r.height,
};
void main() {
  for (final dark in [false, true]) {
    for (final scenario in scenarios) {
      testWidgets(
        'candidate home $scenario dark=$dark',
        (tester) async => render(tester, dark, scenario, 'home'),
      );
    }
    for (final page in [
      'settings',
      'account',
      'details',
      'diagnostics',
      'modal',
    ]) {
      testWidgets(
        'candidate $page dark=$dark',
        (tester) async => render(tester, dark, 'authenticated', page),
      );
    }
  }
}

Future<void> render(
  WidgetTester tester,
  bool dark,
  String scenario,
  String page,
) async {
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetDevicePixelRatio);
  tester.platformDispatcher.textScaleFactorTestValue = 1;
  addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
  await tester.binding.setSurfaceSize(const Size(398, 642));
  addTearDown(() => tester.binding.setSurfaceSize(null));
  await tester.runAsync(() async {
    await fonts.loadCaptureFonts();
    const diagnosticFont = String.fromEnvironment(
      'DIAGNOSTIC_FONT',
      defaultValue: '/mnt/c/Windows/Fonts/consola.ttf',
    );
    if (await File(diagnosticFont).exists()) {
      await (FontLoader('monospace')..addFont(
            File(diagnosticFont).readAsBytes().then(ByteData.sublistView),
          ))
          .load();
    } else if (output.isNotEmpty) {
      throw StateError(
        'DIAGNOSTIC_FONT must identify the existing platform monospace font for valid candidate capture',
      );
    }
  });
  final client = OfflineDemoClient()..selectScenario(scenario);
  final controller = GuiController(
    bootstrapper: OfflineDemoBootstrap(),
    connector: (_) async => client,
  );
  final now = DateTime.utc(2026, 10, 5);
  final notices = AnnouncementController(
    endpoint: OfflineAnnouncementFetcher.endpoint,
    store: MemoryAnnouncementStore(),
    fetcher: OfflineAnnouncementFetcher(now),
    nowUtc: () => now,
  );
  await controller.start();
  await notices.start();
  final appearance = Appearance()
    ..value = dark ? ThemeMode.dark : ThemeMode.light;
  await tester.pumpWidget(
    RepaintBoundary(
      key: const ValueKey('general-capture'),
      child: AppearanceScope(
        appearance: appearance,
        child: MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: (dark ? AppTheme.dark() : AppTheme.light()).copyWith(
            platform: TargetPlatform.windows,
          ),
          home: SidraviaShell(controller: controller, announcements: notices),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
  if (page == 'settings') {
    await tester.tap(find.byTooltip('设置'));
  }
  if (page == 'account') {
    await tester.tap(find.text('吉林大学'));
  }
  if (page == 'details' || page == 'diagnostics') {
    await tester.tap(find.text('连接详情'));
    await tester.pumpAndSettle();
    if (page == 'diagnostics') {
      await tester.ensureVisible(find.text('技术诊断'));
      await tester.tap(find.text('技术诊断'));
    }
  }
  if (page == 'modal') {
    await tester.tap(find.byType(HomeNoticeView));
  }
  await tester.pumpAndSettle();
  expect(tester.takeException(), isNull);
  if (output.isNotEmpty) {
    final data = <String, Object?>{
      'theme': dark ? 'dark' : 'light',
      'scenario': scenario,
      'page': page,
      'size': [398, 642],
      'elements': <String, Object>{},
      'rows': [
        for (final r in tester.widgetList<DesignRow>(find.byType(DesignRow)))
          {'title': r.title, 'value': r.value, 'subtitle': r.subtitle},
      ],
    };
    final elements = data['elements'] as Map<String, Object>;
    for (final key in [
      'home-configuration-button',
      'home-settings-button',
      'home-header',
      'home-status',
      'home-mark',
      'home-context',
      'home-actions',
      'home-divider',
      'home-notice',
    ]) {
      final finder = find.byKey(ValueKey(key));
      if (finder.evaluate().isNotEmpty) {
        elements[key] = rect(tester.getRect(finder));
      }
    }
    final issueFinder = find.byWidgetPredicate(
      (w) => w.runtimeType.toString() == '_HomeIssue',
    );
    if (issueFinder.evaluate().isNotEmpty) {
      elements['home-issue'] = rect(tester.getRect(issueFinder));
    }
    if (find.byType(HomeView).evaluate().isNotEmpty) {
      final d = tester.widget<HomeView>(find.byType(HomeView)).data;
      data['home'] = {'detail': d.detail, 'context': d.context};
    }
    for (final type in [DesignHeader, DesignGroup, Dialog, TextField]) {
      final finder = find.byWidgetPredicate((w) => w.runtimeType == type);
      if (finder.evaluate().isNotEmpty) {
        elements[type.toString()] = rect(
          tester.getRect(
            type == Dialog
                ? find.byKey(const ValueKey('announcement-panel'))
                : type == DesignHeader
                ? find
                      .descendant(
                        of: finder.first,
                        matching: find.byType(Column),
                      )
                      .first
                : finder.first,
          ),
        );
      }
    }
    final boundary = tester.renderObject<RenderRepaintBoundary>(
      find.byKey(const ValueKey('general-capture')),
    );
    await tester.runAsync(() async {
      final dir = Directory(
        '$output/${dark ? 'dark' : 'light'}-${page == 'home' ? scenario : page}',
      );
      await dir.create(recursive: true);
      final image = await boundary.toImage(pixelRatio: 1);
      final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
      await File('${dir.path}/flutter.png')
          .writeAsBytes(bytes!.buffer.asUint8List());
      image.dispose();
      await File('${dir.path}/flutter.json').writeAsString(jsonEncode(data));
    });
  }
  await tester.pumpWidget(const SizedBox.shrink());
  controller.dispose();
  notices.dispose();
  client.dispose();
  appearance.dispose();
}
