import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/integration_settings.dart';
import 'package:sidravia_gui/platform/installation_manager.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

class FakeManager implements InstallationManager {
  int reads = 0;
  bool closed = false, path = false;
  IntegrationChange? lastChange;
  Completer<IntegrationStatus>? pending;
  bool fail = false;
  @override
  Future<IntegrationStatus> status() async {
    reads++;
    if (fail) throw const IntegrationFailure('读取失败');
    if (pending != null) return pending!.future;
    return IntegrationStatus(
      pathEnabled: path,
      autostart: 'disabled',
      guiAvailable: true,
    );
  }

  @override
  Future<void> change(IntegrationChange change) async {
    lastChange = change;
    path = change == IntegrationChange.pathEnable;
  }

  @override
  Future<void> close() async {
    closed = true;
  }
}

void main() {
  test('status rejects unknown, failed and incompatible documents', () {
    for (final data in [
      '{}',
      '{"version":2,"ok":true,"code":"ok"}',
      '{"version":1,"ok":false,"code":"scheduled_task_conflict"}',
      '{"version":1,"ok":true,"code":"ok","capabilities":["user_path","user_autostart"],"pathEnabled":true,"guiAvailable":true,"autostart":"unknown"}',
    ]) {
      expect(
        () => IntegrationStatus.decode(data),
        throwsA(isA<IntegrationFailure>()),
      );
    }
  });
  testWidgets('query and mutation reread real status', (tester) async {
    final manager = FakeManager();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: IntegrationSettings(manager: manager)),
      ),
    );
    await tester.pumpAndSettle();
    expect(manager.reads, 1);
    await tester.tap(find.byType(DesignSwitch));
    await tester.pumpAndSettle();
    expect(manager.lastChange, IntegrationChange.pathEnable);
    expect(manager.reads, 2);
    expect(tester.widget<DesignSwitch>(find.byType(DesignSwitch)).value, true);
    manager.fail = true;
    await tester.tap(find.text('刷新'));
    await tester.pumpAndSettle();
    expect(find.text('读取失败'), findsOneWidget);
    expect(find.byType(DesignSwitch), findsNothing);
    expect(find.text('状态未知'), findsWidgets);
    await tester.pumpWidget(const SizedBox());
    expect(manager.closed, true);
  });
  testWidgets('pending query does not update disposed view', (tester) async {
    final manager = FakeManager()..pending = Completer<IntegrationStatus>();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: IntegrationSettings(manager: manager)),
      ),
    );
    expect(find.byType(DesignSwitch), findsNothing);
    expect(find.text('状态未知'), findsWidgets);
    await tester.pumpWidget(const SizedBox());
    manager.pending!.complete(
      const IntegrationStatus(
        pathEnabled: true,
        autostart: 'gui',
        guiAvailable: true,
      ),
    );
    await tester.pump();
    expect(tester.takeException(), isNull);
    expect(manager.closed, true);
  });
}
