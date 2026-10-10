import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

const _channel = MethodChannel(desktopPresenceChannelName);

TestDefaultBinaryMessenger _messenger() =>
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;

void _resetHandler() => _messenger().setMockMethodCallHandler(_channel, null);

Future<void> _sendNative(String method, [Object? arguments]) async {
  await _messenger().handlePlatformMessage(
    _channel.name,
    const StandardMethodCodec().encodeMethodCall(MethodCall(method, arguments)),
    (_) {},
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'primary initialization samples visible and hidden HWND states',
    () async {
      for (final visible in [true, false]) {
        final methods = <String>[];
        _messenger().setMockMethodCallHandler(_channel, (call) async {
          methods.add(call.method);
          return switch (call.method) {
            'initialize' => 'primary',
            'getVisibility' => visible,
            _ => null,
          };
        });
        final presence = DesktopPresence(channel: _channel);

        expect(await presence.initialize(), DesktopPresenceDisposition.primary);
        expect(presence.enabled, isTrue);
        expect(presence.isMainWindowVisible, visible);
        expect(methods, ['initialize', 'getVisibility']);

        presence.dispose();
        _resetHandler();
      }
    },
  );

  test(
    'activated-existing and disabled presence keep visibility unknown',
    () async {
      final methods = <String>[];
      _messenger().setMockMethodCallHandler(_channel, (call) async {
        methods.add(call.method);
        return 'activatedExisting';
      });
      final existing = DesktopPresence(channel: _channel);
      expect(
        await existing.initialize(),
        DesktopPresenceDisposition.activatedExisting,
      );
      expect(existing.isMainWindowVisible, isNull);
      expect(existing.enabled, isFalse);
      expect(methods, ['initialize']);
      existing.dispose();
      _resetHandler();

      final disabled = DesktopPresence.disabled();
      expect(await disabled.initialize(), DesktopPresenceDisposition.primary);
      expect(disabled.isMainWindowVisible, isNull);
      expect(disabled.enabled, isFalse);
      disabled.dispose();
    },
  );

  test(
    'native visibility transitions notify only when the value changes',
    () async {
      _messenger().setMockMethodCallHandler(_channel, (call) async {
        if (call.method == 'initialize') return 'primary';
        if (call.method == 'getVisibility') return true;
        return null;
      });
      final presence = DesktopPresence(channel: _channel);
      var notifications = 0;
      presence.addListener(() => notifications++);
      await presence.initialize();
      expect(presence.isMainWindowVisible, isTrue);
      expect(notifications, 1);

      await _sendNative('visibilityChanged', false);
      expect(presence.isMainWindowVisible, isFalse);
      expect(notifications, 2);
      await _sendNative('visibilityChanged', false);
      expect(presence.isMainWindowVisible, isFalse);
      expect(notifications, 2);
      await _sendNative('visibilityChanged', true);
      expect(presence.isMainWindowVisible, isTrue);
      expect(notifications, 3);

      presence.dispose();
      _resetHandler();
    },
  );

  test(
    'newer duplicate callback prevents an older query result from winning',
    () async {
      final visibility = Completer<Object?>();
      final methods = <String>[];
      _messenger().setMockMethodCallHandler(_channel, (call) async {
        methods.add(call.method);
        if (call.method == 'initialize') return 'primary';
        if (call.method == 'getVisibility') return visibility.future;
        return null;
      });
      final presence = DesktopPresence(channel: _channel);
      final initializing = presence.initialize();
      await Future<void>.delayed(Duration.zero);
      expect(methods, ['initialize', 'getVisibility']);

      await _sendNative('visibilityChanged', false);
      await _sendNative('visibilityChanged', false);
      visibility.complete(true);
      await initializing;

      expect(presence.isMainWindowVisible, isFalse);
      presence.dispose();
      _resetHandler();
    },
  );

  test('malformed query and callback leave visibility unknown', () async {
    _messenger().setMockMethodCallHandler(_channel, (call) async {
      if (call.method == 'initialize') return 'primary';
      if (call.method == 'getVisibility') return 'visible';
      return null;
    });
    final presence = DesktopPresence(channel: _channel);
    await presence.initialize();
    expect(presence.enabled, isTrue);
    expect(presence.isMainWindowVisible, isNull);
    await _sendNative('visibilityChanged', 'hidden');
    await _sendNative('visibilityChanged', null);
    expect(presence.isMainWindowVisible, isNull);
    expect(presence.feedback, isNull);

    presence.dispose();
    _resetHandler();
  });

  test('query failure leaves preference and exit commands usable', () async {
    final methods = <String>[];
    _messenger().setMockMethodCallHandler(_channel, (call) async {
      methods.add(call.method);
      if (call.method == 'initialize') return 'primary';
      if (call.method == 'getVisibility') {
        throw PlatformException(code: 'visibility_unavailable');
      }
      return null;
    });
    final presence = DesktopPresence(channel: _channel);
    await presence.initialize();
    expect(presence.isMainWindowVisible, isNull);
    expect(presence.enabled, isTrue);
    expect(presence.feedback, isNull);
    expect(
      await presence.configurePreferences(
        closeBehavior: GuiCloseBehavior.exit,
        startHidden: true,
      ),
      isTrue,
    );
    expect(await presence.beginExit(), isTrue);
    await presence.cancelExit();
    expect(methods, [
      'initialize',
      'getVisibility',
      'configurePreferences',
      'beginExit',
      'cancelExit',
    ]);
    expect(presence.feedback, isNull);

    presence.dispose();
    _resetHandler();
  });

  test('malformed initialize response and query failure do not disable primary flow', () async {
    final methods = <String>[];
    _messenger().setMockMethodCallHandler(_channel, (call) async {
      methods.add(call.method);
      return call.method == 'initialize' ? 'unexpected' : true;
    });
    final presence = DesktopPresence(channel: _channel);
    expect(await presence.initialize(), DesktopPresenceDisposition.primary);
    expect(presence.isMainWindowVisible, isNull);
    expect(presence.enabled, isFalse);
    expect(methods, ['initialize']);

    presence.dispose();
    _resetHandler();
  });

  test(
    'dispose during initialization ignores the late primary response',
    () async {
      final initialization = Completer<Object?>();
      _messenger().setMockMethodCallHandler(_channel, (call) async {
        if (call.method == 'initialize') return initialization.future;
        return true;
      });
      final presence = DesktopPresence(channel: _channel);
      var notifications = 0;
      presence.addListener(() => notifications++);
      final pending = presence.initialize();
      presence.dispose();
      initialization.complete('primary');
      await pending;

      expect(presence.enabled, isFalse);
      expect(presence.isMainWindowVisible, isNull);
      expect(notifications, 0);
      _resetHandler();
    },
  );

  test(
    'dispose during visibility query ignores late query and callback',
    () async {
      final visibility = Completer<Object?>();
      _messenger().setMockMethodCallHandler(_channel, (call) async {
        if (call.method == 'initialize') return 'primary';
        if (call.method == 'getVisibility') return visibility.future;
        return null;
      });
      final presence = DesktopPresence(channel: _channel);
      var notifications = 0;
      presence.addListener(() => notifications++);
      final pending = presence.initialize();
      await Future<void>.delayed(Duration.zero);
      presence.dispose();
      visibility.complete(false);
      await pending;
      try {
        await _sendNative('visibilityChanged', true);
      } on Object {
        // The channel handler is removed on dispose; late native delivery is inert.
      }

      expect(presence.isMainWindowVisible, isNull);
      expect(notifications, 0);
      _resetHandler();
    },
  );
}
