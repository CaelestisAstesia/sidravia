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
      await adapter.dispose();
      await adapter.dispose();
      expect(calls.map((call) => call.method), ['show', 'dismiss', 'dispose']);
      expect(calls[1].arguments, {'category': 'announcement'});
      expect(calls[2].arguments, isNull);
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
        await messenger.handlePlatformMessage(
          channel.name,
          const StandardMethodCodec().encodeMethodCall(
            MethodCall('clicked', {'category': category, 'token': token}),
          ),
          (_) {},
        );
      }
      expect(clicked, ['connection:$connectionToken']);
      expect(
        await connection.show(token: connectionToken, title: '连接', body: '状态'),
        SystemNotificationResult.submitted,
      );
    },
  );
}
