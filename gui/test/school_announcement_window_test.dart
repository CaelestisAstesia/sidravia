import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_window.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

const metadata = AnnouncementMetadata(
  url: 'http://login.jlu.edu.cn/notice_win.php',
  clientWidth: 600,
  clientHeight: 460,
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const channel = MethodChannel(schoolAnnouncementChannelName);
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  tearDown(() => messenger.setMockMethodCallHandler(channel, null));

  test('disabled and non-Windows carrier never invokes native', () async {
    messenger.setMockMethodCallHandler(
      channel,
      (_) async => fail('native invoked'),
    );
    for (final window in [
      SchoolAnnouncementWindow.disabled(),
      SchoolAnnouncementWindow(
        channel: channel,
        platform: TargetPlatform.linux,
      ),
    ]) {
      expect(
        await window.open(metadata),
        SchoolAnnouncementOpenResult.unsupported,
      );
      expect(await window.close(), true);
      await window.dispose();
    }
  });

  test(
    'Windows open sends exact safe metadata and namespace, then closes',
    () async {
      final calls = <MethodCall>[];
      messenger.setMockMethodCallHandler(channel, (call) async {
        calls.add(call);
        return call.method == 'open' ? 'opened' : null;
      });
      final window = SchoolAnnouncementWindow(
        channel: channel,
        platform: TargetPlatform.windows,
      );
      expect(
        await window.open(metadata, namespace: 'Test-1'),
        SchoolAnnouncementOpenResult.opened,
      );
      expect(calls.single.method, 'open');
      expect(calls.single.arguments, {
        'url': metadata.url,
        'clientWidth': 600,
        'clientHeight': 460,
        'namespace': 'Test-1',
      });
      expect(await window.close(), true);
      await window.dispose();
      await window.dispose();
      expect(calls.map((c) => c.method), ['open', 'close', 'close']);
      expect(await window.open(metadata), SchoolAnnouncementOpenResult.failed);
      expect(await window.close(), true);
    },
  );

  test('direct invalid metadata and namespace fail before native', () async {
    var invocations = 0;
    messenger.setMockMethodCallHandler(channel, (_) async {
      invocations++;
      return 'opened';
    });
    final window = SchoolAnnouncementWindow(
      channel: channel,
      platform: TargetPlatform.windows,
    );
    for (final url in [
      '',
      'notice.php',
      'file:///notice',
      'ftp://example.com',
      'http://',
      'http://:80',
      'http://@example.com',
      'http://u:p@example.com',
      'http://example.com/#',
      'http://example.com/a b',
      'http://example.com/\n',
      'http://example.com/\u2003',
      'http://example.com/%zz',
    ]) {
      expect(
        await window.open(
          AnnouncementMetadata(url: url, clientWidth: 600, clientHeight: 460),
        ),
        SchoolAnnouncementOpenResult.failed,
        reason: url,
      );
    }
    for (final size in [0, -1, 4097]) {
      expect(
        await window.open(
          AnnouncementMetadata(
            url: metadata.url,
            clientWidth: size,
            clientHeight: 460,
          ),
        ),
        SchoolAnnouncementOpenResult.failed,
      );
      expect(
        await window.open(
          AnnouncementMetadata(
            url: metadata.url,
            clientWidth: 600,
            clientHeight: size,
          ),
        ),
        SchoolAnnouncementOpenResult.failed,
      );
    }
    for (final namespace in [
      'production',
      'PrOd',
      'default',
      '..',
      '.abc',
      'abc.',
      'a/b',
      'a b',
      '学校',
      'a' * 65,
    ]) {
      expect(
        await window.open(metadata, namespace: namespace),
        SchoolAnnouncementOpenResult.failed,
      );
    }
    expect(invocations, 0);
    await window.dispose();
  });

  test('HTTP HTTPS and path/query at-sign remain valid', () async {
    messenger.setMockMethodCallHandler(channel, (_) async => 'opened');
    final window = SchoolAnnouncementWindow(
      channel: channel,
      platform: TargetPlatform.windows,
    );
    for (final url in [
      'http://school.example/a@b?x=@c',
      'https://school.example/notice',
    ]) {
      expect(
        await window.open(
          AnnouncementMetadata(url: url, clientWidth: 1, clientHeight: 4096),
        ),
        SchoolAnnouncementOpenResult.opened,
      );
    }
    await window.dispose();
  });

  test(
    'missing plugin unsupported; platform and malformed replies failed',
    () async {
      final window = SchoolAnnouncementWindow(
        channel: channel,
        platform: TargetPlatform.windows,
      );
      messenger.setMockMethodCallHandler(
        channel,
        (_) async => throw MissingPluginException(),
      );
      expect(
        await window.open(metadata),
        SchoolAnnouncementOpenResult.unsupported,
      );
      expect(await window.close(), false);
      messenger.setMockMethodCallHandler(
        channel,
        (_) async => throw PlatformException(code: 'failed'),
      );
      expect(await window.open(metadata), SchoolAnnouncementOpenResult.failed);
      expect(await window.close(), false);
      for (final reply in [null, 'success', 1, false]) {
        messenger.setMockMethodCallHandler(channel, (_) async => reply);
        expect(
          await window.open(metadata),
          SchoolAnnouncementOpenResult.failed,
        );
      }
      await window.dispose();
    },
  );

  test(
    'dispose closes pending open and late acknowledgement cannot reopen',
    () async {
      final pending = Completer<String>();
      final methods = <String>[];
      messenger.setMockMethodCallHandler(channel, (call) async {
        methods.add(call.method);
        if (call.method == 'open') return pending.future;
        return null;
      });
      final window = SchoolAnnouncementWindow(
        channel: channel,
        platform: TargetPlatform.windows,
      );
      final opening = window.open(metadata);
      await Future<void>.delayed(Duration.zero);
      await window.dispose();
      pending.complete('opened');
      expect(await opening, SchoolAnnouncementOpenResult.failed);
      expect(methods, ['open', 'close']);
    },
  );
}
