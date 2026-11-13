import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';

import 'dart:ui' as ui;

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:sidravia_gui/main.dart' as production;
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/process_gui_bootstrap.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_window.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/platform/gui_settings_storage.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/desktop/system_notifications.dart';
import 'package:sidravia_gui/desktop/log_directory.dart';

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
    'Linux production main uses real CTL daemon IPC and desktop shell',
    (tester) async {
      expect(Platform.isLinux, isTrue);
      final namespace = Platform.environment['SIDRAVIA_NAMESPACE']!;
      expect(namespace, startsWith('linux-gui-test-'));
      final root = Directory(Platform.environment['SIDRAVIA_GUI_FIXTURE']!);
      expect(File('${root.path}/fixture-owner').readAsStringSync(), namespace);
      final runtime = File(
        '${Platform.environment['XDG_CONFIG_HOME']}/Sidravia/namespaces/$namespace/runtime.json',
      );
      final restoring = Platform.environment['SIDRAVIA_GUI_RESTORED'] == '1';
      await production.main([
        '--skip-auto-login',
        restoring ? '--start-hidden' : '--start-visible',
      ]);
      await tester.pump();
      final app = tester.widget<SidraviaApp>(find.byType(SidraviaApp));
      final c = app.controller;
      try {
        await nativeUntil(
          tester,
          () => c.state != GuiConnectionState.bootstrapping && !c.busy,
        );
        expect(c.state, GuiConnectionState.ready, reason: c.failure?.code);
        expect(c.snapshot!.daemon.desktopOwnerPid, pid);
        expect(c.snapshot!.daemon.mode, 'desktop');
        expect(c.snapshot!.profiles.single.id, 'linux-loopback');
        expect(c.snapshot!.configurations, isEmpty);
        expect(c.snapshot!.sessions, isEmpty);
        expect(app.desktopPresence!.enabled, isTrue);
        if (restoring) {
          expect(app.appearance!.value, ThemeMode.dark);
          expect(
            await app.desktopPresence!.refreshMainWindowVisibility(),
            isFalse,
          );
          await app.desktopPresence!.restoreMain();
          await nativeUntil(
            tester,
            () => app.desktopPresence!.isMainWindowVisible != false,
          );
        }
        expect(
          await app.desktopPresence!.refreshMainWindowVisibility(),
          isNot(false),
        );
        await app.desktopPresence!.configurePreferences(
          closeBehavior: GuiCloseBehavior.hideToTray,
          startHidden: true,
        );
        expect(
          await app.desktopPresence!.refreshMainWindowVisibility(),
          isNot(false),
        );
        expect(
          await c.createConfiguration(
            institutionProfileId: 'linux-loopback',
            username: 'fictional-linux',
            password: 'fictional-only',
          ),
          isTrue,
        );
        final id = c.snapshot!.configurations.single.id;
        expect(
          await c.updateConfiguration(
            configurationId: id,
            institutionProfileId: 'linux-loopback',
            username: 'fictional-updated',
          ),
          isTrue,
        );
        expect(c.snapshot!.configurations.single.username, 'fictional-updated');
        expect(
          await c.setPassword(
            configurationId: id,
            password: 'fictional-replaced',
          ),
          isTrue,
        );
        final before = c.snapshot!.daemon.pid;
        for (var i = 0; i < 3; i++) {
          final old = c.snapshot!.daemon.pid;
          expect(await c.restartCore('debug'), isTrue, reason: c.failure?.code);
          expect(c.snapshot!.daemon.pid, isNot(old));
          expect(c.snapshot!.configurations.single.id, id);
          expect(c.snapshot!.sessions, isEmpty);
          c.dismissNotice(c.notice!);
          expect(c.notice, isNull);
        }
        expect(c.snapshot!.daemon.pid, isNot(before));
        // Only loopback is present in the synthetic profile. Start/cancel exercises
        // real lifecycle without any campus endpoint or successful authentication.
        expect(await c.startConfiguration(id), isTrue);
        final session = c.snapshot!.sessions.single;
        expect(await c.stopSession(session.id), isTrue);
        expect(
          c.snapshot!.sessions.single.intent,
          SessionIntent.suspendAuthentication,
        );
        expect(await c.resetSession(session.id), isTrue);
        expect(await c.deleteConfiguration(id), isTrue);
        expect(c.snapshot!.configurations, isEmpty);
        final appearance = app.appearance!;
        appearance.value = ThemeMode.dark;
        await appearance.settled;
        final restored = await (await productionGuiSettingsStore(
          namespace: namespace,
        )).read();
        expect(restored!.appearanceMode, GuiAppearanceMode.dark);
        const window = MethodChannel('sidravia/window');
        await tester.pump();
        expect(find.byKey(const ValueKey('window-top')), findsOneWidget);
        final frame = await window.invokeMapMethod<String, dynamic>(
          'diagnostics',
        );
        expect(frame!['customFrame'], isTrue);
        await tester.runAsync(() async {
          final deadline = DateTime.now().add(const Duration(seconds: 5));
          while ((await window.invokeMapMethod<String, dynamic>(
                'diagnostics',
              ))?['waylandWindowIcon'] ==
              'pending') {
            if (DateTime.now().isAfter(deadline)) {
              fail('Wayland icon protocol deadline');
            }
            await Future<void>.delayed(const Duration(milliseconds: 25));
          }
        });
        expect(
          (await window.invokeMapMethod<String, dynamic>(
            'diagnostics',
          ))?['waylandWindowIcon'],
          'submitted',
        );
        await tester.tap(find.byTooltip('最大化'));
        await waitWindowState(tester, window, true);
        await until(tester, () => find.byTooltip('还原').evaluate().isNotEmpty);
        expect(find.byTooltip('还原'), findsOneWidget);
        await tester.tap(find.byTooltip('还原'));
        await waitWindowState(tester, window, false);
        await tester.pump();
        await window.invokeMethod<void>('setDarkMode', true);
        await tester.tap(find.byTooltip('最小化'));
        await nativeUntil(
          tester,
          () => app.desktopPresence!.isMainWindowVisible != true,
        );
        expect(
          await app.desktopPresence!.refreshMainWindowVisibility(),
          isNot(true),
        );
        await app.desktopPresence!.restoreMain();
        await nativeUntil(
          tester,
          () => app.desktopPresence!.isMainWindowVisible != false,
        );
        await tester.pump();
        await tester.tap(find.byTooltip('关闭'));
        await nativeUntil(
          tester,
          () => app.desktopPresence!.isMainWindowVisible == false,
        );
        await app.desktopPresence!.restoreMain();
        await nativeUntil(
          tester,
          () => app.desktopPresence!.isMainWindowVisible != false,
        );
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 400));
        final boundary = tester.renderObject<RenderRepaintBoundary>(
          find.byType(RepaintBoundary).first,
        );
        final image = await boundary.toImage();
        final png = await image.toByteData(format: ui.ImageByteFormat.png);
        await File('${root.path}/gui-${restoring ? 'restored' : 'initial'}.png')
            .writeAsBytes(png!.buffer.asUint8List());
        image.dispose();
        final announcements = SchoolAnnouncementWindow();
        final port = Platform.environment['SIDRAVIA_GUI_HTTP_PORT'];
        expect(
          await announcements.open(
            AnnouncementMetadata(
              url: 'http://127.0.0.1:$port/notice',
              clientWidth: 600,
              clientHeight: 460,
            ),
            namespace: namespace,
          ),
          SchoolAnnouncementOpenResult.opened,
        );
        await until(
          tester,
          () => File('${root.path}/notice-requested').existsSync(),
        );
        expect(await announcements.close(), isTrue);
        await announcements.dispose();
        final notifications = app.systemNotifications!;
        final submitted = await notifications
            .slot(SystemNotificationCategory.connection)
            .show(
              token: '1' * 32,
              title: 'Sidravia 隔离测试',
              body: '虚构配置；未连接校园网',
              namespace: namespace,
            );
        expect(submitted, SystemNotificationResult.submitted);
        await notifications
            .slot(SystemNotificationCategory.connection)
            .dismiss();
        expect(
          (await ProcessGuiBootstrap(
            resolvedExecutable: () => '${root.path}/missing/gui',
          ).bootstrap()).failure?.code,
          'bootstrap_failed',
        );
        if (!restoring &&
            Platform.environment['SIDRAVIA_GUI_TEST_OPEN_DESKTOP'] == '1') {
          final logs = DesktopLogDirectory();
          expect(await logs.open(), isTrue);
          await logs.close();
          await window.invokeMethod<void>('openProjectPage');
        }
        expect(await c.exitAndDisconnect(), isTrue);
        await until(tester, () => !runtime.existsSync());
        binding.reportData = {
          'realDaemon': true,
          'productionMain': true,
          'namespace': namespace,
          'campusAuthentication': false,
        };
        debugPrint(
          'LINUX PASS production-main/configuration/restart-three-times/cancel/preferences/announcement-http/notification/exit',
        );
      } finally {
        await c.exitAndDisconnect();
        await tester.pumpWidget(const SizedBox());
      }
    },
    timeout: const Timeout(Duration(minutes: 3)),
  );
}

Future<void> until(WidgetTester tester, bool Function() condition) async {
  final deadline = DateTime.now().add(const Duration(seconds: 20));
  while (!condition()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('Linux fixture condition timeout');
    }
    await tester.pump(const Duration(milliseconds: 25));
  }
  await tester.pump();
}

Future<void> nativeUntil(WidgetTester tester, bool Function() condition) async {
  // A hidden/minimized native surface cannot complete tester.pump's frame.
  // Observe platform events with the real event loop instead.
  await tester.runAsync(() async {
    final deadline = DateTime.now().add(const Duration(seconds: 10));
    while (!condition()) {
      if (DateTime.now().isAfter(deadline)) fail('Native visibility deadline');
      await Future<void>.delayed(const Duration(milliseconds: 25));
    }
  });
}

Future<void> waitWindowState(
  WidgetTester tester,
  MethodChannel window,
  bool maximized,
) async {
  await tester.runAsync(() async {
    final deadline = DateTime.now().add(const Duration(seconds: 10));
    while ((await window.invokeMapMethod<String, dynamic>(
          'diagnostics',
        ))?['maximized'] !=
        maximized) {
      if (DateTime.now().isAfter(deadline)) {
        fail('Window maximize state deadline');
      }
      await Future<void>.delayed(const Duration(milliseconds: 25));
    }
  });
}
