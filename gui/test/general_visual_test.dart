import 'school_announcement_controller_test.dart' as school_dispatch;

import 'package:sidravia_gui/shared/widgets/connection_issue_card.dart';

import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
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
    for (final enabled in [false, true]) {
      testWidgets(
        'common buttons retain ordinary font dark=$dark enabled=$enabled',
        (tester) async {
          await tester.pumpWidget(
            MaterialApp(
              theme: dark ? AppTheme.dark() : AppTheme.light(),
              home: Scaffold(
                body: Column(
                  children: [
                    FilledButton(
                      onPressed: enabled ? () {} : null,
                      child: const Text('保存修改'),
                    ),
                    OutlinedButton(
                      onPressed: enabled ? () {} : null,
                      child: const Text('连接与诊断'),
                    ),
                  ],
                ),
              ),
            ),
          );
          for (final label in ['保存修改', '连接与诊断']) {
            final span =
                tester.renderObject<RenderParagraph>(find.text(label)).text
                    as TextSpan;
            expect(span.style?.fontFamily, 'HarmonyOS Sans');
          }
        },
      );
    }
  }

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
      'announcement-feedback',
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
  await controller.start();
  final notices = await school_dispatch.manualAnnouncements(tester, controller);
  await notices.start();
  final appearance = Appearance()
    ..value = dark ? ThemeMode.dark : ThemeMode.light;
  try {
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
      await tester.tap(find.text('连接与诊断'));
      await tester.pumpAndSettle();
      if (page == 'diagnostics') {
        await tester.ensureVisible(find.text('技术信息'));
        await tester.tap(find.text('技术信息'));
        await tester.pumpAndSettle();
        expect(find.text('核心服务状态'), findsOneWidget);
      } else {
        expect(find.text('核心服务状态'), findsNothing);
      }
    }
    if (page == 'announcement-feedback') {
      await tester.ensureVisible(find.byType(HomeNoticeView));
      await tester.tap(find.byType(HomeNoticeView));
    }
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    if (page == 'home' && scenario == 'blocked_by_error') {
      expect(find.byType(ConnectionIssueCard), findsOneWidget);
    }
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
      final issueFinder = find.byType(ConnectionIssueCard);
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
              type == DesignHeader
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
  } finally {
    await tester.pump();
    await school_dispatch.pumpCleanup(tester, () async {
      await notices.close();
      await controller.close();
      await appearance.settled;
    });
    await tester.pumpWidget(const SizedBox.shrink());
    controller.dispose();
    notices.dispose();
    client.dispose();
    appearance.dispose();
  }
}
