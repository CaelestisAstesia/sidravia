import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/dev/preview_fixtures.dart';
import 'package:sidravia_gui/dev/preview_main.dart' as preview;
import 'package:sidravia_gui/dev/widget_previews.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';

void main() {
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
      expect(find.text('注销'), findsOneWidget);

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

  testWidgets('all official widget previews render without external state', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    for (final widget in [
      authenticatedHomePreview(),
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
      expect(find.byType(NavigationBar), findsOneWidget);
      await tester.tap(find.text('关于'));
      await tester.pumpAndSettle();
      expect(find.text('校园网认证工具'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );

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

    expect(find.text('正在连接…'), findsOneWidget);
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
        of: find.byType(NavigationBar),
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
        of: find.byType(NavigationBar),
        matching: find.text('关于'),
      ),
    );
    await tester.pumpAndSettle();
    expect(
      find.descendant(of: header, matching: find.text('关于')),
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
          .widget<Text>(find.descendant(of: header, matching: find.text('连接')))
          .style
          ?.fontSize,
      theme.textTheme.titleSmall?.fontSize,
    );
    expect(
      tester.widget<Text>(find.text('已连接')).style?.fontSize,
      theme.textTheme.titleMedium?.fontSize,
    );
    expect(
      MediaQuery.textScalerOf(tester.element(find.text('已连接'))).scale(10),
      10,
    );
    expect(SidraviaLayout.compactContentMaxTextScale, 1.6);

    await tester.tap(
      find.descendant(
        of: find.byType(NavigationBar),
        matching: find.text('关于'),
      ),
    );
    await tester.pumpAndSettle();
    expect(
      tester.widget<Text>(find.text('Sidravia')).style?.fontSize,
      theme.textTheme.titleSmall?.fontSize,
    );
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
}
