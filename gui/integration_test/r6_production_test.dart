import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/bootstrap/process_gui_bootstrap.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/web_socket_ipc_client.dart';
import 'package:sidravia_gui/main.dart' as production;

// Only the owned portable bundle prepared by r6_windows_ipc.ps1 is allowed.
// No connector, bootstrap, preferences or data is injected into production main.
void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets('Windows production main real daemon round-trip', (tester) async {
    expect(Platform.isWindows, isTrue);
    final namespace = Platform.environment['SIDRAVIA_NAMESPACE'];
    expect(namespace, 'r6-local-20261006');
    final root = File(Platform.resolvedExecutable).parent;
    expect(File('${root.path}/sidravia.portable').existsSync(), isTrue);
    final directory = Directory('${root.path}/namespaces/$namespace');
    final runtime = File('${directory.path}/runtime.json');
    final events = <String>[];
    GuiController? controller;
    try {
      await production.main();
      await tester.pump();
      controller = tester
          .widget<SidraviaApp>(find.byType(SidraviaApp))
          .controller;
      final c = controller;
      await _until(
        tester,
        () => !c.busy && c.state != GuiConnectionState.bootstrapping,
      );
      expect(c.state, GuiConnectionState.ready, reason: c.failure?.code);
      final initial = c.snapshot!;
      expect(initial.daemon.pid, greaterThan(0));
      expect(initial.daemon.mode, 'desktop');
      expect(initial.daemon.desktopOwnerPid, pid);
      expect(initial.daemon.buildId, 'dev');
      expect(initial.configurations, isEmpty);
      expect(initial.sessions, isEmpty);
      expect(initial.profiles.single.id, 'r6-loopback');
      expect(c.bootstrapper, isA<ProcessGuiBootstrap>());
      events.add(
        'production-main/bootstrap/real-snapshot pid=${initial.daemon.pid} owner=$pid',
      );
      final missing = await ProcessGuiBootstrap(
        resolvedExecutable: () => '${root.path}/missing/sidravia.exe',
      ).bootstrap();
      expect(missing.failure?.code, 'bootstrap_failed');
      events.add('missing-ctl/actual-process-launch-failure');
      final boot = await c.bootstrapper.bootstrap();
      expect(boot.isSuccess, isTrue);
      expect(boot.value!.daemonPid, initial.daemon.pid);
      for (final invalidBuild in [false, true]) {
        final rejected = GuiBootstrap(
          endpoint: boot.value!.endpoint,
          token: invalidBuild ? boot.value!.token : '0' * 64,
          productVersion: boot.value!.productVersion,
          buildId: invalidBuild ? 'r6-mismatch' : boot.value!.buildId,
          daemonPid: boot.value!.daemonPid,
          mode: 'desktop',
        );
        await expectLater(
          WebSocketIpcClient.connect(rejected),
          throwsA(
            isA<IpcTransportException>().having(
              (e) => e.code,
              'code',
              'ipc_handshake_failed',
            ),
          ),
        );
      }
      events.add('actual-daemon-handshake-401-and-409');
      expect(
        await c.createConfiguration(
          institutionProfileId: 'r6-loopback',
          username: 'r6-fictional',
          password: 'fictional-only',
        ),
        isTrue,
      );
      await tester.pump();
      var configuration = c.snapshot!.configurations.single;
      final configurationId = configuration.id;
      expect(configuration.credentialStored, isTrue);
      expect(configuration.storageProtection, 'protected');
      expect(configuration.autoLogin, isFalse);
      expect(configuration.autoReconnect, isTrue);
      expect(find.text('r6-fictional'), findsWidgets);
      expect(
        await c.updateConfiguration(
          configurationId: configurationId,
          institutionProfileId: 'r6-loopback',
          username: 'r6-updated',
        ),
        isTrue,
      );
      await tester.pump();
      expect(c.snapshot!.configurations.single.username, 'r6-updated');
      expect(find.text('r6-updated'), findsWidgets);
      expect(
        await c.setPassword(
          configurationId: configurationId,
          password: 'fictional-replaced',
        ),
        isTrue,
      );
      events.add(
        'configuration-create/update/setPassword/controller-refresh/render',
      );
      tester
          .widget<HomePage>(find.byType(HomePage))
          .onNavigate(AppPage.settings);
      await tester.pump();
      expect(find.byType(SettingsPage), findsOneWidget);
      for (final (key, read) in <(String, bool Function(ConfigurationSummary))>[
        ('auto-login-switch', (v) => v.autoLogin),
        ('auto-reconnect-switch', (v) => v.autoReconnect),
      ]) {
        for (var count = 0; count < 2; count++) {
          final before = read(c.snapshot!.configurations.single);
          await tester.ensureVisible(find.byKey(ValueKey(key)));
          await tester.tap(find.byKey(ValueKey(key)));
          await _until(
            tester,
            () => !c.busy && read(c.snapshot!.configurations.single) == !before,
          );
          expect(c.state, GuiConnectionState.ready);
        }
      }
      expect(c.snapshot!.configurations.single.autoLogin, isFalse);
      expect(c.snapshot!.configurations.single.autoReconnect, isTrue);
      events.add('actual-settings-switches/true-false/persistence-refresh');
      expect(
        await c.setAutoLogin(configurationId: 'wrong-target', autoLogin: true),
        isFalse,
      );
      expect(c.snapshot!.configurations.single.autoLogin, isFalse);
      expect(
        await c.updateConfiguration(
          configurationId: configurationId,
          institutionProfileId: 'missing',
          username: 'r6-updated',
        ),
        isFalse,
      );
      expect(c.state, GuiConnectionState.ready);
      expect(c.notice, '学校配置已不存在，请刷新。');
      events.add('capability-target-refusal/business-refusal-real-daemon');
      final previous = c.snapshot;
      await _until(tester, () => !identical(previous, c.snapshot) && !c.busy);
      expect(c.snapshot!.daemon.pid, initial.daemon.pid);
      expect(c.state, GuiConnectionState.ready);
      events.add('poll/same-daemon/stable-ready');
      expect(await c.startConfiguration(configurationId), isTrue);
      final sessionId = c.snapshot!.sessions.single.id;
      expect(c.snapshot!.sessions.single.configurationId, configurationId);
      expect(c.snapshot!.sessions.single.intent, 'maintain_authentication');
      expect(await c.stopSession(sessionId), isTrue);
      expect(c.snapshot!.sessions.single.intent, 'suspend_authentication');
      expect(c.snapshot!.sessions.single.state, 'suspended');
      configuration = c.snapshot!.configurations.single;
      expect(configuration.autoLogin, isFalse);
      expect(configuration.autoReconnect, isTrue);
      expect(await c.ensureSessionRunning(sessionId), isTrue);
      expect(c.snapshot!.sessions.single.id, sessionId);
      expect(c.snapshot!.sessions.single.intent, 'maintain_authentication');
      expect(await c.restartSession(sessionId), isTrue);
      expect(c.snapshot!.sessions.single.id, sessionId);
      expect(await c.stopSession(sessionId), isTrue);
      expect(await c.resetSession(sessionId), isTrue);
      expect(c.snapshot!.sessions, isEmpty);
      expect(
        await c.setAutoReconnect(
          configurationId: configurationId,
          autoReconnect: false,
        ),
        isTrue,
      );
      events.add(
        'session-start/stop/ensure/restart/remove/non-campus-loopback',
      );
      // Test-only observer stops the real daemon after an acknowledged mutation
      // and before refresh; all result/state bytes still come from actual IPC.
      final faultController = GuiController(
        bootstrapper: c.bootstrapper,
        connector: (value) async => _StopAfterMutation(
          await WebSocketIpcClient.connect(value),
          () => _until(tester, () => !runtime.existsSync()),
        ),
        pollDelay: const Duration(days: 1),
      );
      try {
        await faultController.start();
        expect(faultController.state, GuiConnectionState.ready);
        expect(
          await faultController.setAutoReconnect(
            configurationId: configurationId,
            autoReconnect: true,
          ),
          isFalse,
        );
        expect(faultController.state, GuiConnectionState.stale);
        expect(
          faultController.snapshot!.configurations.single.autoReconnect,
          isFalse,
        );
        expect(faultController.failure?.code, 'ipc_disconnected');
      } finally {
        faultController.dispose();
      }
      await _until(tester, () => c.state == GuiConnectionState.stale);
      expect(c.failure?.code, 'ipc_disconnected');
      expect(c.snapshot!.configurations.single.id, configurationId);
      events.add(
        'real-mutation-committed/daemon-stop-before-refresh/stale-old-snapshot',
      );
      final daemon = File('${root.path}/sidraviad.exe');
      final held = await daemon.rename('${daemon.path}.r6-held');
      try {
        final failed = await c.bootstrapper.bootstrap();
        expect(failed.isSuccess, isFalse);
        expect(failed.failure?.code, 'bootstrap_failed');
      } finally {
        await held.rename(daemon.path);
      }
      expect(runtime.existsSync(), isFalse);
      events.add('missing-daemon/actual-start-failure');
      // Actual daemon executable starts but cannot compose a malformed owned
      // Profile. The bootstrap must fail safely before becoming ready.
      final profile = File(
        '${directory.path}/institution-profiles/r6-loopback.json',
      );
      final profileBytes = await profile.readAsBytes();
      try {
        await profile.writeAsString('{}', flush: true);
        await c.retry();
        expect(c.state, GuiConnectionState.stale);
        expect(c.failure?.code, 'bootstrap_failed');
        expect(runtime.existsSync(), isFalse);
      } finally {
        await profile.writeAsBytes(profileBytes, flush: true);
      }
      events.add('real-daemon-starts-but-not-ready/safe-bootstrap-failure');
      await c.retry();
      expect(c.state, GuiConnectionState.ready, reason: c.failure?.code);
      expect(c.snapshot!.daemon.pid, isNot(initial.daemon.pid));
      expect(c.snapshot!.configurations.single.id, configurationId);
      expect(c.snapshot!.configurations.single.autoReconnect, isTrue);
      expect(
        await c.setAutoReconnect(
          configurationId: configurationId,
          autoReconnect: false,
        ),
        isTrue,
      );
      expect(c.snapshot!.configurations.single.autoReconnect, isFalse);
      expect(c.snapshot!.configurations.single.autoLogin, isFalse);
      expect(c.snapshot!.sessions, isEmpty);
      events.add(
        'daemon-restart/new-pid/acknowledged-mutation-restored/false-round-trip/new-session-generation',
      );
      final persistedBoot = await c.bootstrapper.bootstrap();
      final persistedControl = await WebSocketIpcClient.connect(
        persistedBoot.value!,
      );
      try {
        await persistedControl.daemonStop();
      } finally {
        await persistedControl.close();
      }
      await _until(
        tester,
        () => !runtime.existsSync() && c.state == GuiConnectionState.stale,
      );
      await c.retry();
      expect(c.state, GuiConnectionState.ready);
      expect(c.snapshot!.configurations.single.autoReconnect, isFalse);
      expect(c.snapshot!.configurations.single.autoLogin, isFalse);
      events.add('false-preferences-persist-across-real-daemon-generation');
      expect(await c.deleteConfiguration(configurationId), isTrue);
      expect(c.snapshot!.configurations, isEmpty);
      expect(c.capabilities.canCreate, isTrue);
      events.add('configuration-remove/round-trip');
      expect(await c.exitAndDisconnect(), isTrue);
      await _until(tester, () => !runtime.existsSync());
      events.add('explicit-controller-exit/daemon-stop/runtime-removed');
      binding.reportData = {
        'namespace': namespace,
        'realDaemon': true,
        'productionMain': true,
        'campusAuthentication': false,
        'events': events,
      };
      for (final event in events) {
        debugPrint('R6 PASS $event');
      }
    } finally {
      if (controller != null) {
        if (!await controller.exitAndDisconnect() && runtime.existsSync()) {
          final result = await controller.bootstrapper.bootstrap();
          if (result.isSuccess) {
            final client = await WebSocketIpcClient.connect(result.value!);
            try {
              await client.daemonStop();
            } finally {
              await client.close();
            }
          }
        }
        await _until(tester, () => !runtime.existsSync());
      }
      await tester.pumpWidget(const SizedBox());
      await _deleteWhenReleased(directory);
      expect(directory.existsSync(), isFalse);
    }
  }, timeout: const Timeout(Duration(minutes: 3)));
}

Future<void> _until(WidgetTester tester, bool Function() condition) async {
  final deadline = DateTime.now().add(const Duration(seconds: 20));
  while (!condition()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('R6 condition deadline exceeded');
    }
    await tester.pump(const Duration(milliseconds: 25));
  }
  await tester.pump();
}

// A deterministic fault observer, not a state simulator. Only the accessed
// methods are forwarded; an unexpected test call fails immediately.
class _StopAfterMutation implements SidraviaDesktopClient {
  _StopAfterMutation(this.real, this.waitStopped);
  final WebSocketIpcClient real;
  final Future<void> Function() waitStopped;
  @override
  Future<DaemonStatus> daemonStatus() => real.daemonStatus();
  @override
  Future<List<InstitutionProfile>> profileList() => real.profileList();
  @override
  Future<List<ConfigurationSummary>> configurationList() =>
      real.configurationList();
  @override
  Future<List<SessionSummary>> sessionList() => real.sessionList();
  @override
  Future<ConfigurationSummary> configurationSetAutoReconnect({
    required String configurationId,
    required bool autoReconnect,
  }) async {
    final result = await real.configurationSetAutoReconnect(
      configurationId: configurationId,
      autoReconnect: autoReconnect,
    );
    await real.daemonStop();
    await waitStopped();
    return result;
  }

  @override
  Future<void> close() => real.close();
  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw UnsupportedError('Unexpected fault-observer call');
}

// Runtime removal precedes final Windows log-handle release. Wait for that
// observable release, only for sharing violation32 and only in the owned namespace.
Future<void> _deleteWhenReleased(Directory directory) async {
  final deadline = DateTime.now().add(const Duration(seconds: 20));
  while (directory.existsSync()) {
    try {
      await directory.delete(recursive: true);
    } on FileSystemException catch (error) {
      if (error.osError?.errorCode != 32 || DateTime.now().isAfter(deadline)) {
        rethrow;
      }
      await Future<void>.delayed(const Duration(milliseconds: 25));
    }
  }
}
