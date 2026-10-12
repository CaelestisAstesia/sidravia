import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/desktop/system_notifications.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_notifications.dart';

const tokenA = '00112233445566778899aabbccddeeff';
const tokenB = 'ffeeddccbbaa99887766554433221100';
const channel = MethodChannel(systemNotificationsChannelName);
Future<void> event(String method, Object? payload) async =>
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .handlePlatformMessage(
          channel.name,
          const StandardMethodCodec().encodeMethodCall(
            MethodCall(method, payload),
          ),
          (_) {},
        );
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late SystemNotifications service;
  final calls = <MethodCall>[];
  setUp(() {
    calls.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          return call.method == 'show' ? 'submitted' : null;
        });
    service = SystemNotifications(
      channel: channel,
      platform: TargetPlatform.windows,
    );
  });
  tearDown(() async {
    await service.dispose();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
  });
  test(
    'announcement and connection share one channel with independent callbacks',
    () async {
      final announcements = SchoolAnnouncementNotifications.shared(service);
      final connection = service.slot(SystemNotificationCategory.connection);
      final clicked = <String>[];
      announcements.onClicked = (t) => clicked.add('announcement:$t');
      connection.onClicked = (t) => clicked.add('connection:$t');
      expect(
        await announcements.show(token: tokenA, namespace: 'demo'),
        SchoolAnnouncementNotificationResult.submitted,
      );
      expect(
        await connection.show(
          token: tokenB,
          title: '已连接',
          body: '校园网认证成功',
          namespace: 'demo',
        ),
        SystemNotificationResult.submitted,
      );
      expect(calls.where((c) => c.method == 'show'), hasLength(2));
      expect((calls.first.arguments as Map)['category'], 'announcement');
      await event('clicked', {'category': 'connection', 'token': tokenA});
      expect(clicked, isEmpty);
      await event('clicked', {'category': 'announcement', 'token': tokenA});
      await event('clicked', {'category': 'connection', 'token': tokenB});
      await event('clicked', {'category': 'connection', 'token': tokenB});
      expect(clicked, ['announcement:$tokenA', 'connection:$tokenB']);
    },
  );
  test('replacement and dismiss invalidate only their category', () async {
    final ann = service.slot(SystemNotificationCategory.announcement);
    final conn = service.slot(SystemNotificationCategory.connection);
    final clicked = <String>[];
    ann.onClicked = clicked.add;
    conn.onClicked = clicked.add;
    await ann.show(token: tokenA, title: '公告', body: '查看');
    await conn.show(token: tokenA, title: '连接', body: '恢复');
    await ann.show(token: tokenB, title: '公告', body: '更新');
    await event('clicked', {'category': 'announcement', 'token': tokenA});
    expect(clicked, isEmpty);
    await ann.dismiss();
    expect(calls.last.arguments, {'category': 'announcement'});
    await event('clicked', {'category': 'announcement', 'token': tokenB});
    await event('clicked', {'category': 'connection', 'token': tokenA});
    expect(clicked, [tokenA]);
  });
  test('invalid payloads never cross the platform boundary', () async {
    final slot = service.slot(SystemNotificationCategory.connection);
    for (final title in ['', 'a' * 129, '\u0000', '\ud800', '\udc00']) {
      expect(
        await slot.show(token: tokenA, title: title, body: '正常'),
        SystemNotificationResult.failed,
      );
    }
    expect(
      await slot.show(token: tokenA, title: '正常', body: 'a' * 513),
      SystemNotificationResult.failed,
    );
    expect(
      await slot.show(token: 'A' * 32, title: '正常', body: '正常'),
      SystemNotificationResult.failed,
    );
    expect(calls, isEmpty);
    expect(
      await slot.show(token: tokenA, title: '<&" 中文', body: 'emoji 🌟 & text'),
      SystemNotificationResult.submitted,
    );
  });
  test('malformed callbacks do not consume an active token', () async {
    final slot = service.slot(SystemNotificationCategory.connection);
    final clicked = <String>[];
    slot.onClicked = clicked.add;
    await slot.show(token: tokenA, title: '连接', body: '状态');
    for (final payload in [
      tokenA,
      {'token': tokenA},
      {'category': 'other', 'token': tokenA},
      {'category': 'connection', 'token': tokenA, 'extra': true},
      {'category': 'connection', 'token': 2},
    ]) {
      await event('clicked', payload);
    }
    expect(clicked, isEmpty);
    await event('clicked', {'category': 'connection', 'token': tokenA});
    expect(clicked, [tokenA]);
  });
  test('failure identity and HRESULT are isolated per slot', () async {
    final ann = service.slot(SystemNotificationCategory.announcement);
    final conn = service.slot(SystemNotificationCategory.connection);
    final failures = <String>[];
    conn.onFailed = failures.add;
    await ann.show(token: tokenA, title: '公告', body: '查看');
    await conn.show(token: tokenB, title: '连接', body: '状态');
    await event('failed', {
      'category': 'connection',
      'token': tokenB,
      'error_code': -2147023728,
    });
    expect(failures, [tokenB]);
    expect(conn.lastFailureCode, '0x80070490');
    expect(ann.lastFailureCode, isNull);
  });
  test('late show response cannot restore a dismissed slot', () async {
    final response = Completer<Object?>();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          channel,
          (call) async => call.method == 'show' ? response.future : null,
        );
    final slot = service.slot(SystemNotificationCategory.connection);
    final clicked = <String>[];
    slot.onClicked = clicked.add;
    final pending = slot.show(token: tokenA, title: '连接', body: '状态');
    await slot.dismiss();
    response.complete('submitted');
    expect(await pending, SystemNotificationResult.failed);
    await event('clicked', {'category': 'connection', 'token': tokenA});
    expect(clicked, isEmpty);
  });
  test('unsupported platforms never invoke channel', () async {
    final unsupported = SystemNotifications(
      channel: channel,
      platform: TargetPlatform.linux,
    );
    expect(
      await unsupported
          .slot(SystemNotificationCategory.connection)
          .show(token: tokenA, title: '连接', body: '状态'),
      SystemNotificationResult.unsupported,
    );
    await unsupported.dispose();
    expect(calls, isEmpty);
  });
  test('newline token is rejected before platform submission', () async {
    final slot = service.slot(SystemNotificationCategory.connection);
    expect(
      await slot.show(token: '$tokenA\n', title: '连接', body: '状态'),
      SystemNotificationResult.failed,
    );
    expect(calls, isEmpty);
  });
}
