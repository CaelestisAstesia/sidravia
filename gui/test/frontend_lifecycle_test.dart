import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class _DelayedSaveClient extends OfflineDemoClient {
  final updateGate = Completer<void>();
  final passwordGate = Completer<void>();
  final createGate = Completer<void>();
  String? submittedPassword;

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    bool allowInsecureStorage = false,
  }) async {
    submittedPassword = password;
    await updateGate.future;
    return super.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
      allowInsecureStorage: allowInsecureStorage,
    );
  }

  @override
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
  }) async {
    submittedPassword = password;
    await passwordGate.future;
    return super.configurationSetPassword(
      configurationId: configurationId,
      password: password,
    );
  }

  @override
  Future<ConfigurationSummary> configurationCreate({
    required String institutionProfileId,
    required String username,
    required String password,
  }) async {
    await createGate.future;
    return super.configurationCreate(
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
    );
  }
}

class _DisconnectClient extends OfflineDemoClient {
  bool disconnected = false;

  @override
  Future<DaemonStatus> daemonStatus() async {
    if (disconnected) throw const IpcTransportException('ipc_disconnected');
    return super.daemonStatus();
  }
}

Future<void> _mount(WidgetTester tester, GuiController controller) async {
  await tester.pumpWidget(
    MaterialApp(home: SidraviaShell(controller: controller)),
  );
  await tester.pumpAndSettle();
}

Future<void> _unmount(WidgetTester tester, GuiController controller) async {
  await tester.pumpWidget(const SizedBox.shrink());
  controller.dispose();
}

Future<void> _openConfiguration(WidgetTester tester) async {
  await tester.tap(find.byKey(const ValueKey('home-configuration-button')));
  await tester.pumpAndSettle();
}

String _username(WidgetTester tester) =>
    tester.widget<TextField>(find.byType(TextField).first).controller!.text;

String? _selectedProfile(WidgetTester tester) => tester
    .state<FormFieldState<String>>(find.byType(DropdownButtonFormField<String>))
    .value;

void main() {
  testWidgets('initial IPC failure can retry and coalesces pending retry', (
    tester,
  ) async {
    final client = OfflineDemoClient();
    final connected = Completer<OfflineDemoClient>();
    var attempts = 0;
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async {
        if (++attempts == 1) {
          throw const IpcTransportException('ipc_connection_failed');
        }
        return connected.future;
      },
      pollDelay: const Duration(days: 1),
    );
    await controller.start();
    await _mount(tester, controller);
    expect(find.text('尚未配置'), findsNothing);
    expect(find.text('重试连接'), findsOneWidget);
    await tester.tap(find.text('重试连接'));
    await tester.pumpAndSettle();
    expect(controller.busy, isTrue);
    expect(
      tester.widget<HomeView>(find.byType(HomeView)).data.onPrimary,
      isNull,
    );
    expect(attempts, 2);
    connected.complete(client);
    await tester.pumpAndSettle();
    expect(controller.state, GuiConnectionState.ready);
    expect(find.text('已连接'), findsOneWidget);
    await _unmount(tester, controller);
  });

  testWidgets('stale IPC retains account identity and retry restores actions', (
    tester,
  ) async {
    final oldClient = _DisconnectClient();
    final newClient = OfflineDemoClient();
    var attempts = 0;
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => ++attempts == 1 ? oldClient : newClient,
      pollDelay: const Duration(milliseconds: 50),
    );
    await controller.start();
    await _mount(tester, controller);
    oldClient.disconnected = true;
    await tester.pump(const Duration(milliseconds: 60));
    await tester.pumpAndSettle();
    expect(controller.state, GuiConnectionState.stale);
    expect(oldClient.closed, isTrue);
    expect(find.text('student01'), findsOneWidget);
    expect(find.text('尚未配置'), findsNothing);
    expect(find.text('已连接'), findsNothing);
    expect(find.text('断开连接'), findsNothing);
    await tester.tap(find.text('重试连接'));
    await tester.pumpAndSettle();
    expect(controller.state, GuiConnectionState.ready);
    expect(attempts, 2);
    expect(find.text('断开连接'), findsOneWidget);
    await _unmount(tester, controller);
  });

  testWidgets('unsupported platform offers no IPC retry', (tester) async {
    final controller = GuiController(
      bootstrapper: const UnsupportedGuiBootstrapper(),
    );
    await controller.start();
    await _mount(tester, controller);
    expect(controller.state, GuiConnectionState.unsupported);
    expect(find.text('重试连接'), findsNothing);
    expect(
      tester.widget<HomeView>(find.byType(HomeView)).data.onPrimary,
      isNull,
    );
    expect(find.text('尚未配置'), findsNothing);
    await _unmount(tester, controller);
  });

  for (final creating in [false, true]) {
    testWidgets('late bootstrap hydrates form, creating=$creating', (
      tester,
    ) async {
      final client = OfflineDemoClient();
      if (creating) client.selectScenario('empty');
      final connected = Completer<OfflineDemoClient>();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) => connected.future,
        pollDelay: const Duration(days: 1),
      );
      final started = controller.start();
      await _mount(tester, controller);
      await _openConfiguration(tester);
      expect(_username(tester), isEmpty);
      connected.complete(client);
      await started;
      await tester.pumpAndSettle();
      expect(_selectedProfile(tester), 'jlu');
      expect(_username(tester), creating ? '' : 'student01');
      await tester.enterText(find.byType(TextField).first, 'edited-account');
      await tester.enterText(find.byType(TextField).last, 'fake-password');
      await tester.tap(find.text(creating ? '保存配置' : '保存更改'));
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);
      expect(
        controller.snapshot!.configurations.single.username,
        'edited-account',
      );
      await _unmount(tester, controller);
    });
  }

  testWidgets('normal polls preserve unsaved account and password', (
    tester,
  ) async {
    final client = OfflineDemoClient();
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(milliseconds: 50),
    );
    await controller.start();
    await _mount(tester, controller);
    await _openConfiguration(tester);
    await tester.enterText(find.byType(TextField).first, 'unsaved-account');
    await tester.enterText(find.byType(TextField).last, 'unsaved-password');
    await tester.pump(const Duration(milliseconds: 60));
    await tester.pumpAndSettle();
    expect(_username(tester), 'unsaved-account');
    expect(_selectedProfile(tester), 'jlu');
    expect(
      tester.widget<TextField>(find.byType(TextField).last).controller!.text,
      'unsaved-password',
    );
    await _unmount(tester, controller);
  });

  for (final leaveDuringPassword in [false, true]) {
    testWidgets(
      'atomic save survives Escape, delayed frame=$leaveDuringPassword',
      (tester) async {
        final client = _DelayedSaveClient();
        final controller = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client,
          pollDelay: const Duration(days: 1),
        );
        await controller.start();
        await _mount(tester, controller);
        await _openConfiguration(tester);
        await tester.enterText(find.byType(TextField).first, 'saved-account');
        await tester.enterText(find.byType(TextField).last, 'fake-password');
        await tester.tap(find.text('保存更改'));
        await tester.pump();
        if (leaveDuringPassword) {
          await tester.pump(const Duration(milliseconds: 50));
          expect(client.submittedPassword, 'fake-password');
        }
        await tester.sendKeyEvent(LogicalKeyboardKey.escape);
        await tester.pumpAndSettle();
        expect(find.byType(ConfigurationPage), findsNothing);
        client.updateGate.complete();
        client.passwordGate.complete();
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull);
        expect(client.submittedPassword, 'fake-password');
        expect(
          controller.snapshot!.configurations.single.username,
          'saved-account',
        );
        expect(controller.busy, isFalse);
        expect(find.byType(HomePage), findsOneWidget);
        await _unmount(tester, controller);
      },
    );
  }

  testWidgets('first create survives Cancel while request is pending', (
    tester,
  ) async {
    final client = _DelayedSaveClient()..selectScenario('empty');
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    await controller.start();
    await _mount(tester, controller);
    await _openConfiguration(tester);
    await tester.enterText(find.byType(TextField).first, 'created-account');
    await tester.enterText(find.byType(TextField).last, 'fake-password');
    await tester.tap(find.text('保存配置'));
    await tester.pump();
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    client.createGate.complete();
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(
      controller.snapshot!.configurations.single.username,
      'created-account',
    );
    expect(controller.busy, isFalse);
    expect(find.byType(HomePage), findsOneWidget);
    await _unmount(tester, controller);
  });
}
