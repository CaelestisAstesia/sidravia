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
  bool fail = false, guiAvailable = true;
  @override
  Future<IntegrationStatus> status() async {
    reads++;
    if (fail) throw const IntegrationFailure('读取失败');
    if (pending != null) return pending!.future;
    return IntegrationStatus(
      pathEnabled: path,
      autostart: 'disabled',
      guiAvailable: guiAvailable,
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

class FakeUserServiceManager extends FakeManager implements UserServiceManager {
  bool registered = false;
  final actions = <String>[];

  @override
  Future<UserServiceStatus?> userService(String action) async {
    if (action != 'status') {
      actions.add(action);
      registered = action == 'takeover';
    }
    return UserServiceStatus(
      available: true,
      directory: registered ? '/fixture' : null,
      running: registered,
      enabled: false,
      linger: false,
      matchesInstallation: registered,
      recoveryPending: false,
      canDiscardReplacement: false,
      recoveryBlocked: false,
      canDiscardDamaged: false,
    );
  }
}

void main() {
  testWidgets('background dropdown confirms changes and reports current mode', (
    tester,
  ) async {
    final manager = FakeUserServiceManager();
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData.dark(),
        home: Scaffold(
          body: Align(
            alignment: Alignment.topLeft,
            child: SizedBox(
              width: 320,
              child: IntegrationSettings(manager: manager),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    final selector = find.byKey(const ValueKey('user-service-selector'));
    expect(
      find.descendant(of: selector, matching: find.text('应用管理')),
      findsOneWidget,
    );
    Future<void> select(String option) async {
      await tester.tap(selector);
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(MenuItemButton, option));
      await tester.pumpAndSettle();
    }

    await select('切换到 systemd');
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(manager.actions, isEmpty);

    await select('切换到 systemd');
    await tester.tap(find.text('确认'));
    await tester.pumpAndSettle();
    expect(manager.actions, ['takeover']);
    expect(
      find.descendant(of: selector, matching: find.text('systemd')),
      findsOneWidget,
    );
    expect(find.textContaining('/fixture'), findsNothing);

    await select('切回应用管理');
    await tester.tap(find.text('确认'));
    await tester.pumpAndSettle();
    expect(manager.actions, ['takeover', 'release']);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    await tester.pumpAndSettle();
  });

  testWidgets('unavailable GUI explains and disables tray autostart', (
    tester,
  ) async {
    final manager = FakeManager()..guiAvailable = false;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: IntegrationSettings(manager: manager)),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('图形程序不可用，无法启动到托盘'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('autostart-selector')));
    await tester.pumpAndSettle();
    final option = tester.widget<MenuItemButton>(
      find.widgetWithText(MenuItemButton, '启动到托盘'),
    );
    expect(option.onPressed, isNull);
    expect(manager.lastChange, isNull);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpAndSettle();
    expect(manager.closed, isTrue);
  });

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
    await tester.tap(find.byType(DesignSwitch));
    await tester.pumpAndSettle();
    expect(find.text('读取失败'), findsNWidgets(2));
    expect(find.byType(DesignSwitch), findsNothing);
    expect(find.text('状态未知'), findsWidgets);
    manager.fail = false;
    await tester.tap(find.text('重试').first);
    await tester.pumpAndSettle();
    expect(find.text('读取失败'), findsNothing);
    expect(find.text('重试'), findsNothing);
    expect(find.text('刷新'), findsNothing);
    expect(manager.reads, 4);
    expect(manager.lastChange, IntegrationChange.pathDisable);
    expect(tester.widget<DesignSwitch>(find.byType(DesignSwitch)).value, false);
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
    expect(find.text('正在读取…'), findsWidgets);
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
