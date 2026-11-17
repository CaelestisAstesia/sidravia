import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/connection_clock.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/dev/interactive_demo_main.dart';
import 'package:sidravia_gui/features/diagnostics/runtime_snapshot.dart';

import 'school_announcement_controller_test.dart' as owners;
import 'support/runtime_snapshot.dart';

void main() {
  final fixed = DateTime.utc(2026, 1, 2, 12, 34, 56);
  test(
    'frozen demo time covers scenarios, fallback data and restart',
    () async {
      final client = OfflineDemoClient(now: fixed);
      try {
        client.selectScenario('authenticated_business');
        var session = (await client.sessionList()).single;
        expect(
          session.authenticationEstablishedAt,
          fixed.subtract(const Duration(hours: 2, minutes: 18)),
        );
        var business = await client.queryBusiness(session.id).result;
        expect(
          business.messages.last.receivedAt,
          session.authenticationEstablishedAt,
        );
        client.selectScenario('suspended');
        session = (await client.sessionList()).single;
        business = await client.queryBusiness(session.id).result;
        expect(business.updatedAt, fixed);
        session = await client.sessionEnsureRunning(session.id);
        expect(session.authenticationEstablishedAt, fixed);
        session = await client.sessionRestart(session.id);
        expect(session.authenticationEstablishedAt, fixed);
      } finally {
        await client.close();
        client.dispose();
      }
    },
  );

  test(
    'snapshot shares presentation clock and preserves explicit override',
    () async {
      var now = fixed;
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => OfflineDemoClient(now: fixed),
        presentationClock: ConnectionClock(now: () => now),
      );
      expect(
        jsonDecode(captureRuntimeSnapshot(controller))['capturedAt'],
        fixed.toIso8601String(),
      );
      now = fixed.add(const Duration(days: 400));
      expect(
        jsonDecode(captureRuntimeSnapshot(controller))['capturedAt'],
        now.toIso8601String(),
      );
      expect(
        jsonDecode(
          captureRuntimeSnapshot(controller, capturedAt: fixed),
        )['capturedAt'],
        fixed.toIso8601String(),
      );
      await controller.close();
      controller.dispose();
    },
  );

  testWidgets('actual demo diagnostics uses the same frozen time', (
    tester,
  ) async {
    try {
      await tester.pumpWidget(OfflineDemoApp(fixedTime: fixed));
      await tester.pumpAndSettle();
      await tester.tap(find.text('连接与诊断'));
      await tester.pumpAndSettle();
      expect(displayedSnapshot(tester)['capturedAt'], fixed.toIso8601String());
      await tester.pump(const Duration(minutes: 2));
      await tester.pumpAndSettle();
      expect(displayedSnapshot(tester)['capturedAt'], fixed.toIso8601String());
    } finally {
      await owners.closeShellOwners(tester);
      await tester.pumpWidget(const SizedBox.shrink());
    }
  });
}
