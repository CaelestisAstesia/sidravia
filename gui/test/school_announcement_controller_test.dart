import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_notifications.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_window.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';

import 'school_announcement_policy_test.dart' as data;

Future<SchoolAnnouncementController> manualAnnouncements(
  WidgetTester tester,
  GuiController gui,
) async {
  final preferences = Appearance();
  await preferences.initialize();
  final desktop = DesktopPresence.disabled();
  final owner = SchoolAnnouncementController(
    preferences: preferences,
    desktop: desktop,
    window: SchoolAnnouncementWindow.disabled(),
    notifications: SchoolAnnouncementNotifications.disabled(),
    startedAt: DateTime.now(),
  );
  owner.updateCapabilities(gui.capabilities);
  await owner.start();
  addTearDown(() {
    expect(
      owner.available,
      isFalse,
      reason: 'Fixture must close while tester is active',
    );
    owner.dispose();
    preferences.dispose();
    desktop.dispose();
  });
  return owner;
}

/// Flush the zone that owns mock futures before awaiting their completion.
Future<void> pumpCleanup(
  WidgetTester tester,
  Future<void> Function() cleanup,
) async {
  var complete = false;
  Object? failure;
  StackTrace? failureStack;
  final future = cleanup();
  future.then<void>(
    (_) {
      complete = true;
    },
    onError: (Object error, StackTrace stack) {
      failure = error;
      failureStack = stack;
      complete = true;
    },
  );
  for (var i = 0; i < 20 && !complete; i++) {
    await tester.pump();
  }
  expect(
    complete,
    isTrue,
    reason: 'Fixture cleanup must finish within 20 pumps',
  );
  if (failure != null) Error.throwWithStackTrace(failure!, failureStack!);
  await future;
}

Future<void> closeShellOwners(WidgetTester tester) async {
  final shells = find.byType(SidraviaShell);
  if (shells.evaluate().isEmpty) return;
  final shell = tester.widget<SidraviaShell>(shells.first);
  await pumpCleanup(tester, () async {
    await shell.announcements?.close();
    await shell.controller.close();
  });
}

const windowChannel = MethodChannel(schoolAnnouncementChannelName);
const notificationChannel = MethodChannel(
  schoolAnnouncementNotificationsChannelName,
);
const desktopChannel = MethodChannel(desktopPresenceChannelName);
Future<void> callback(
  MethodChannel channel,
  String method,
  Object? value,
) async {
  await TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .handlePlatformMessage(
        channel.name,
        const StandardMethodCodec().encodeMethodCall(MethodCall(method, value)),
        (_) {},
      );
}

Future<void> settleWork() async {
  for (var i = 0; i < 8; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late Appearance preferences;
  late DesktopPresence desktop;
  late SchoolAnnouncementController owner;
  final opens = <MethodCall>[];
  final notices = <MethodCall>[];
  bool? visible;
  Object? windowReply;
  Object? notificationReply;
  setUp(() async {
    opens.clear();
    notices.clear();
    visible = true;
    windowReply = 'opened';
    notificationReply = 'submitted';
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(windowChannel, (call) async {
      if (call.method == 'open') {
        opens.add(call);
        return windowReply;
      }
      return null;
    });
    messenger.setMockMethodCallHandler(notificationChannel, (call) async {
      notices.add(call);
      return call.method == 'show' ? notificationReply : null;
    });
    messenger.setMockMethodCallHandler(
      desktopChannel,
      (call) async => switch (call.method) {
        'initialize' => 'primary',
        'getVisibility' => visible,
        _ => null,
      },
    );
    preferences = Appearance();
    await preferences.initialize();
    preferences.announcementFrequency =
        GuiAnnouncementFrequency.everyConnection;
    desktop = DesktopPresence(channel: desktopChannel);
    await desktop.initialize();
    owner = SchoolAnnouncementController(
      preferences: preferences,
      desktop: desktop,
      window: SchoolAnnouncementWindow(
        channel: windowChannel,
        platform: TargetPlatform.windows,
      ),
      notifications: SchoolAnnouncementNotifications(
        channel: notificationChannel,
        platform: TargetPlatform.windows,
      ),
      startedAt: data.started,
      clock: () => data.now,
      namespace: 'Test',
    );
  });
  tearDown(() async {
    await owner.close();
    owner.dispose();
    await preferences.settled;
    preferences.dispose();
    desktop.dispose();
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    for (final channel in [
      windowChannel,
      notificationChannel,
      desktopChannel,
    ]) {
      messenger.setMockMethodCallHandler(channel, null);
    }
  });
  Future<void> success({int revision = 1, int minutes = 0}) async {
    owner.updateCapabilities(
      data.capabilities(
        sessions: [
          data.session(
            data.success.add(Duration(minutes: minutes)),
            revision: revision,
          ),
        ],
      ),
    );
    await settleWork();
  }

  Future<void> visibility(bool value) {
    visible = value;
    return callback(desktopChannel, 'visibilityChanged', value);
  }

  void queryReply(Object? reply) {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          desktopChannel,
          (call) async => call.method == 'initialize'
              ? 'primary'
              : call.method == 'getVisibility'
              ? reply
              : null,
        );
  }

  test(
    'stale hidden cache queries visible and opens once without toast',
    () async {
      await visibility(false);
      queryReply(true);
      await owner.start();
      await success();
      expect(opens, hasLength(1));
      expect(notices.where((c) => c.method == 'show'), isEmpty);
      await success(revision: 2);
      expect(opens, hasLength(1));
    },
  );
  for (final behavior in [
    GuiHiddenAnnouncementBehavior.notify,
    GuiHiddenAnnouncementBehavior.onRestore,
  ]) {
    test('stale visible cache uses current hidden policy $behavior', () async {
      preferences.hiddenAnnouncementBehavior = behavior;
      queryReply(false);
      await owner.start();
      await success();
      expect(opens, isEmpty);
      expect(
        notices.where((c) => c.method == 'show'),
        behavior == GuiHiddenAnnouncementBehavior.notify
            ? hasLength(1)
            : isEmpty,
      );
      if (behavior == GuiHiddenAnnouncementBehavior.onRestore) {
        await visibility(true);
        await settleWork();
        expect(opens, hasLength(1));
        await visibility(true);
        expect(opens, hasLength(1));
      }
    });
  }
  test(
    'new callback including duplicate wins over pending query at dispatch',
    () async {
      await visibility(false);
      final reply = Completer<Object?>();
      queryReply(reply.future);
      await owner.start();
      await success();
      expect(opens, isEmpty);
      expect(notices.where((c) => c.method == 'show'), isEmpty);
      await visibility(false);
      reply.complete(true);
      await settleWork();
      expect(opens, isEmpty);
      expect(notices.where((c) => c.method == 'show'), hasLength(1));
    },
  );
  for (final mode in ['malformed', 'error', 'timeout']) {
    test(
      'unknown visibility $mode consumes success without guessed dispatch or replay',
      () async {
        preferences.announcementFrequency =
            GuiAnnouncementFrequency.dailyFirstConnection;
        final reply = Completer<Object?>();
        if (mode == 'error') {
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
              .setMockMethodCallHandler(
                desktopChannel,
                (_) async => throw PlatformException(code: 'failed'),
              );
        } else {
          queryReply(mode == 'timeout' ? reply.future : 'invalid');
        }
        await owner.start();
        await success();
        if (mode == 'timeout') {
          await Future<void>.delayed(const Duration(milliseconds: 2100));
        }
        await settleWork();
        expect(owner.feedback, '公告暂未显示，无法确认窗口状态，可在首页查看');
        expect(opens, isEmpty);
        expect(notices.where((c) => c.method == 'show'), isEmpty);
        expect(preferences.lastAutomaticAnnouncementDate, '2026-10-10');
        if (mode == 'timeout') reply.complete(true);
        queryReply(true);
        await success(revision: 2);
        expect(opens, isEmpty);
        expect(notices.where((c) => c.method == 'show'), isEmpty);
        await owner.openManually();
        expect(opens, hasLength(1));
        expect(owner.feedback, isNull);
      },
    );
  }
  for (final cancellation in [
    'manual',
    'configuration',
    'profile',
    'manualOnly',
    'close',
    'newSuccess',
  ]) {
    test(
      'pending query cancelled by $cancellation cannot dispatch late',
      () async {
        final reply = Completer<Object?>();
        queryReply(reply.future);
        await owner.start();
        await success();
        expect(opens, isEmpty);
        Future<void>? closing;
        if (cancellation == 'manual') await owner.openManually();
        if (cancellation == 'configuration') {
          owner.updateCapabilities(data.capabilities(configurations: []));
        }
        if (cancellation == 'profile') {
          owner.updateCapabilities(
            data.capabilities(profiles: [data.noMetadata]),
          );
        }
        if (cancellation == 'manualOnly') {
          preferences.announcementFrequency =
              GuiAnnouncementFrequency.manualOnly;
        }
        if (cancellation == 'close') closing = owner.close();
        if (cancellation == 'newSuccess') {
          queryReply(false);
          await success(revision: 2, minutes: 1);
        }
        reply.complete(true);
        await settleWork();
        if (closing != null) await closing;
        expect(opens, hasLength(cancellation == 'manual' ? 1 : 0));
        expect(
          notices.where((c) => c.method == 'show'),
          hasLength(cancellation == 'newSuccess' ? 1 : 0),
        );
      },
    );
  }
  test(
    'close waits for bounded unresolved query then prevents dispatch',
    () async {
      final reply = Completer<Object?>();
      queryReply(reply.future);
      await owner.start();
      await success();
      var completed = false;
      final closing = owner.close().then((_) => completed = true);
      await settleWork();
      expect(completed, isFalse);
      await closing;
      expect(completed, isTrue);
      expect(opens, isEmpty);
      expect(notices.where((c) => c.method == 'show'), isEmpty);
      reply.complete(true);
    },
  );
  test('hidden showImmediately does not wait for visibility query', () async {
    queryReply(Completer<Object?>().future);
    preferences.hiddenAnnouncementBehavior =
        GuiHiddenAnnouncementBehavior.showImmediately;
    await owner.start();
    await success();
    expect(opens, hasLength(1));
    expect(notices.where((c) => c.method == 'show'), isEmpty);
  });

  test(
    'restored preferences gate buffered startup without replacing startedAt',
    () async {
      await owner.close();
      owner.dispose();
      final storage = _DelayedSettings();
      final restored = Appearance(store: storage);
      owner = SchoolAnnouncementController(
        preferences: restored,
        desktop: desktop,
        window: SchoolAnnouncementWindow(
          channel: windowChannel,
          platform: TargetPlatform.windows,
        ),
        notifications: SchoolAnnouncementNotifications.disabled(),
        startedAt: data.started,
        clock: () => data.now,
      );
      await success();
      final starting = owner.start();
      await settleWork();
      expect(opens, isEmpty);
      storage.reading.complete(
        const GuiSettings(
          announcementFrequency: GuiAnnouncementFrequency.manualOnly,
        ),
      );
      await starting;
      await settleWork();
      expect(opens, isEmpty);
      expect(restored.lastAutomaticAnnouncementDate, isNull);
      expect(await owner.openManually(), SchoolAnnouncementOpenResult.opened);
      await owner.close();
      await restored.settled;
      restored.dispose();
    },
  );
  test(
    'buffered startup success opens exact metadata after start only',
    () async {
      await success();
      expect(opens, isEmpty);
      await owner.start();
      await settleWork();
      expect(opens.single.arguments, {
        'url': data.school.announcement!.url,
        'clientWidth': 600,
        'clientHeight': 460,
        'namespace': 'Test',
      });
      expect(preferences.lastAutomaticAnnouncementDate, '2026-10-10');
      await success(revision: 2);
      expect(opens, hasLength(1));
      await owner.start();
      expect(opens, hasLength(1));
    },
  );
  test('unknown and hidden direct never restore the main window', () async {
    await visibility(false);
    preferences.hiddenAnnouncementBehavior =
        GuiHiddenAnnouncementBehavior.showImmediately;
    await owner.start();
    await success();
    expect(opens, hasLength(1));
    expect(desktop.isMainWindowVisible, isFalse);
  });
  test(
    'notify exact click consumes once and ignores stale replaced identity',
    () async {
      await visibility(false);
      await owner.start();
      await success();
      final old =
          (notices.where((c) => c.method == 'show').last.arguments
              as Map)['token'];
      await success(revision: 2, minutes: 1);
      final token =
          (notices.where((c) => c.method == 'show').last.arguments
              as Map)['token'];
      expect(token, matches(RegExp(r'^[a-f0-9]{32}$')));
      expect(token, isNot(old));
      await callback(notificationChannel, 'clicked', old);
      expect(opens, isEmpty);
      await callback(notificationChannel, 'clicked', token);
      await settleWork();
      await callback(notificationChannel, 'failed', {'token': token});
      expect(opens, hasLength(1));
      expect(owner.feedback, isNull);
      expect(desktop.isMainWindowVisible, isFalse);
    },
  );
  test(
    'callback before failed reply retains successful terminal outcome',
    () async {
      await visibility(false);
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(notificationChannel, (call) async {
            if (call.method == 'show') {
              await callback(
                notificationChannel,
                'clicked',
                (call.arguments as Map)['token'],
              );
              await settleWork();
              return 'failed';
            }
            return null;
          });
      await owner.start();
      await success();
      expect(opens, hasLength(1));
      expect(owner.feedback, isNull);
    },
  );
  test(
    'restore is latest and one-shot; manualOnly cancels queued automatic',
    () async {
      await visibility(false);
      preferences.hiddenAnnouncementBehavior =
          GuiHiddenAnnouncementBehavior.onRestore;
      await owner.start();
      await success();
      await success(revision: 2, minutes: 1);
      expect(opens, isEmpty);
      await visibility(true);
      await settleWork();
      await visibility(true);
      expect(opens, hasLength(1));
      await visibility(false);
      await success(revision: 3, minutes: 2);
      preferences.announcementFrequency = GuiAnnouncementFrequency.manualOnly;
      await visibility(true);
      await settleWork();
      expect(opens, hasLength(1));
    },
  );
  test('restore survives stale IPC and consumes once when ready and already visible', () async {
    await visibility(false);
    preferences.hiddenAnnouncementBehavior =
        GuiHiddenAnnouncementBehavior.onRestore;
    await owner.start();
    await success();
    owner.updateCapabilities(
      data.capabilities(state: GuiConnectionState.stale),
    );
    await visibility(true);
    await settleWork();
    expect(opens, isEmpty);
    expect(owner.feedback, isNull);
    await success(revision: 2);
    expect(opens, hasLength(1));
    await success(revision: 3);
    expect(opens, hasLength(1));
  });
  test(
    'new visible intent dismisses own earlier notification before opening',
    () async {
      await visibility(false);
      await owner.start();
      await success();
      final previous = (notices.last.arguments as Map)['token'];
      await visibility(true);
      await success(revision: 2, minutes: 1);
      expect(notices.last.method, 'dismiss');
      expect(opens, hasLength(1));
      await callback(notificationChannel, 'clicked', previous);
      await settleWork();
      expect(opens, hasLength(1));
    },
  );
  test('manual bypasses frequency and does not record day', () async {
    preferences.announcementFrequency = GuiAnnouncementFrequency.manualOnly;
    await owner.start();
    await success();
    expect(preferences.lastAutomaticAnnouncementDate, isNull);
    expect(await owner.openManually(), SchoolAnnouncementOpenResult.opened);
    expect(opens, hasLength(1));
    expect(preferences.lastAutomaticAnnouncementDate, isNull);
  });
  test(
    'ready identity change cancels while transient stale preserves pending',
    () async {
      await visibility(false);
      await owner.start();
      await success();
      final token = (notices.last.arguments as Map)['token'];
      owner.updateCapabilities(
        data.capabilities(state: GuiConnectionState.stale),
      );
      await callback(notificationChannel, 'clicked', token);
      await settleWork();
      expect(opens, isEmpty);
      expect(owner.feedback, '当前学校公告暂不可用');
      await success(revision: 2);
      expect(notices.where((c) => c.method == 'show'), hasLength(1));
      await success(revision: 3, minutes: 1);
      owner.updateCapabilities(data.capabilities(configurations: []));
      await settleWork();
      final count = opens.length;
      await callback(
        notificationChannel,
        'clicked',
        (notices.where((c) => c.method == 'show').last.arguments
            as Map)['token'],
      );
      await settleWork();
      expect(opens, hasLength(count));
      expect(owner.available, isFalse);
      expect(notices.where((c) => c.method == 'dismiss'), isNotEmpty);
    },
  );
  test(
    'missing and ambiguous metadata never open manually or automatically',
    () async {
      await owner.start();
      for (final profiles in [
        [data.noMetadata],
        [data.school, data.school],
      ]) {
        owner.updateCapabilities(
          data.capabilities(
            profiles: profiles,
            sessions: [data.session(data.success)],
          ),
        );
        expect(owner.available, isFalse);
        expect(await owner.openManually(), SchoolAnnouncementOpenResult.failed);
      }
      expect(opens, isEmpty);
      expect(preferences.lastAutomaticAnnouncementDate, isNull);
    },
  );
  test(
    'window failure consumes day; unsupported manual feedback is explicit',
    () async {
      windowReply = 'failed';
      await owner.start();
      await success();
      expect(owner.feedback, '公告加载失败');
      expect(preferences.lastAutomaticAnnouncementDate, '2026-10-10');
      await success(revision: 2);
      expect(opens, hasLength(1));
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            windowChannel,
            (call) async => throw MissingPluginException(),
          );
      expect(
        await owner.openManually(),
        SchoolAnnouncementOpenResult.unsupported,
      );
      expect(owner.feedback, '当前环境暂不支持应用内公告');
    },
  );
  test(
    'actual platform failure code is shown without raw details or retry',
    () async {
      await visibility(false);
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(notificationChannel, (call) async {
            notices.add(call);
            if (call.method == 'show') {
              throw PlatformException(
                code: 'notification_failed',
                message: 'private path and identity',
                details: {'error_code': -2147024891},
              );
            }
            return null;
          });
      await owner.start();
      await success();
      expect(owner.feedback, '公告通知发送失败，可在首页查看（错误码 0x80070005）');
      await success(revision: 2);
      expect(notices.where((call) => call.method == 'show'), hasLength(1));
      expect(preferences.lastAutomaticAnnouncementDate, '2026-10-10');
    },
  );

  test('early failure code survives show reply but obsolete callback cannot restore feedback', () async {
    await visibility(false);
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(notificationChannel, (call) async {
          notices.add(call);
          if (call.method == 'show') {
            final token = (call.arguments as Map)['token'];
            await callback(notificationChannel, 'failed', {
              'token': token,
              'error_code': -2147024891,
            });
            return 'submitted';
          }
          return null;
        });
    await owner.start();
    await success();
    expect(owner.feedback, '公告通知发送失败，可在首页查看（错误码 0x80070005）');
    final token =
        (notices.firstWhere((call) => call.method == 'show').arguments
            as Map)['token'];
    await owner.openManually();
    expect(owner.feedback, isNull);
    await callback(notificationChannel, 'failed', {
      'token': token,
      'error_code': -1,
    });
    expect(owner.feedback, isNull);
    expect(opens, hasLength(1));
  });

  test(
    'notification failure has no replay; preference changes do not dispatch',
    () async {
      await visibility(false);
      notificationReply = 'failed';
      await owner.start();
      await success();
      expect(owner.feedback, '公告通知发送失败，可在首页查看');
      preferences.hiddenAnnouncementBehavior =
          GuiHiddenAnnouncementBehavior.showImmediately;
      await success(revision: 2);
      expect(opens, isEmpty);
      expect(notices.where((c) => c.method == 'show'), hasLength(1));
    },
  );
  test('close invalidates pending async open, detaches external listeners and preserves preferences', () async {
    final reply = Completer<Object?>();
    windowReply = reply.future;
    await owner.start();
    await success();
    var changes = 0;
    owner.addListener(() => changes++);
    final closing = owner.close();
    reply.complete('failed');
    await closing;
    final before = changes;
    preferences.announcementFrequency = GuiAnnouncementFrequency.manualOnly;
    await visibility(false);
    await success(revision: 2, minutes: 1);
    expect(await owner.openManually(), SchoolAnnouncementOpenResult.failed);
    expect(changes, before);
    expect(
      preferences.announcementFrequency,
      GuiAnnouncementFrequency.manualOnly,
    );
    expect(owner.feedback, isNull);
  });
}

class _DelayedSettings implements GuiSettingsStore {
  final reading = Completer<GuiSettings?>();
  @override
  bool get persistent => false;
  @override
  Future<GuiSettings?> read() => reading.future;
  @override
  Future<void> save(GuiSettings settings) async {}
}
