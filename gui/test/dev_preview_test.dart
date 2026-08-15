import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/dev/preview_fixtures.dart';
import 'package:sidravia_gui/dev/preview_main.dart' as preview;
import 'package:sidravia_gui/dev/widget_previews.dart';

void main() {
  testWidgets('preview catalog switches fictional state and product page', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(const preview.PreviewCatalog());
    await tester.pumpAndSettle();

    expect(find.text('开发预览 · 不连接 daemon'), findsOneWidget);
    expect(find.text('会话 预览校园网络 已认证。'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('preview-scenario')));
    await tester.pumpAndSettle();
    await tester.tap(find.text(PreviewScenario.noConfiguration.label).last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('配置').last);
    await tester.pumpAndSettle();

    expect(find.text('创建登录配置'), findsOneWidget);
    expect(find.text('preview.student'), findsNothing);
  });

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

  testWidgets('preview controls stay inside a narrow visual surface', (
    tester,
  ) async {
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
    final sectionControl = find.byKey(
      const ValueKey<String>('preview-section'),
    );
    expectInside(scenarioControl);
    expectInside(sectionControl);
    expectInside(
      find
          .descendant(
            of: scenarioControl,
            matching: find.text(PreviewScenario.authenticated.label),
          )
          .first,
    );
    for (final section in preview.PreviewSection.values) {
      expectInside(
        find
            .descendant(of: sectionControl, matching: find.text(section.label))
            .first,
      );
    }
    expect(tester.takeException(), isNull);
  });

  testWidgets('bootstrapping preview remains bounded and disposable', (
    tester,
  ) async {
    await tester.pumpWidget(
      const preview.PreviewCatalog(
        initialScenario: PreviewScenario.bootstrapping,
        showToolbar: false,
      ),
    );
    await tester.pump();

    expect(find.text('正在接入 daemon'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
