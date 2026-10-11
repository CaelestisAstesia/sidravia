import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class RestartBootstrap extends OfflineDemoBootstrap
    implements GuiCoreRestarter {
  int calls = 0;
  bool fail = false;
  Completer<void>? wait;
  String? level;
  @override
  Future<GuiBootstrapResult> restartCore(String logLevel) async {
    calls++;
    await wait?.future;
    level = logLevel;
    if (fail) {
      return const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
    }
    return bootstrap();
  }
}

class LevelClient extends OfflineDemoClient {
  LevelClient(this.level);
  final String level;
  @override
  Future<DaemonStatus> daemonStatus() async => DaemonStatus(
    productVersion: 'offline-demo',
    buildId: 'offline-demo',
    pid: 1,
    status: 'running',
    mode: 'desktop',
    logLevel: level,
  );
}

void main() {
  test(
    'close during restart awaits operation and prevents late restore',
    () async {
      final old = LevelClient('info');
      final boot = RestartBootstrap()..wait = Completer<void>();
      var connects = 0;
      final c = GuiController(
        bootstrapper: boot,
        connector: (_) async {
          connects++;
          return old;
        },
      );
      await c.start();
      final restart = c.restartCore('debug');
      await Future<void>.delayed(Duration.zero);
      final closing = c.close();
      boot.wait!.complete();
      await closing;
      await restart;
      expect(connects, 1);
      c.dispose();
      old.dispose();
    },
  );

  testWidgets(
    'pending level shows restart left of selector and reverting hides it',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(960, 1200));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final client = LevelClient('info');
      final boot = RestartBootstrap();
      final controller = GuiController(
        bootstrapper: boot,
        connector: (_) async => client,
      );
      await controller.start();
      final preferences = Appearance();
      await preferences.initialize();
      await tester.pumpWidget(
        AppearanceScope(
          appearance: preferences,
          child: MaterialApp(
            home: Scaffold(
              body: AdvancedPage(controller: controller, onBack: () {}),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.widgetWithText(OutlinedButton, '重启核心'), findsNothing);
      preferences.logLevel = 'debug';
      await tester.pumpAndSettle();
      final restart = find.widgetWithText(OutlinedButton, '重启核心');
      expect(restart, findsOneWidget);
      expect(
        tester.getRect(restart).right,
        lessThanOrEqualTo(
          tester.getRect(find.byType(DropdownButtonFormField<String>)).left,
        ),
      );
      await tester.ensureVisible(restart);
      await tester.tap(restart);
      await tester.pumpAndSettle();
      await tester.tap(find.text('取消'));
      await tester.pumpAndSettle();
      expect(boot.calls, 0);
      preferences.logLevel = 'info';
      await tester.pumpAndSettle();
      expect(find.widgetWithText(OutlinedButton, '重启核心'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      await controller.close();
      controller.dispose();
      client.dispose();
      preferences.dispose();
    },
  );

  for (final connected in [true, false]) {
    test('restart restores previous maintain intent $connected', () async {
      final old = LevelClient('info');
      if (!connected) old.selectScenario('suspended');
      final fresh = LevelClient('debug')..selectScenario('suspended');
      final boot = RestartBootstrap();
      var connects = 0;
      final c = GuiController(
        bootstrapper: boot,
        connector: (_) async => connects++ == 0 ? old : fresh,
      );
      await c.start();
      expect(c.runningLogLevel, 'info');
      expect(await c.restartCore('debug'), isTrue);
      expect(boot.calls, 1);
      expect(boot.level, 'debug');
      expect(c.runningLogLevel, 'debug');
      expect(
        fresh.operations
            .where((v) => v == 'session.start_configuration')
            .length,
        connected ? 1 : 0,
      );
      await c.close();
      c.dispose();
      old.dispose();
      fresh.dispose();
    });
  }
  test('failed restart does not reconnect or automatically restart', () async {
    final old = LevelClient('info');
    final boot = RestartBootstrap()..fail = true;
    var connects = 0;
    final c = GuiController(
      bootstrapper: boot,
      connector: (_) async {
        connects++;
        return old;
      },
      reconnectDelay: const Duration(milliseconds: 1),
    );
    await c.start();
    expect(await c.restartCore('debug'), isFalse);
    await Future<void>.delayed(const Duration(milliseconds: 30));
    expect(boot.calls, 1);
    expect(connects, 1);
    expect(c.notice, contains('失败'));
    await c.close();
    c.dispose();
    old.dispose();
  });
}
