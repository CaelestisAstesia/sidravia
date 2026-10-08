import 'package:sidravia_gui/application/gui_snapshot.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  for (final initialState in ['authenticated', 'blocked_by_error']) {
    test(
      'daemon restart observes reused session freshly from $initialState',
      () {
        final policy = DesktopNotificationPolicy();
        expect(
          policy.evaluate(
            _snapshot([_session('authenticated', BigInt.from(90))], pid: 100),
          ),
          isNull,
        );
        expect(
          policy
              .evaluate(
                _snapshot([
                  _session('blocked_by_error', BigInt.from(91)),
                ], pid: 100),
              )
              ?.title,
          '校园网需要处理',
        );
        expect(
          policy.evaluate(
            _snapshot([_session(initialState, BigInt.one)], pid: 200),
          ),
          isNull,
        );
        expect(
          policy.evaluate(
            _snapshot([_session('authenticated', BigInt.from(2))], pid: 200),
          ),
          isNull,
        );
        expect(
          policy
              .evaluate(
                _snapshot([
                  _session('blocked_by_error', BigInt.from(3)),
                ], pid: 200),
              )
              ?.title,
          '校园网需要处理',
        );
        expect(
          policy
              .evaluate(
                _snapshot([
                  _session('authenticated', BigInt.from(4)),
                ], pid: 200),
              )
              ?.title,
          '校园网状态已恢复',
        );
      },
    );
  }
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

      var notifyCalls = 0;
      messenger.setMockMethodCallHandler(channel, (call) async {
        notifyCalls++;
        return null;
      });
      await presence.considerSnapshot(
        _snapshot([_session('blocked_by_error', BigInt.from(1))]),
      );
      expect(notifyCalls, 0);
    },
  );

  test('blocked/recovery reducer emits no initial noise and dedupes', () {
    final policy = DesktopNotificationPolicy();
    expect(
      policy.evaluate(
        _snapshot([_session('blocked_by_error', BigInt.from(1))]),
      ),
      isNull,
    );
    expect(
      policy.evaluate(
        _snapshot([_session('blocked_by_error', BigInt.from(1))]),
      ),
      isNull,
    );

    expect(
      policy.evaluate(_snapshot([_session('authenticated', BigInt.from(2))])),
      isNull,
    );

    expect(
      policy.evaluate(_snapshot([_session('authenticated', BigInt.from(3))])),
      isNull,
    );

    final blocked = policy.evaluate(
      _snapshot([
        _session('blocked_by_error', BigInt.from(4), description: '用户名或密码错误。'),
      ]),
    );
    expect(blocked?.title, '校园网需要处理');
    expect(blocked?.body, '用户名或密码错误。');

    final recovery = policy.evaluate(
      _snapshot([_session('authenticated', BigInt.from(5))]),
    );
    expect(recovery?.title, '校园网状态已恢复');
    expect(recovery?.body, '连接状态已更新。');
  });

  test('non-actionable states never produce notifications', () {
    final policy = DesktopNotificationPolicy();
    var revision = BigInt.zero;
    for (final state in const [
      'authenticating',
      'waiting_for_network',
      'waiting_before_retry',
      'stopping',
      'suspended',
      'authenticated',
    ]) {
      revision += BigInt.one;
      expect(
        policy.evaluate(_snapshot([_session(state, revision)])),
        isNull,
        reason: state,
      );
    }
  });

  test('DesktopPresence relays exactly one notification to native', () async {
    const channel = MethodChannel(desktopPresenceChannelName);
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    final calls = <MethodCall>[];
    addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
    messenger.setMockMethodCallHandler(channel, (call) async {
      calls.add(call);
      return null;
    });

    final presence = DesktopPresence(channel: channel);
    addTearDown(presence.dispose);
    await presence.considerSnapshot(
      _snapshot([_session('authenticated', BigInt.from(1))]),
    );
    await presence.considerSnapshot(
      _snapshot([
        _session('blocked_by_error', BigInt.from(2), description: '错误内容'),
      ]),
    );

    expect(calls, hasLength(1));
    expect(calls.single.method, 'notify');
    final args = calls.single.arguments as Map<Object?, Object?>;
    expect(args['title'], '校园网需要处理');
    expect(args['body'], '错误内容');
  });

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
    await presence.considerSnapshot(
      _snapshot([_session('blocked_by_error', BigInt.from(1))]),
    );
    await presence.destroy();
  });
}

GuiSnapshot _snapshot(List<SessionSummary> sessions, {int pid = 1}) =>
    GuiSnapshot(
      daemon: DaemonStatus(
        productVersion: 'test',
        buildId: 'test',
        pid: pid,
        status: 'running',
        mode: 'desktop',
        desktopOwnerPid: 2,
      ),
      profiles: const [],
      configurations: const [],
      sessions: sessions,
    );

SessionSummary _session(String state, BigInt revision, {String? description}) =>
    SessionSummary(
      id: 'session-a',
      displayName: '',
      accountName: 'user',
      state: SessionState.decode(state),
      intent: SessionIntent.maintainAuthentication,
      configurationId: 'cfg-a',
      revision: revision,
      stateReason: description == null
          ? null
          : SessionStateReason(code: 'test', description: description),
    );
