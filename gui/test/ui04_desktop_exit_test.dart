import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

const channel = MethodChannel(desktopPresenceChannelName);

Future<void> requestExit() async {
  await TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .handlePlatformMessage(
        desktopPresenceChannelName,
        const StandardMethodCodec().encodeMethodCall(
          const MethodCall('exitRequested'),
        ),
        (_) {},
      );
}

class ExitClient extends OfflineDemoClient implements SidraviaDesktopClient {
  ExitClient(this.events);
  final List<String> events;
  bool failStop = false;
  @override
  Future<DaemonStopResult> daemonStop() async {
    events.add('stop');
    if (failStop) throw const IpcRequestFailure('internal');
    return const DaemonStopResult(status: 'stopping');
  }
}

Future<(GuiController, ExitClient, DesktopPresence)> mount(
  WidgetTester tester,
  List<String> events, {
  String scenario = 'authenticated',
  bool failBegin = false,
  bool failStop = false,
}) async {
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  messenger.setMockMethodCallHandler(channel, (call) async {
    if (call.method == 'initialize') return 'primary';
    if (call.method == 'beginExit' ||
        call.method == 'cancelExit' ||
        call.method == 'destroy') {
      events.add(call.method);
    }
    if (call.method == 'beginExit' && failBegin) {
      throw PlatformException(code: 'failed');
    }
    return null;
  });
  final presence = DesktopPresence(channel: channel);
  await presence.initialize();
  final client = ExitClient(events)
    ..selectScenario(scenario)
    ..failStop = failStop;
  final controller = GuiController(
    bootstrapper: OfflineDemoBootstrap(),
    connector: (_) async => client,
    pollDelay: const Duration(days: 1),
  );
  await controller.start();
  await tester.pumpWidget(
    SidraviaApp(controller: controller, desktopPresence: presence),
  );
  await tester.pumpAndSettle();
  addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
  return (controller, client, presence);
}

Future<void> cleanup(
  WidgetTester tester,
  GuiController controller,
  ExitClient client,
) async {
  await tester.pumpWidget(const SizedBox());
  await controller.close();
  client.dispose();
}

void main() {
  for (final scenario in [
    'authenticated',
    'authenticating',
    'waiting_for_network',
    'waiting_before_retry',
  ]) {
    testWidgets('consent precedes exit timer and stop for $scenario', (
      tester,
    ) async {
      final events = <String>[];
      final (controller, client, _) = await mount(
        tester,
        events,
        scenario: scenario,
      );
      unawaited(requestExit());
      await tester.pumpAndSettle();
      expect(find.text('断开连接并退出？'), findsOneWidget);
      await tester.pump(const Duration(seconds: 7));
      expect(events, isEmpty);
      unawaited(requestExit());
      await tester.pump();
      expect(find.byType(AlertDialog), findsOneWidget);
      await tester.tap(find.text('取消'));
      await tester.pumpAndSettle();
      expect(events, ['cancelExit']);
      unawaited(requestExit());
      await tester.pumpAndSettle();
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();
      expect(events, ['cancelExit', 'cancelExit']);
      unawaited(requestExit());
      await tester.pumpAndSettle();
      await tester.tap(find.text('断开并退出'));
      await tester.pumpAndSettle();
      expect(events, [
        'cancelExit',
        'cancelExit',
        'beginExit',
        'stop',
        'destroy',
      ]);
      unawaited(requestExit());
      await tester.pump();
      expect(events.where((e) => e == 'stop'), hasLength(1));
      await cleanup(tester, controller, client);
    });
  }
  for (final failure in ['stop', 'begin']) {
    testWidgets('$failure failure keeps consent boundaries', (tester) async {
      final events = <String>[];
      final (controller, client, _) = await mount(
        tester,
        events,
        failStop: failure == 'stop',
        failBegin: failure == 'begin',
      );
      unawaited(requestExit());
      await tester.pumpAndSettle();
      await tester.tap(find.text('断开并退出'));
      await tester.pumpAndSettle();
      if (failure == 'stop') {
        expect(events, ['beginExit', 'stop', 'destroy']);
      } else {
        expect(events, ['beginExit', 'cancelExit']);
        expect(find.text('无法开始退出，连接保持不变，请重试。'), findsOneWidget);
      }
      await cleanup(tester, controller, client);
    });
  }
  testWidgets('disconnected exits directly after native acceptance', (
    tester,
  ) async {
    final events = <String>[];
    final (controller, client, _) = await mount(
      tester,
      events,
      scenario: 'suspended',
    );
    unawaited(requestExit());
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(events, ['beginExit', 'stop', 'destroy']);
    await cleanup(tester, controller, client);
  });
  testWidgets(
    'dirty exit prompt does not clear draft when either consent cancels',
    (tester) async {
      final events = <String>[];
      final (controller, client, _) = await mount(tester, events);
      await tester.tap(find.byKey(const ValueKey('home-configuration-button')));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, 'changed');
      unawaited(requestExit());
      await tester.pumpAndSettle();
      await tester.tap(find.text('继续编辑'));
      await tester.pumpAndSettle();
      expect(events, ['cancelExit']);
      unawaited(requestExit());
      await tester.pumpAndSettle();
      await tester.tap(find.text('放弃修改'));
      await tester.pumpAndSettle();
      expect(find.text('断开连接并退出？'), findsOneWidget);
      await tester.tap(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.text('取消'),
        ),
      );
      await tester.pumpAndSettle();
      expect(events, ['cancelExit', 'cancelExit']);
      expect(
        tester.widget<TextField>(find.byType(TextField).first).controller!.text,
        'changed',
      );
      expect(client.operations, isNot(contains('configuration.update')));
      await cleanup(tester, controller, client);
    },
  );
}
