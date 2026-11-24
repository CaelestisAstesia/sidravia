import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/desktop/system_notifications.dart';

const tokenA = '00112233445566778899aabbccddeeff';
const tokenB = 'ffeeddccbbaa99887766554433221100';
const channel = MethodChannel(systemNotificationsChannelName);
const submitted = SystemNotificationResult.submitted;
const failed = SystemNotificationResult.failed;
const unsupported = SystemNotificationResult.unsupported;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  late SystemNotifications service;
  late SystemNotificationSlot connection;
  final calls = <MethodCall>[];

  void respond(Future<Object?> Function(MethodCall) response) =>
      messenger.setMockMethodCallHandler(channel, response);

  Future<void> event(String method, Object? payload) async {
    await messenger.handlePlatformMessage(
      channel.name,
      const StandardMethodCodec().encodeMethodCall(MethodCall(method, payload)),
      (_) {},
    );
  }

  Future<SystemNotificationResult> show({
    String token = tokenA,
    String namespace = '',
    String title = '连接',
    String body = '状态',
  }) => connection.show(
    token: token,
    title: title,
    body: body,
    namespace: namespace,
  );

  Future<void> click([String token = tokenA]) =>
      event('clicked', {'category': 'connection', 'token': token});

  Future<void> failure(String token, Object? code) => event('failed', {
    'category': 'connection',
    'token': token,
    'error_code': ?code,
  });

  setUp(() {
    calls.clear();
    respond((call) async {
      calls.add(call);
      return call.method == 'show' ? 'submitted' : null;
    });
    service = SystemNotifications(
      channel: channel,
      platform: TargetPlatform.windows,
    );
    connection = service.slot(SystemNotificationCategory.connection);
  });
  tearDown(() async {
    await service.dispose();
    messenger.setMockMethodCallHandler(channel, null);
  });

  test('replacement and dismiss invalidate only their category', () async {
    final announcement = service.slot(SystemNotificationCategory.announcement);
    final clicked = <String>[];
    announcement.onClicked = (token) => clicked.add('announcement:$token');
    connection.onClicked = (token) => clicked.add('connection:$token');
    expect(
      await announcement.show(token: tokenA, title: '公告', body: '查看'),
      submitted,
    );
    expect(await show(token: tokenB), submitted);
    await click(tokenA);
    expect(clicked, isEmpty);
    await event('clicked', {'category': 'announcement', 'token': tokenA});
    expect(clicked, ['announcement:$tokenA']);
    await announcement.show(token: tokenA, title: '公告', body: '查看');
    expect(
      await announcement.show(token: tokenB, title: '公告', body: '更新'),
      submitted,
    );
    expect(calls.map((call) => (call.arguments as Map)['category']), [
      'announcement',
      'connection',
      'announcement',
      'announcement',
    ]);
    await event('clicked', {'category': 'announcement', 'token': tokenA});
    expect(clicked, ['announcement:$tokenA']);
    await announcement.dismiss();
    expect(calls.last.arguments, {'category': 'announcement'});
    await event('clicked', {'category': 'announcement', 'token': tokenB});
    await click(tokenB);
    await click(tokenB);
    expect(clicked, ['announcement:$tokenA', 'connection:$tokenB']);
  });

  test(
    'invalid text, tokens and namespaces never invoke the channel',
    () async {
      for (final title in ['', 'a' * 129, '\u0000', '\ud800', '\udc00']) {
        expect(await show(title: title), failed);
      }
      expect(await show(body: 'a' * 513), failed);
      for (final token in [
        '',
        'a' * 31,
        'a' * 33,
        'A' * 32,
        'g' * 32,
        '中' * 32,
        '$tokenA\n',
      ]) {
        expect(await show(token: token), failed);
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
        expect(await show(namespace: namespace), failed);
      }
      expect(calls, isEmpty);
      expect(await show(title: '<&" 中文', body: 'emoji 🌟 & text'), submitted);
    },
  );

  test('unsupported platforms never invoke channel', () async {
    final unavailable = SystemNotifications(
      channel: channel,
      platform: TargetPlatform.android,
    );
    addTearDown(unavailable.dispose);
    final slot = unavailable.slot(SystemNotificationCategory.connection);
    expect(
      await slot.show(token: tokenA, title: '连接', body: '状态'),
      unsupported,
    );
    expect(await slot.dismiss(), isTrue);
    await unavailable.dispose();
    expect(calls, isEmpty);
  });

  test(
    'native results distinguish unsupported from malformed and failed',
    () async {
      for (final (result, expected) in [
        ('unsupported', unsupported),
        (MissingPluginException(), unsupported),
        ('failed', failed),
        ('unknown', failed),
        (null, failed),
        (42, failed),
        (PlatformException(code: 'denied'), failed),
      ]) {
        respond((call) async {
          if (call.method != 'show') return null;
          if (result is Exception) throw result;
          return result;
        });
        expect(await show(), expected);
      }
    },
  );

  test(
    'only validated signed HRESULT is exposed for a failed native reply',
    () async {
      for (final (details, expected) in <(Object?, String?)>[
        ({'error_code': -2147024891}, '0x80070005'),
        ({'error_code': -2147483648}, '0x80000000'),
        ({'error_code': -1}, '0xFFFFFFFF'),
        ({}, null),
        ({'error_code': 0}, null),
        ({'error_code': 2147942405}, null),
        ({'error_code': -2147483649}, null),
        ({'error_code': '-2147024891'}, null),
        ({'error_code': -1.5}, null),
        ({'error_code': -2147024891, 'message': 'private'}, null),
        ('private raw detail', null),
      ]) {
        respond((call) async {
          if (call.method != 'show') return null;
          throw PlatformException(
            code: 'notification_failed',
            message: 'private raw native message',
            details: details,
          );
        });
        expect(await show(), failed);
        expect(connection.lastFailureCode, expected);
        await connection.dismiss();
        expect(connection.lastFailureCode, isNull);
      }
    },
  );

  test('malformed callbacks do not consume an active token', () async {
    final clicked = <String>[];
    connection.onClicked = clicked.add;
    await show();
    for (final payload in [
      tokenA,
      {'token': tokenA},
      {'category': 'other', 'token': tokenA},
      {'category': 'connection', 'token': tokenA, 'extra': true},
      {'category': 'connection', 'token': 2},
    ]) {
      await event('clicked', payload);
    }
    await event('unknown', {'category': 'connection', 'token': tokenA});
    expect(clicked, isEmpty);
    await click();
    expect(clicked, [tokenA]);
  });

  test(
    'failure callback validates identity and preserves per-slot HRESULT',
    () async {
      final announcement = service.slot(
        SystemNotificationCategory.announcement,
      );
      final failures = <String>[];
      final clicked = <String>[];
      connection.onFailed = failures.add;
      connection.onClicked = clicked.add;
      await announcement.show(token: tokenB, title: '公告', body: '查看');
      await show();
      await event('failed', tokenA);
      await failure(tokenB, -2147024891);
      await event('failed', {
        'category': 'connection',
        'token': tokenA,
        'error_code': -1,
        'extra': 'private',
      });
      expect(failures, isEmpty);
      expect(connection.lastFailureCode, isNull);
      await failure(tokenA, -2147023728);
      expect(failures, [tokenA]);
      expect(connection.lastFailureCode, '0x80070490');
      expect(announcement.lastFailureCode, isNull);
      await failure(tokenA, -1);
      await click();
      expect(failures, [tokenA]);
      expect(clicked, isEmpty);
      expect(connection.lastFailureCode, '0x80070490');

      await show(token: tokenB);
      expect(connection.lastFailureCode, isNull);
      await failure(tokenB, 2147942405);
      expect(connection.lastFailureCode, isNull);
      await show();
      await failure(tokenA, null);
      expect(failures, [tokenA, tokenB, tokenA]);
      expect(connection.lastFailureCode, isNull);
    },
  );

  test(
    'click and failure are terminal even before the show acknowledgement',
    () async {
      for (final method in ['clicked', 'failed']) {
        final received = <String>[];
        connection.onClicked = (token) => received.add('clicked:$token');
        connection.onFailed = (token) => received.add('failed:$token');
        respond((call) async {
          if (call.method != 'show') return null;
          await event(method, {
            'category': 'connection',
            'token': tokenA,
            if (method == 'failed') 'error_code': -2147024891,
          });
          return 'submitted';
        });
        expect(await show(), submitted);
        await click();
        await failure(tokenA, -1);
        expect(received, ['$method:$tokenA']);
        expect(
          connection.lastFailureCode,
          method == 'failed' ? '0x80070005' : null,
        );
      }
      await service.dispose();
      expect(connection.lastFailureCode, isNull);
      await failure(tokenA, -1);
      expect(connection.lastFailureCode, isNull);
    },
  );

  test('new show survives an older asynchronous native error', () async {
    final old = Completer<Object?>();
    final started = Completer<void>();
    respond((call) async {
      if (call.method != 'show') return null;
      if ((call.arguments as Map)['token'] == tokenA) {
        started.complete();
        return await old.future;
      }
      return 'submitted';
    });
    final clicked = <String>[];
    connection.onClicked = clicked.add;
    final pending = show();
    await started.future;
    expect(await show(token: tokenB), submitted);
    old.completeError(
      PlatformException(
        code: 'notification_failed',
        details: {'error_code': -2147024891},
      ),
    );
    expect(await pending, failed);
    expect(connection.lastFailureCode, isNull);
    await click();
    await click(tokenB);
    expect(clicked, [tokenB]);
  });

  test(
    'dismiss clears identity before acknowledgement and ignores late show',
    () async {
      final showed = Completer<Object?>();
      final dismissed = Completer<Object?>();
      respond(
        (call) async => switch (call.method) {
          'show' => showed.future,
          'dismiss' => dismissed.future,
          _ => null,
        },
      );
      final clicked = <String>[];
      connection.onClicked = clicked.add;
      final pending = show();
      final clearing = connection.dismiss();
      await click();
      showed.complete('submitted');
      expect(await pending, failed);
      dismissed.complete(null);
      expect(await clearing, isTrue);
      await click();
      expect(clicked, isEmpty);
    },
  );

  test('failed native dismissal leaves the token cleared', () async {
    final clicked = <String>[];
    connection.onClicked = clicked.add;
    await show();
    respond((call) async => throw PlatformException(code: 'hide_failed'));
    expect(await connection.dismiss(), isFalse);
    await click();
    expect(clicked, isEmpty);
  });

  test(
    'owner disposal detaches before acknowledgement and ignores late show',
    () async {
      final showed = Completer<Object?>();
      final disposed = Completer<Object?>();
      respond(
        (call) async => call.method == 'show' ? showed.future : disposed.future,
      );
      final clicked = <String>[];
      connection.onClicked = clicked.add;
      final pending = show();
      final closing = service.dispose();
      await click();
      showed.complete('submitted');
      expect(await pending, failed);
      disposed.complete(null);
      await closing;
      expect(clicked, isEmpty);
    },
  );
}
