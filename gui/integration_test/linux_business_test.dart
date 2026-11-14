import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:sidravia_gui/main.dart' as production;
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets('synthetic business travels UDP Run Session IPC GUI', (
    tester,
  ) async {
    final namespace = Platform.environment['SIDRAVIA_NAMESPACE']!;
    final root = Directory(Platform.environment['SIDRAVIA_GUI_FIXTURE']!);
    expect(File('${root.path}/fixture-owner').readAsStringSync(), namespace);
    expect(Platform.environment['SIDRAVIA_GUI_BUSINESS_FIXTURE'], '1');
    await production.main(['--skip-auto-login', '--start-visible']);
    await tester.pump();
    final app = tester.widget<SidraviaApp>(find.byType(SidraviaApp));
    final c = app.controller;
    try {
      await waitNative(
        tester,
        () => c.state != GuiConnectionState.bootstrapping && !c.busy,
      );
      expect(c.state, GuiConnectionState.ready);
      expect(c.snapshot!.profiles.single.id, 'linux-loopback');
      expect(c.snapshot!.configurations, isEmpty);
      expect(
        await c.createConfiguration(
          institutionProfileId: 'linux-loopback',
          username: 'fictional-business',
          password: 'fictional-only',
        ),
        isTrue,
      );
      final id = c.snapshot!.configurations.single.id;
      expect(await c.startConfiguration(id), isTrue);
      await waitNative(
        tester,
        () =>
            c.snapshot!.sessions.single.state == SessionState.authenticated &&
            c.business.snapshot?.messages.isNotEmpty == true,
      );
      final first = c.business.snapshot!;
      expect(first.current, isTrue);
      expect(first.statistics!.trafficBytes, 126418944);
      expect(first.statistics!.balanceCents, 12345);
      expect(first.messages.single.text, 'Synthetic server notice');
      await tester.pump();
      expect(find.text('余额：123.45 元'), findsOneWidget);
      expect(find.text('Synthetic server notice'), findsOneWidget);
      expect(await c.restartSession(c.snapshot!.sessions.single.id), isTrue);
      await waitNative(
        tester,
        () =>
            c.business.snapshot != null &&
            c.business.snapshot!.runGeneration > first.runGeneration &&
            c.business.snapshot!.messages.isNotEmpty,
      );
      expect(
        c.business.snapshot!.messages.single.text,
        'Synthetic server notice',
      );
      expect(await c.stopSession(c.snapshot!.sessions.single.id), isTrue);
      await waitNative(tester, () => c.business.snapshot?.current == false);
      expect(c.business.snapshot!.statistics!.balanceCents, 12345);
      File('${root.path}/business-ui-confirmed').writeAsStringSync(
        'login statistics and verified text rendered; new Run isolated; stop retained',
      );
    } finally {
      await c.close();
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    }
  });
}

Future<void> waitNative(WidgetTester tester, bool Function() condition) async {
  await tester.runAsync(() async {
    final end = DateTime.now().add(const Duration(seconds: 20));
    while (!condition()) {
      if (DateTime.now().isAfter(end)) fail('business chain deadline');
      await Future<void>.delayed(const Duration(milliseconds: 25));
    }
  });
}
