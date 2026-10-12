import 'dart:async';

import 'package:flutter/services.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<void> visibilityCallback(bool value) =>
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .handlePlatformMessage(
            desktopPresenceChannelName,
            const StandardMethodCodec().encodeMethodCall(
              MethodCall('visibilityChanged', value),
            ),
            (_) {},
          );

  test(
    'current query updates owner and callback including duplicate wins',
    () async {
      const channel = MethodChannel(desktopPresenceChannelName);
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      Object? reply = true;
      messenger.setMockMethodCallHandler(
        channel,
        (call) async => call.method == 'initialize' ? 'primary' : reply,
      );
      final presence = DesktopPresence(channel: channel);
      addTearDown(() {
        presence.dispose();
        messenger.setMockMethodCallHandler(channel, null);
      });
      await presence.initialize();
      reply = false;
      expect(await presence.refreshMainWindowVisibility(), isFalse);
      expect(presence.isMainWindowVisible, isFalse);
      final pending = Completer<Object?>();
      reply = pending.future;
      final query = presence.refreshMainWindowVisibility();
      await visibilityCallback(false);
      pending.complete(true);
      expect(await query, isFalse);
      expect(presence.isMainWindowVisible, isFalse);
      expect(presence.enabled, isTrue);
    },
  );
  test(
    'invalid queries preserve availability and unrelated feedback',
    () async {
      const channel = MethodChannel(desktopPresenceChannelName);
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      Object? reply = true;
      messenger.setMockMethodCallHandler(channel, (call) async {
        if (call.method == 'initialize') return 'primary';
        if (call.method == 'configurePreferences') {
          throw PlatformException(code: 'failed');
        }
        if (reply is Exception) throw reply;
        return reply;
      });
      final presence = DesktopPresence(channel: channel);
      addTearDown(() {
        presence.dispose();
        messenger.setMockMethodCallHandler(channel, null);
      });
      await presence.initialize();
      await presence.configurePreferences(
        closeBehavior: GuiCloseBehavior.exit,
        startHidden: false,
      );
      final feedback = presence.feedback;
      for (final value in [null, 'false', PlatformException(code: 'failed')]) {
        reply = value;
        expect(await presence.refreshMainWindowVisibility(), isNull);
        expect(presence.enabled, isTrue);
        expect(presence.feedback, feedback);
        expect(presence.isMainWindowVisible, isTrue);
      }
      final pending = Completer<Object?>();
      reply = pending.future;
      final query = presence.refreshMainWindowVisibility();
      await visibilityCallback(false);
      pending.completeError(PlatformException(code: 'failed'));
      expect(await query, isFalse);
      expect(presence.feedback, feedback);
    },
  );
  test(
    'visibility query timeout and disposed query do not guess cached state',
    () async {
      const channel = MethodChannel(desktopPresenceChannelName);
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      Object? reply = true;
      messenger.setMockMethodCallHandler(
        channel,
        (call) async => call.method == 'initialize' ? 'primary' : reply,
      );
      final presence = DesktopPresence(channel: channel);
      addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
      var disposed = false;
      try {
        await presence.initialize();
        final pending = Completer<Object?>();
        reply = pending.future;
        expect(await presence.refreshMainWindowVisibility(), isNull);
        expect(presence.enabled, isTrue);
        pending.complete(false);
        await Future<void>.delayed(Duration.zero);
        expect(presence.isMainWindowVisible, isTrue);
        final late = Completer<Object?>();
        reply = late.future;
        final query = presence.refreshMainWindowVisibility();
        presence.dispose();
        disposed = true;
        late.complete(false);
        expect(await query, isNull);
        expect(
          await DesktopPresence.disabled().refreshMainWindowVisibility(),
          isNull,
        );
      } finally {
        if (!disposed) presence.dispose();
      }
    },
  );
  test(
    'newer query supersedes old reply without changing callback authority',
    () async {
      const channel = MethodChannel(desktopPresenceChannelName);
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      Object? reply = true;
      messenger.setMockMethodCallHandler(
        channel,
        (call) async => call.method == 'initialize' ? 'primary' : reply,
      );
      final presence = DesktopPresence(channel: channel);
      addTearDown(() {
        presence.dispose();
        messenger.setMockMethodCallHandler(channel, null);
      });
      await presence.initialize();
      final old = Completer<Object?>();
      reply = old.future;
      final first = presence.refreshMainWindowVisibility();
      await Future<void>.delayed(Duration.zero);
      reply = false;
      expect(await presence.refreshMainWindowVisibility(), isFalse);
      old.complete(true);
      expect(await first, isNull);
      expect(presence.isMainWindowVisible, isFalse);
    },
  );

  test(
    'initialize reports activatedExisting and primary from native',
    () async {
      const channel = MethodChannel(desktopPresenceChannelName);
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      addTearDown(() => messenger.setMockMethodCallHandler(channel, null));

      messenger.setMockMethodCallHandler(channel, (call) async {
        expect(call.method, 'initialize');
        expect(call.arguments, '');
        return 'activatedExisting';
      });
      final presence = DesktopPresence(channel: channel);
      addTearDown(presence.dispose);

      expect(
        await presence.initialize(),
        DesktopPresenceDisposition.activatedExisting,
      );

      messenger.setMockMethodCallHandler(channel, (call) async => 'primary');
      expect(await presence.initialize(), DesktopPresenceDisposition.primary);
    },
  );

  test(
    'desktop preference map, begin and cancel use existing channel',
    () async {
      const channel = MethodChannel(desktopPresenceChannelName);
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      final calls = <MethodCall>[];
      messenger.setMockMethodCallHandler(channel, (call) async {
        calls.add(call);
        return call.method == 'initialize' ? 'primary' : null;
      });
      addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
      final presence = DesktopPresence(channel: channel);
      addTearDown(presence.dispose);
      expect(presence.enabled, false);
      await presence.initialize();
      expect(presence.enabled, true);
      expect(
        await presence.configurePreferences(
          closeBehavior: GuiCloseBehavior.exit,
          startHidden: true,
        ),
        true,
      );
      expect(calls.last.arguments, {'closeToTray': false, 'startHidden': true});
      expect(await presence.beginExit(), true);
      await presence.cancelExit();
      expect(calls.map((c) => c.method), [
        'initialize',
        'getVisibility',
        'configurePreferences',
        'beginExit',
        'cancelExit',
      ]);
      messenger.setMockMethodCallHandler(
        channel,
        (_) async => throw PlatformException(code: 'failed'),
      );
      expect(
        await presence.configurePreferences(
          closeBehavior: GuiCloseBehavior.hideToTray,
          startHidden: false,
        ),
        false,
      );
      expect(presence.feedback, contains('未能应用'));
      expect(presence.feedback, isNot(contains('已保存')));
      expect(await presence.beginExit(), false);
    },
  );

  test('validates production, accepted and rejected runtime namespaces', () {
    expect(validateDesktopNamespace(null), '');
    expect(validateDesktopNamespace(''), '');
    expect(validateDesktopNamespace('ns02-native-a'), 'ns02-native-a');
    expect(validateDesktopNamespace('A.B_c-9'), 'A.B_c-9');
    for (final invalid in [
      ' production',
      'production ',
      '../invalid',
      'a:b',
      '.leading',
      'trailing.',
      'a..b',
      'DEFAULT',
      'prod',
      'é',
    ]) {
      expect(validateDesktopNamespace(invalid), isNull, reason: invalid);
    }
    expect(validateDesktopNamespace('a' * 65), isNull);
  });

  test('initialize passes the exact validated namespace argument', () async {
    const channel = MethodChannel(desktopPresenceChannelName);
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
    messenger.setMockMethodCallHandler(channel, (call) async {
      expect(call.method, 'initialize');
      expect(call.arguments, 'Ns02-Native-A');
      return 'primary';
    });
    final presence = DesktopPresence(
      channel: channel,
      namespace: validateDesktopNamespace('Ns02-Native-A')!,
    );
    addTearDown(presence.dispose);
    expect(await presence.initialize(), DesktopPresenceDisposition.primary);
  });

  test(
    'native initialization failure never blocks the primary bootstrap',
    () async {
      const channel = MethodChannel(desktopPresenceChannelName);
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
      messenger.setMockMethodCallHandler(channel, (call) async {
        throw StateError('missing native');
      });

      final presence = DesktopPresence(channel: channel);
      addTearDown(presence.dispose);
      expect(await presence.initialize(), DesktopPresenceDisposition.primary);
    },
  );

  test('DesktopPresence.destroy asks native to destroy once', () async {
    const channel = MethodChannel(desktopPresenceChannelName);
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    final methods = <String>[];
    addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
    messenger.setMockMethodCallHandler(channel, (call) async {
      methods.add(call.method);
      return null;
    });

    final presence = DesktopPresence(channel: channel);
    addTearDown(presence.dispose);
    await presence.destroy();

    expect(methods, ['destroy']);
  });

  test('disabled presence never touches the channel', () async {
    final presence = DesktopPresence.disabled();
    expect(presence.enabled, isFalse);
    expect(await presence.initialize(), DesktopPresenceDisposition.primary);
    await presence.destroy();
  });
}
