import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';

void main() {
  testWidgets('wide shell uses horizontal navigation and switches sections', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(const SidraviaApp());

    expect(find.byType(NavigationRail), findsNothing);
    expect(find.byType(NavigationBar), findsNothing);
    for (final destination in appDestinations) {
      expect(find.text(destination.label), findsWidgets);
    }
    final homeNode = tester.getSemantics(
      find.byKey(const ValueKey<String>('destination-home')),
    );
    final configurationNode = tester.getSemantics(
      find.byKey(const ValueKey<String>('destination-configuration')),
    );
    final settingsNode = tester.getSemantics(
      find.byKey(const ValueKey<String>('destination-settings')),
    );
    expect(homeNode.flagsCollection.isSelected, ui.Tristate.isTrue);
    expect(configurationNode.flagsCollection.isSelected, ui.Tristate.isFalse);
    expect(settingsNode.flagsCollection.isSelected, ui.Tristate.isFalse);
    expect(find.text('daemon 尚未接入'), findsOneWidget);

    await tester.tap(find.text('配置').first);
    await tester.pumpAndSettle();

    expect(find.text('校园登录'), findsOneWidget);
    expect(
      tester
          .getSemantics(
            find.byKey(const ValueKey<String>('destination-configuration')),
          )
          .flagsCollection
          .isSelected,
      ui.Tristate.isTrue,
    );
  });

  testWidgets('narrow shell uses bottom navigation and switches sections', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(const SidraviaApp());

    expect(find.byType(NavigationBar), findsOneWidget);
    expect(find.byType(NavigationRail), findsNothing);

    await tester.tap(find.text('设置'));
    await tester.pumpAndSettle();

    expect(find.text('启动与路径'), findsOneWidget);
    final app = tester.widget<MaterialApp>(find.byType(MaterialApp));
    expect(app.title, 'Sidravia');
  });

  testWidgets('selection survives a responsive layout change', (tester) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(const SidraviaApp());
    await tester.tap(find.text('配置'));
    await tester.pumpAndSettle();
    await tester.binding.setSurfaceSize(const Size(1024, 720));
    await tester.pumpAndSettle();

    expect(find.text('校园登录'), findsOneWidget);
    expect(find.byType(NavigationBar), findsNothing);
  });

  testWidgets('compact header truncates rather than overflowing', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(220, 360));
    tester.binding.platformDispatcher.textScaleFactorTestValue = 2;
    addTearDown(() {
      tester.binding.platformDispatcher.clearTextScaleFactorTestValue();
      tester.binding.setSurfaceSize(null);
    });

    await tester.pumpWidget(const SidraviaApp());

    expect(tester.takeException(), isNull);
    expect(find.text('Sidravia'), findsOneWidget);
  });
}
