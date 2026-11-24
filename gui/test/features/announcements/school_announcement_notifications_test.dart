import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/desktop/system_notifications.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_notifications.dart';

const channel = MethodChannel(systemNotificationsChannelName);
const announcementToken = '0123456789abcdef0123456789abcdef';
const connectionToken = 'abcdef0123456789abcdef0123456789';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  final calls = <MethodCall>[];

  Future<void> nativeEvent(String method, Map<String, Object> arguments) async {
    await messenger.handlePlatformMessage(
      channel.name,
      const StandardMethodCodec().encodeMethodCall(
        MethodCall(method, arguments),
      ),
      (_) {},
    );
  }

  setUp(() {
    calls.clear();
    messenger.setMockMethodCallHandler(channel, (call) async {
      calls.add(call);
      return call.method == 'show' ? 'submitted' : null;
    });
  });
  tearDown(() => messenger.setMockMethodCallHandler(channel, null));

  test(
    'announcement sends its exact payload and disposes its owned channel',
    () async {
      final adapter = SchoolAnnouncementNotifications(
        channel: channel,
        platform: TargetPlatform.windows,
      );
      addTearDown(adapter.dispose);

      expect(
        await adapter.show(token: announcementToken, namespace: 'Test-1'),
        SchoolAnnouncementNotificationResult.submitted,
      );
      expect(calls.single.method, 'show');
      expect(calls.single.arguments, {
        'category': 'announcement',
        'token': announcementToken,
        'namespace': 'Test-1',
        'title': '校园网公告',
        'body': '点击查看学校公告',
      });
      expect(await adapter.dismiss(), isTrue);
      final failures = <String>[];
      adapter.onFailed = failures.add;
      expect(
        await adapter.show(token: announcementToken),
        SchoolAnnouncementNotificationResult.submitted,
      );
      await nativeEvent('failed', {
        'category': 'announcement',
        'token': announcementToken,
        'error_code': -2147024891,
      });
      expect(failures, [announcementToken]);
      expect(adapter.lastFailureCode, '0x80070005');

      await adapter.dispose();
      await adapter.dispose();
      expect(calls.map((call) => call.method), [
        'show',
        'dismiss',
        'show',
        'dispose',
      ]);
      expect(calls[1].arguments, {'category': 'announcement'});
      expect(calls.last.arguments, isNull);
      expect(adapter.lastFailureCode, isNull);
      await nativeEvent('failed', {
        'category': 'announcement',
        'token': announcementToken,
        'error_code': -1,
      });
      expect(failures, [announcementToken]);
      expect(adapter.lastFailureCode, isNull);
      expect(
        await adapter.show(token: announcementToken),
        SchoolAnnouncementNotificationResult.failed,
      );
    },
  );

  test('announcement maps both unsuccessful transport results', () async {
    final disabled = SchoolAnnouncementNotifications.disabled();
    addTearDown(disabled.dispose);
    expect(
      await disabled.show(token: announcementToken),
      SchoolAnnouncementNotificationResult.unsupported,
    );
    expect(await disabled.dismiss(), isTrue);
    expect(calls, isEmpty);

    final adapter = SchoolAnnouncementNotifications(
      channel: channel,
      platform: TargetPlatform.windows,
    );
    addTearDown(adapter.dispose);
    messenger.setMockMethodCallHandler(channel, (call) async => 'failed');
    expect(
      await adapter.show(token: announcementToken),
      SchoolAnnouncementNotificationResult.failed,
    );
  });

  test(
    'disposing shared announcement leaves connection callbacks active',
    () async {
      final service = SystemNotifications(
        channel: channel,
        platform: TargetPlatform.windows,
      );
      final adapter = SchoolAnnouncementNotifications.shared(service);
      addTearDown(service.dispose);
      addTearDown(adapter.dispose);
      final connection = service.slot(SystemNotificationCategory.connection);
      final clicked = <String>[];
      adapter.onClicked = (token) => clicked.add('announcement:$token');
      connection.onClicked = (token) => clicked.add('connection:$token');

      await adapter.show(token: announcementToken);
      await connection.show(token: connectionToken, title: '连接', body: '状态');
      await adapter.dispose();
      expect(calls.map((call) => call.method), ['show', 'show', 'dismiss']);
      expect(calls.last.arguments, {'category': 'announcement'});

      for (final (category, token) in [
        ('announcement', announcementToken),
        ('connection', connectionToken),
      ]) {
        await nativeEvent('clicked', {'category': category, 'token': token});
      }
      expect(clicked, ['connection:$connectionToken']);
      expect(
        await connection.show(token: connectionToken, title: '连接', body: '状态'),
        SystemNotificationResult.submitted,
      );
    },
  );
}
