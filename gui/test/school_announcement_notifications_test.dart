import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_notifications.dart';

const channel = MethodChannel(schoolAnnouncementNotificationsChannelName);
const first = '0123456789abcdef0123456789abcdef';
const second = 'abcdef0123456789abcdef0123456789';
const submitted = SchoolAnnouncementNotificationResult.submitted;
const failed = SchoolAnnouncementNotificationResult.failed;
const unsupported = SchoolAnnouncementNotificationResult.unsupported;

Future<void> native(String method, Object? arguments) async {
  await TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .handlePlatformMessage(
        channel.name,
        const StandardMethodCodec().encodeMethodCall(
          MethodCall(method, arguments),
        ),
        (_) {},
      );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late SchoolAnnouncementNotifications adapter;
  final calls = <MethodCall>[];
  setUp(() {
    calls.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          return call.method == 'show' ? 'submitted' : null;
        });
    adapter = SchoolAnnouncementNotifications(
      channel: channel,
      platform: TargetPlatform.windows,
    );
  });
  tearDown(() async {
    await adapter.dispose();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
  });

  test('exact payload and null teardown arguments', () async {
    expect(await adapter.show(token: first, namespace: 'Test-1'), submitted);
    expect(calls.single.method, 'show');
    expect(calls.single.arguments, {'token': first, 'namespace': 'Test-1'});
    expect(await adapter.dismiss(), isTrue);
    await adapter.dispose();
    await adapter.dispose();
    expect(calls.map((call) => call.method), ['show', 'dismiss', 'dispose']);
    expect(calls.skip(1).every((call) => call.arguments == null), isTrue);
    expect(await adapter.show(token: second), failed);
  });

  test('invalid tokens and namespaces never invoke native', () async {
    for (final token in [
      '',
      'a' * 31,
      'a' * 33,
      'A' * 32,
      'g' * 32,
      '中' * 32,
    ]) {
      expect(await adapter.show(token: token), failed);
    }
    for (final namespace in [
      'prod',
      'Production',
      'DEFAULT',
      '.a',
      'a.',
      'a..b',
      'a/b',
      '中',
      'a' * 65,
    ]) {
      expect(await adapter.show(token: first, namespace: namespace), failed);
    }
    expect(calls, isEmpty);
  });

  test('disabled and non-Windows adapters are unsupported', () async {
    for (final disabled in [
      SchoolAnnouncementNotifications.disabled(),
      SchoolAnnouncementNotifications(
        channel: channel,
        platform: TargetPlatform.linux,
      ),
    ]) {
      expect(await disabled.show(token: first), unsupported);
      expect(await disabled.dismiss(), isTrue);
      await disabled.dispose();
    }
    expect(calls, isEmpty);
  });

  test('missing, errors and malformed replies are typed failures', () async {
    final outcomes = <Object?>[
      'unsupported',
      'failed',
      'unknown',
      null,
      42,
      MissingPluginException(),
      PlatformException(code: 'denied'),
    ];
    for (final outcome in outcomes) {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            if (outcome is Exception) throw outcome;
            return outcome;
          });
      expect(
        await adapter.show(token: first),
        outcome == 'unsupported' || outcome is MissingPluginException
            ? unsupported
            : failed,
      );
    }
  });

  test('click identity is current and terminal exactly once', () async {
    final clicked = <String>[];
    final errors = <String>[];
    adapter.onClicked = clicked.add;
    adapter.onFailed = errors.add;
    await adapter.show(token: first);
    await native('clicked', second);
    await native('clicked', {'token': first});
    await native('unknown', first);
    await native('failed', 'bad');
    expect(clicked, isEmpty);
    await native('clicked', first);
    await native('clicked', first);
    await native('failed', first);
    expect(clicked, [first]);
    expect(errors, isEmpty);
    await adapter.show(token: second);
    await native('failed', first);
    await native('failed', second);
    await native('clicked', second);
    expect(errors, [second]);
  });

  test('newer show survives older asynchronous error', () async {
    final old = Completer<Object?>();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          channel,
          (call) async => (call.arguments as Map)['token'] == first
              ? old.future
              : 'submitted',
        );
    final clicked = <String>[];
    adapter.onClicked = clicked.add;
    final pending = adapter.show(token: first);
    expect(await adapter.show(token: second), submitted);
    old.complete('failed');
    expect(await pending, failed);
    await native('clicked', first);
    await native('clicked', second);
    expect(clicked, [second]);
  });

  test('immediate callback before show acknowledgement is safe', () async {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          await native('clicked', first);
          return 'submitted';
        });
    final clicked = <String>[];
    adapter.onClicked = clicked.add;
    expect(await adapter.show(token: first), submitted);
    await native('clicked', first);
    expect(clicked, [first]);
  });

  test(
    'dismiss clears identity before await and late show cannot restore it',
    () async {
      final show = Completer<Object?>();
      final dismiss = Completer<Object?>();
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            channel,
            (call) async => call.method == 'show'
                ? show.future
                : call.method == 'dismiss'
                ? dismiss.future
                : null,
          );
      final clicked = <String>[];
      adapter.onClicked = clicked.add;
      final pending = adapter.show(token: first);
      final clearing = adapter.dismiss();
      await native('clicked', first);
      show.complete('submitted');
      expect(await pending, failed);
      dismiss.complete(null);
      expect(await clearing, isTrue);
      await native('clicked', first);
      expect(clicked, isEmpty);
    },
  );

  test(
    'dispose detaches immediately and ignores late results and callbacks',
    () async {
      final show = Completer<Object?>();
      final dispose = Completer<Object?>();
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            channel,
            (call) async =>
                call.method == 'show' ? show.future : dispose.future,
          );
      final clicked = <String>[];
      adapter.onClicked = clicked.add;
      final pending = adapter.show(token: first);
      final closing = adapter.dispose();
      await native('clicked', first);
      show.complete('submitted');
      expect(await pending, failed);
      dispose.complete(null);
      await closing;
      expect(clicked, isEmpty);
    },
  );

  test(
    'dismiss platform failure remains nonfatal and identity stays cleared',
    () async {
      await adapter.show(token: first);
      final clicked = <String>[];
      adapter.onClicked = clicked.add;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            channel,
            (call) async => throw PlatformException(code: 'hide_failed'),
          );
      expect(await adapter.dismiss(), isFalse);
      await native('clicked', first);
      expect(clicked, isEmpty);
    },
  );
}
