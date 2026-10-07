import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/dev/preview_fixtures.dart';
import 'package:sidravia_gui/dev/preview_main.dart' as preview;
import 'package:sidravia_gui/dev/widget_previews.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';

void main() {
  test('preview entry keeps zero desktop presence surface', () {
    final main = File('lib/dev/preview_main.dart').readAsStringSync();
    final fixtures = File('lib/dev/preview_fixtures.dart').readAsStringSync();
    expect(main, isNot(contains('desktop_presence')));
    expect(main, isNot(contains('DesktopPresence')));
    expect(main, isNot(contains('daemonStop')));
    expect(main, isNot(contains('tray')));
    expect(main, isNot(contains('notification')));
    expect(fixtures, isNot(contains('DesktopPresence')));
    expect(fixtures, isNot(contains('daemonStop')));
    expect(fixtures, isNot(contains('tray')));
    expect(fixtures, isNot(contains('notification')));
  });

  testWidgets(
    'preview catalog uses real shell navigation with fictional state',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(1280, 720));
      addTearDown(() => tester.binding.setSurfaceSize(null));

      await tester.pumpWidget(const preview.PreviewCatalog());
      await tester.pumpAndSettle();

      expect(find.text('开发预览 · 不连接 daemon'), findsOneWidget);
      final sidebar = find.byKey(const ValueKey<String>('wide-sidebar'));
      expect(sidebar, findsOneWidget);
      expect(
        find.byKey(const ValueKey<String>('compact-header')),
        findsNothing,
      );
      expect(find.byType(NavigationBar), findsNothing);
      expect(
        find.byKey(const ValueKey<String>('preview-section')),
        findsNothing,
      );
      expect(find.text('已连接'), findsOneWidget);
      expect(find.text('断开连接'), findsOneWidget);

      await tester.tap(find.descendant(of: sidebar, matching: find.text('配置')));
      await tester.pumpAndSettle();
      expect(find.text('preview.student'), findsWidgets);

      await tester.tap(find.byKey(const ValueKey('preview-scenario')));
      await tester.pumpAndSettle();
      await tester.tap(find.text(PreviewScenario.noConfiguration.label).last);
      await tester.pumpAndSettle();

      expect(find.text('登录信息'), findsOneWidget);
      expect(find.text('preview.student'), findsNothing);
    },
  );

  testWidgets('Windows preview uses the native window frame', (tester) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.windows;
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    try {
      await tester.pumpWidget(const preview.PreviewCatalog());
      await tester.pumpAndSettle();

      final toolbar = find.text('开发预览 · 不连接 daemon');
      expect(toolbar, findsOneWidget);
      expect(
        find.byKey(const ValueKey<String>('windows-title-bar')),
        findsNothing,
      );
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  testWidgets('all official widget previews render without external state', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    for (final widget in [
      authenticatedHomePreview(),
      maintenanceAnnouncementHomePreview(),
      emptyConfigurationPreview(),
      settingsPreview(),
    ]) {
      await tester.pumpWidget(widget);
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    }
  });

  testWidgets(
    'preview controls and real narrow navigation stay inside surface',
    (tester) async {
      const size = Size(640, 600);
      await tester.binding.setSurfaceSize(size);
      addTearDown(() => tester.binding.setSurfaceSize(null));

      await tester.pumpWidget(const preview.PreviewCatalog());
      await tester.pumpAndSettle();

      void expectInside(Finder finder) {
        final rect = tester.getRect(finder);
        expect(rect.left, greaterThanOrEqualTo(0));
        expect(rect.top, greaterThanOrEqualTo(0));
        expect(rect.right, lessThanOrEqualTo(size.width));
        expect(rect.bottom, lessThanOrEqualTo(size.height));
      }

      final scenarioControl = find.byKey(
        const ValueKey<String>('preview-scenario'),
      );
      expectInside(scenarioControl);
      expectInside(
        find
            .descendant(
              of: scenarioControl,
              matching: find.text(PreviewScenario.authenticated.label),
            )
            .first,
      );
      expect(find.byKey(const ValueKey<String>('wide-sidebar')), findsNothing);
      expect(
        find.byKey(const ValueKey<String>('compact-header')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey<String>('compact-navigation')),
        findsOneWidget,
      );
      await tester.tap(
        find.byKey(const ValueKey<String>('destination-options')),
      );
      await tester.pumpAndSettle();
      expect(find.text('选项'), findsWidgets);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('preview catalog renders all four announcement scenarios', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    for (final scenario in PreviewAnnouncementScenario.values) {
      await tester.pumpWidget(
        preview.PreviewCatalog(announcementScenario: scenario),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const ValueKey('preview-announcement-scenario')),
        findsOneWidget,
        reason: scenario.name,
      );
      expect(tester.takeException(), isNull, reason: scenario.name);
      final inline = find.byKey(const ValueKey('announcement-inline-notice'));
      switch (scenario) {
        case PreviewAnnouncementScenario.maintenance:
        case PreviewAnnouncementScenario.critical:
          expect(inline, findsOneWidget, reason: scenario.name);
        case PreviewAnnouncementScenario.empty:
        case PreviewAnnouncementScenario.info:
          expect(inline, findsNothing, reason: scenario.name);
      }
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    }
  });

  testWidgets('bootstrapping preview remains bounded and disposable', (
    tester,
  ) async {
    await tester.pumpWidget(
      const preview.PreviewCatalog(
        initialScenario: PreviewScenario.bootstrapping,
        surface: preview.PreviewSurface.home,
        showToolbar: false,
      ),
    );
    await tester.pump();

    expect(find.text('正在准备连接'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('compact shell does not repeat destination page headings', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(const preview.PreviewCatalog(showToolbar: false));
    await tester.pumpAndSettle();

    final header = find.byKey(const ValueKey<String>('compact-header'));
    expect(
      find.descendant(of: header, matching: find.text('Sidravia')),
      findsNothing,
    );

    await tester.tap(
      find.descendant(
        of: find.byKey(const ValueKey<String>('compact-navigation')),
        matching: find.text('配置'),
      ),
    );
    await tester.pumpAndSettle();
    expect(
      find.descendant(of: header, matching: find.text('配置')),
      findsOneWidget,
    );

    await tester.tap(
      find.descendant(
        of: find.byKey(const ValueKey<String>('compact-navigation')),
        matching: find.text('选项'),
      ),
    );
    await tester.pumpAndSettle();
    expect(
      find.descendant(of: header, matching: find.text('选项')),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('compact preview keeps the primary type hierarchy restrained', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(const preview.PreviewCatalog(showToolbar: false));
    await tester.pumpAndSettle();

    final header = find.byKey(const ValueKey<String>('compact-header'));
    final theme = Theme.of(tester.element(header));
    expect(
      tester
          .widget<Text>(find.descendant(of: header, matching: find.text('仪表板')))
          .style
          ?.fontSize,
      theme.textTheme.titleSmall?.fontSize,
    );
    expect(
      tester.widget<Text>(find.text('已连接')).style?.fontSize,
      theme.textTheme.headlineSmall?.fontSize,
    );
    expect(
      MediaQuery.textScalerOf(tester.element(find.text('已连接'))).scale(10),
      10,
    );
    expect(SidraviaLayout.compactContentMaxTextScale, 1.6);

    await tester.tap(
      find.descendant(
        of: find.byKey(const ValueKey<String>('compact-navigation')),
        matching: find.text('选项'),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('选项'), findsWidgets);
    expect(tester.takeException(), isNull);
  });

  testWidgets('every preview scenario renders through the production home', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    for (final scenario in PreviewScenario.values) {
      await tester.pumpWidget(
        preview.PreviewCatalog(
          initialScenario: scenario,
          surface: preview.PreviewSurface.home,
          showToolbar: false,
        ),
      );
      await tester.pump(const Duration(milliseconds: 20));
      expect(
        find.byKey(const ValueKey<String>('connection-status-surface')),
        findsOneWidget,
        reason: scenario.name,
      );
      expect(tester.takeException(), isNull, reason: scenario.name);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    }
  });

  testWidgets(
    'preview configuration exercises AutoLogin, reset and deletion fictionally',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(1280, 1500));
      addTearDown(() => tester.binding.setSurfaceSize(null));

      await tester.pumpWidget(
        const preview.PreviewCatalog(
          initialScenario: PreviewScenario.authenticated,
          surface: preview.PreviewSurface.configuration,
          showToolbar: false,
        ),
      );
      await tester.pumpAndSettle();

      final autoLogin = find.byKey(const ValueKey('configuration-auto-login'));
      expect(tester.widget<SwitchListTile>(autoLogin).value, isTrue);
      await tester.tap(autoLogin);
      await tester.pumpAndSettle();
      expect(tester.widget<SwitchListTile>(autoLogin).value, isFalse);

      final reset = find.byKey(const ValueKey('reset-session'));
      expect(reset, findsOneWidget);
      await tester.tap(reset);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('reset-confirm')));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('reset-session')), findsNothing);
      expect(
        find.byKey(const ValueKey('delete-configuration')),
        findsOneWidget,
      );

      await tester.tap(find.byKey(const ValueKey('delete-configuration')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('delete-confirm')));
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey('configuration-account')),
        findsOneWidget,
      );
      expect(find.byKey(const ValueKey('delete-configuration')), findsNothing);
      expect(tester.takeException(), isNull);
    },
  );
}
