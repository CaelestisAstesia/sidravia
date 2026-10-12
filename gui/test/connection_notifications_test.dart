import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/desktop/connection_notifications.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/desktop/system_notifications.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/gui_preferences.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

final base = DateTime.utc(2026, 10, 12, 10);
SessionSummary session(
  SessionState state,
  int revision, {
  SessionIntent intent = SessionIntent.maintainAuthentication,
  DateTime? established,
  String? reason,
  String? failure,
  String id = 'test-session',
}) => SessionSummary(
  id: id,
  displayName: '学校',
  accountName: 'private-user',
  state: state,
  intent: intent,
  revision: BigInt.from(revision),
  authenticationEstablishedAt: established,
  stateReason: reason == null
      ? null
      : SessionStateReason(code: reason, description: 'raw secret data'),
  lastAuthenticationFailure: failure == null
      ? null
      : SessionAuthenticationFailure(
          code: failure,
          description: 'raw-secret-account',
          handlingRecommendation: 'raw-private-error',
        ),
  updatedAt: base.subtract(const Duration(seconds: 1)),
);
void main() {
  test('success requires fresh established fact, duplicates and old revisions ignored', () {
    final p = ConnectionNotificationPolicy(startedAt: base);
    expect(
      p.observe(
        daemonPid: 1,
        session: session(
          SessionState.authenticated,
          10,
          established: base.subtract(const Duration(days: 1)),
        ),
        now: base,
      ),
      isEmpty,
    );
    expect(
      p.observe(
        daemonPid: 1,
        session: session(SessionState.authenticating, 9),
        now: base,
      ),
      isEmpty,
    );
    final event = p.observe(
      daemonPid: 1,
      session: session(
        SessionState.authenticated,
        11,
        established: base.add(const Duration(seconds: 1)),
      ),
      now: base,
    );
    expect(event.single.kind, ConnectionNoticeKind.success);
    expect(event.single.allowed(GuiNotificationMode.abnormal), isFalse);
    expect(event.single.allowed(GuiNotificationMode.all), isTrue);
    expect(event.single.allowed(GuiNotificationMode.silent), isFalse);
    expect(
      p.observe(
        daemonPid: 1,
        session: session(SessionState.authenticated, 11, established: base),
        now: base,
      ),
      isEmpty,
    );
  });
  test('short interruption emits recovery only, not retry attempts', () {
    final p = ConnectionNotificationPolicy(startedAt: base);
    p.observe(
      daemonPid: 1,
      session: session(
        SessionState.authenticated,
        1,
        established: base.subtract(const Duration(days: 1)),
      ),
      now: base,
    );
    p.observe(
      daemonPid: 1,
      session: session(SessionState.waitingBeforeRetry, 2),
      now: base,
    );
    expect(p.due(base.add(const Duration(seconds: 4))), isEmpty);
    final recovered = p.observe(
      daemonPid: 1,
      session: session(
        SessionState.authenticated,
        3,
        established: base.add(const Duration(seconds: 4)),
      ),
      now: base.add(const Duration(seconds: 4)),
    );
    expect(recovered.single.title, '已重新连接');
    expect(recovered.single.kind, ConnectionNoticeKind.recovery);
    expect(recovered.single.allowed(GuiNotificationMode.abnormal), isTrue);
    expect(p.nextDeadline, isNull);
    expect(p.due(base.add(const Duration(minutes: 1))), isEmpty);
  });
  test('one interruption and one delayed notice, not per retry revision', () {
    final p = ConnectionNotificationPolicy(startedAt: base);
    p.observe(
      daemonPid: 1,
      session: session(
        SessionState.authenticated,
        1,
        established: base.subtract(const Duration(days: 1)),
      ),
      now: base,
    );
    p.observe(
      daemonPid: 1,
      session: session(SessionState.waitingForNetwork, 2),
      now: base,
    );
    expect(p.due(base.add(const Duration(seconds: 5))).single.title, '连接已中断');
    for (var revision = 3; revision < 8; revision++) {
      expect(
        p.observe(
          daemonPid: 1,
          session: session(SessionState.waitingBeforeRetry, revision),
          now: base.add(Duration(seconds: revision)),
        ),
        isEmpty,
      );
    }
    expect(p.due(base.add(const Duration(seconds: 20))), isEmpty);
    expect(p.due(base.add(const Duration(seconds: 30))).single.title, '暂未恢复连接');
    expect(p.due(base.add(const Duration(minutes: 5))), isEmpty);
  });
  test('blocked exit into retry is not recovery and error body is curated', () {
    final p = ConnectionNotificationPolicy(startedAt: base);
    p.observe(
      daemonPid: 1,
      session: session(SessionState.authenticating, 1),
      now: base,
    );
    final error = p.observe(
      daemonPid: 1,
      session: session(
        SessionState.blockedByError,
        2,
        failure: 'authentication_rejected',
      ),
      now: base,
    );
    expect(error.single.title, '认证失败');
    expect(error.single.body, '认证被服务器拒绝，点击查看处理建议');
    expect(error.single.body, isNot(contains('raw')));
    expect(
      p.observe(
        daemonPid: 1,
        session: session(SessionState.authenticating, 3),
        now: base,
      ),
      isEmpty,
    );
    expect(
      p.observe(
        daemonPid: 1,
        session: session(
          SessionState.blockedByError,
          4,
          failure: 'authentication_rejected',
        ),
        now: base,
      ),
      isEmpty,
    );
    expect(p.due(base.add(const Duration(minutes: 1))), isEmpty);
    final recovered = p.observe(
      daemonPid: 1,
      session: session(SessionState.authenticated, 5, established: base),
      now: base,
    );
    expect(recovered.single.title, '已连接');
    expect(recovered.single.kind, ConnectionNoticeKind.recovery);
  });
  test(
    'old startup error stays quiet and PID/session change cancel old episodes',
    () {
      final p = ConnectionNotificationPolicy(startedAt: base);
      expect(
        p.observe(
          daemonPid: 1,
          session: session(SessionState.blockedByError, 1),
          now: base,
        ),
        isEmpty,
      );
      p.observe(
        daemonPid: 1,
        session: session(
          SessionState.authenticated,
          2,
          established: base.subtract(const Duration(days: 1)),
        ),
        now: base,
      );
      p.observe(
        daemonPid: 1,
        session: session(SessionState.waitingForNetwork, 3),
        now: base,
      );
      expect(
        p.observe(
          daemonPid: 2,
          session: session(
            SessionState.authenticated,
            1,
            established: base.subtract(const Duration(days: 1)),
          ),
          now: base,
        ),
        isEmpty,
      );
      expect(p.nextDeadline, isNull);
      expect(
        p.observe(
          daemonPid: 2,
          session: session(SessionState.blockedByError, 1, id: 'new'),
          now: base,
        ),
        isEmpty,
      );
    },
  );
  for (final stopping in [SessionState.suspended, SessionState.stopping]) {
    test('explicit stop cancels pending outage for $stopping', () {
      final p = ConnectionNotificationPolicy(startedAt: base);
      p.observe(
        daemonPid: 1,
        session: session(SessionState.authenticated, 1, established: base),
        now: base,
      );
      p.observe(
        daemonPid: 1,
        session: session(SessionState.waitingForNetwork, 2),
        now: base,
      );
      expect(
        p.observe(
          daemonPid: 1,
          session: session(
            stopping,
            3,
            intent: SessionIntent.suspendAuthentication,
          ),
          now: base,
        ),
        isEmpty,
      );
      expect(p.nextDeadline, isNull);
      expect(p.due(base.add(const Duration(minutes: 1))), isEmpty);
    });
  }
  test('automatic reconnect disabled gives an actionable message, no retries promised', () {
    final p = ConnectionNotificationPolicy(startedAt: base);
    p.observe(
      daemonPid: 1,
      session: session(SessionState.authenticated, 1, established: base),
      now: base,
    );
    final result = p.observe(
      daemonPid: 1,
      session: session(
        SessionState.suspended,
        2,
        reason: 'automatic_reconnect_disabled',
      ),
      now: base,
    );
    expect(result.single.body, '自动重连已关闭，点击查看');
    expect(p.nextDeadline, isNull);
  });
  test('first authentication wait is not falsely called a lost connection', () {
    final p = ConnectionNotificationPolicy(startedAt: base);
    p.observe(
      daemonPid: 1,
      session: session(SessionState.authenticating, 1),
      now: base,
    );
    p.observe(
      daemonPid: 1,
      session: session(SessionState.waitingBeforeRetry, 2),
      now: base,
    );
    expect(p.due(base.add(const Duration(seconds: 5))), isEmpty);
    expect(p.due(base.add(const Duration(seconds: 30))).single.title, '暂未连接');
  });
  test('late deadline callback emits only useful current notice', () {
    final p = ConnectionNotificationPolicy(startedAt: base);
    p.observe(
      daemonPid: 1,
      session: session(SessionState.authenticated, 1, established: base),
      now: base,
    );
    p.observe(
      daemonPid: 1,
      session: session(SessionState.waitingForNetwork, 2),
      now: base,
    );
    expect(p.due(base.add(const Duration(minutes: 1))).single.title, '暂未恢复连接');
  });
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'shared fresh visibility query does not invalidate two consumers',
    () async {
      const channel = MethodChannel(desktopPresenceChannelName);
      final reply = Completer<Object?>();
      var queries = 0;
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      messenger.setMockMethodCallHandler(channel, (call) async {
        if (call.method == 'initialize') return 'primary';
        if (call.method == 'getVisibility') {
          queries++;
          return queries == 1 ? false : reply.future;
        }
        return null;
      });
      final desktop = DesktopPresence(channel: channel);
      await desktop.initialize();
      final first = desktop.refreshNotificationVisibility(),
          second = desktop.refreshNotificationVisibility();
      reply.complete(false);
      expect(await first, isFalse);
      expect(await second, isFalse);
      expect(queries, 2);
      desktop.dispose();
      messenger.setMockMethodCallHandler(channel, null);
    },
  );
  test(
    'silent controller consumes success and level change does not replay it',
    () async {
      const deskChannel = MethodChannel(desktopPresenceChannelName);
      const notifyChannel = MethodChannel(systemNotificationsChannelName);
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      final calls = <MethodCall>[];
      messenger.setMockMethodCallHandler(
        deskChannel,
        (call) async => call.method == 'initialize' ? 'primary' : false,
      );
      messenger.setMockMethodCallHandler(notifyChannel, (call) async {
        calls.add(call);
        return call.method == 'show' ? 'submitted' : null;
      });
      final desktop = DesktopPresence(channel: deskChannel);
      await desktop.initialize();
      final prefs = GuiPreferences();
      await prefs.initialize();
      prefs.notificationMode = GuiNotificationMode.silent;
      final service = SystemNotifications(
        channel: notifyChannel,
        platform: TargetPlatform.windows,
      );
      final client = OfflineDemoClient();
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await c.start();
      final owner = ConnectionNotificationController(
        preferences: prefs,
        desktop: desktop,
        notifications: service.slot(SystemNotificationCategory.connection),
        startedAt: DateTime.utc(2000),
        onOpenDetails: () async {},
      );
      await owner.start();
      owner.updateCapabilities(c.capabilities);
      await Future<void>.delayed(Duration.zero);
      prefs.notificationMode = GuiNotificationMode.all;
      await prefs.settled;
      owner.updateCapabilities(c.capabilities);
      await Future<void>.delayed(Duration.zero);
      expect(calls.where((call) => call.method == 'show'), isEmpty);
      await owner.close();
      owner.dispose();
      await service.dispose();
      desktop.dispose();
      await c.close();
      c.dispose();
      prefs.dispose();
      messenger.setMockMethodCallHandler(deskChannel, null);
      messenger.setMockMethodCallHandler(notifyChannel, null);
    },
  );
  test(
    'new visibility context supersedes old answer without clearing new request',
    () async {
      const channel = MethodChannel(desktopPresenceChannelName);
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      final old = Completer<Object?>();
      var queries = 0;
      messenger.setMockMethodCallHandler(channel, (call) async {
        if (call.method == 'initialize') return 'primary';
        if (call.method == 'getVisibility') {
          queries++;
          return queries == 2 ? old.future : false;
        }
        return null;
      });
      final desktop = DesktopPresence(channel: channel);
      await desktop.initialize();
      final first = desktop.refreshNotificationVisibility(context: Object());
      final context = Object();
      expect(
        await desktop.refreshNotificationVisibility(context: context),
        isFalse,
      );
      old.complete(true);
      expect(await first, isNull);
      expect(desktop.isMainWindowVisible, isFalse);
      desktop.dispose();
      messenger.setMockMethodCallHandler(channel, null);
    },
  );
  for (final closing in [false, true]) {
    test(
      'pending visibility cannot send after ${closing ? 'close' : 'mute'}',
      () async {
        const dc = MethodChannel(desktopPresenceChannelName);
        const nc = MethodChannel(systemNotificationsChannelName);
        final messenger =
            TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
        final visibility = Completer<bool>();
        var shows = 0;
        messenger.setMockMethodCallHandler(
          dc,
          (call) async =>
              call.method == 'initialize' ? 'primary' : visibility.future,
        );
        messenger.setMockMethodCallHandler(nc, (call) async {
          if (call.method == 'show') shows++;
          return call.method == 'show' ? 'submitted' : null;
        });
        final desktop = DesktopPresence(channel: dc);
        await desktop.initialize();
        final prefs = GuiPreferences();
        await prefs.initialize();
        prefs.notificationMode = GuiNotificationMode.all;
        final service = SystemNotifications(
          channel: nc,
          platform: TargetPlatform.windows,
        );
        final client = OfflineDemoClient();
        final c = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client,
        );
        await c.start();
        final owner = ConnectionNotificationController(
          preferences: prefs,
          desktop: desktop,
          notifications: service.slot(SystemNotificationCategory.connection),
          startedAt: DateTime.utc(2000),
          onOpenDetails: () async {},
        );
        await owner.start();
        owner.updateCapabilities(c.capabilities);
        await Future<void>.delayed(Duration.zero);
        final close = closing ? owner.close() : null;
        if (!closing) prefs.notificationMode = GuiNotificationMode.silent;
        visibility.complete(false);
        for (var n = 0; n < 8; n++) {
          await Future<void>.delayed(Duration.zero);
        }
        if (close != null) await close;
        expect(shows, 0);
        await owner.close();
        owner.dispose();
        await service.dispose();
        desktop.dispose();
        await c.close();
        c.dispose();
        prefs.dispose();
        messenger.setMockMethodCallHandler(dc, null);
        messenger.setMockMethodCallHandler(nc, null);
      },
    );
  }
  for (final mode in GuiNotificationMode.values) {
    for (final visible in [true, false]) {
      test(
        'fresh success honors $mode and foreground=$visible, click has no auth side effects',
        () async {
          const dc = MethodChannel(desktopPresenceChannelName),
              nc = MethodChannel(systemNotificationsChannelName);
          final messenger =
              TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
          final calls = <MethodCall>[];
          messenger.setMockMethodCallHandler(
            dc,
            (call) async => call.method == 'initialize' ? 'primary' : visible,
          );
          messenger.setMockMethodCallHandler(nc, (call) async {
            calls.add(call);
            return call.method == 'show' ? 'submitted' : null;
          });
          final desktop = DesktopPresence(channel: dc);
          await desktop.initialize();
          final prefs = GuiPreferences();
          await prefs.initialize();
          prefs.notificationMode = mode;
          final service = SystemNotifications(
            channel: nc,
            platform: TargetPlatform.windows,
          );
          final client = OfflineDemoClient();
          final c = GuiController(
            bootstrapper: OfflineDemoBootstrap(),
            connector: (_) async => client,
          );
          await c.start();
          var opens = 0;
          final owner = ConnectionNotificationController(
            preferences: prefs,
            desktop: desktop,
            notifications: service.slot(SystemNotificationCategory.connection),
            startedAt: DateTime.utc(2000),
            onOpenDetails: () async {
              opens++;
            },
          );
          await owner.start();
          owner.updateCapabilities(c.capabilities);
          for (var n = 0; n < 8; n++) {
            await Future<void>.delayed(Duration.zero);
          }
          final shows = calls.where((call) => call.method == 'show').toList();
          expect(
            shows,
            hasLength(mode == GuiNotificationMode.all && !visible ? 1 : 0),
          );
          if (shows.isNotEmpty) {
            final payload = shows.single.arguments as Map;
            final operations = List<String>.of(client.operations);
            await messenger.handlePlatformMessage(
              nc.name,
              const StandardMethodCodec().encodeMethodCall(
                MethodCall('clicked', {
                  'category': 'connection',
                  'token': payload['token'],
                }),
              ),
              (_) {},
            );
            expect(opens, 1);
            expect(client.operations, operations);
          }
          await owner.close();
          owner.dispose();
          await service.dispose();
          desktop.dispose();
          await c.close();
          c.dispose();
          prefs.dispose();
          messenger.setMockMethodCallHandler(dc, null);
          messenger.setMockMethodCallHandler(nc, null);
        },
      );
    }
  }
}
